package accessanalysis

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

const (
	defaultScanMaxBytes = 64 * 1024 * 1024
	defaultScanMaxLines = 500000
	finalizeTimeout     = 3 * time.Second
	exportPageSize      = 200
	exportMaxRows       = 100000
	staleJobThreshold   = 10 * time.Minute
	staleJobError       = "访问分析扫描因服务异常退出或超时而中止"
)

type Agent interface {
	AccessAnalysisScan(ctx context.Context, req *AgentScanRequest) (*AgentScanResponse, error)
	AccessAnalysisFormatDetect(ctx context.Context, req *AgentFormatDetectRequest) (*FormatDetectResponse, error)
}

type Service struct {
	siteRepo         *repo.SiteRepo
	repo             *Repo
	opRepo           *repo.OperationRepo
	agent            Agent
	scheduledTaskSvc ScheduledTaskService
	taskLogDir       string
	rootCtx          context.Context
	running          sync.Map
	admissionMu      sync.Mutex
}

func NewService(rootCtx context.Context, siteRepo *repo.SiteRepo, analysisRepo *Repo, opRepo *repo.OperationRepo, agent Agent) *Service {
	return &Service{siteRepo: siteRepo, repo: analysisRepo, opRepo: opRepo, agent: agent, rootCtx: rootCtx}
}

func (s *Service) SetTaskLogDir(dir string) {
	s.taskLogDir = dir
}

func (s *Service) withRootCancellation(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.rootCtx == nil {
		return context.WithCancel(ctx)
	}
	derived, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.rootCtx, cancel)
	return derived, func() {
		stop()
		cancel()
	}
}

func (s *Service) RecoverStaleJobs(ctx context.Context) error {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	protected := make([]string, 0)
	s.running.Range(func(key, _ any) bool {
		if siteID, ok := key.(string); ok {
			protected = append(protected, siteID)
		}
		return true
	})
	now := time.Now().UTC()
	_, err := s.repo.RecoverStaleJobs(ctx, "", now.Add(-staleJobThreshold), now, protected)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, "恢复遗留访问分析任务失败: "+err.Error(), nil)
	}
	return nil
}

func (s *Service) admitScan(ctx context.Context, siteID string) (*Job, error) {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	if _, active := s.running.Load(siteID); active {
		existing, err := s.repo.GetRunningJob(ctx, siteID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
		return nil, app.NewAppError(app.ErrConflict, "当前站点已有访问分析扫描正在运行", nil)
	}
	now := time.Now().UTC()
	if _, err := s.repo.RecoverStaleJobs(ctx, siteID, now.Add(-staleJobThreshold), now, nil); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetRunningJob(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	s.running.Store(siteID, struct{}{})
	return nil, nil
}

func (s *Service) releaseScan(siteID string) {
	s.admissionMu.Lock()
	s.running.Delete(siteID)
	s.admissionMu.Unlock()
}

func (s *Service) Summary(ctx context.Context, siteID string, q Query) (*SummaryResponse, error) {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return nil, err
	}
	from, to, err := normalizeQueryRange(q)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.Summary(ctx, siteID, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	trend, err := s.repo.Hourly(ctx, siteID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return &SummaryResponse{Summary: *summary, Trend: trend}, nil
}

func (s *Service) Scan(ctx context.Context, siteID string, req ScanRequest, requestID string) (*ScanResponse, error) {
	return s.scan(ctx, siteID, req, requestID, "manual")
}

func (s *Service) scan(ctx context.Context, siteID string, req ScanRequest, requestID, trigger string) (response *ScanResponse, retErr error) {
	ctx, cancel := s.withRootCancellation(ctx)
	defer cancel()
	if existing, err := s.admitScan(ctx, siteID); err != nil {
		var appErr *app.AppError
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	} else if existing != nil {
		return &ScanResponse{JobID: existing.ID, Status: existing.Status, ScannedLines: existing.ScannedLines, SkippedLines: existing.SkippedLines, DurationMS: existing.DurationMS}, nil
	}
	defer s.releaseScan(siteID)

	site, err := s.requireSiteContext(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if site.AccessLogPath == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "站点未配置 access log 路径", nil)
	}
	settings, err := s.repo.GetSettings(ctx, siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	from, to, err := resolveScanRange(req)
	if err != nil {
		return nil, err
	}

	jobID := app.NewOperationID()
	job := &Job{ID: jobID, SiteID: siteID, Trigger: trigger, RangeStart: from.Format(time.RFC3339), RangeEnd: to.Format(time.RFC3339), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	jobFinalized := false
	start := time.Now()
	opID := app.NewOperationID()
	message := "手动扫描访问日志 " + site.PrimaryDomain
	if trigger == "scheduler" {
		message = "定时扫描访问日志 " + site.PrimaryDomain
	}
	_ = s.opRepo.CreateContext(ctx, &repo.Operation{ID: opID, Action: "site.access_analysis.scan", TargetType: "site", TargetID: siteID, Status: "pending", RequestID: requestID, Actor: "admin", Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	defer func() {
		if jobFinalized {
			return
		}
		finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
		defer finalCancel()
		failure := "访问分析扫描异常中止"
		if retErr != nil {
			failure = retErr.Error()
		} else if ctx.Err() != nil {
			failure = "访问分析扫描已取消: " + ctx.Err().Error()
		}
		durationMS := time.Since(start).Milliseconds()
		_ = s.repo.FinishJobFailed(finalCtx, jobID, failure, durationMS)
		_ = s.opRepo.UpdateErrorContext(finalCtx, opID, "failed", app.ErrInternalError, failure, "")
	}()

	cursor, err := s.repo.GetCursor(ctx, siteID, site.AccessLogPath)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	includeRotated := settings.IncludeRotated
	if req.IncludeRotated != nil {
		includeRotated = *req.IncludeRotated
	}
	result, err := s.agent.AccessAnalysisScan(ctx, &AgentScanRequest{Path: site.AccessLogPath, FromTime: job.RangeStart, ToTime: job.RangeEnd, MaxBytes: defaultScanMaxBytes, MaxLines: defaultScanMaxLines, Format: settings.LogFormat, CustomPattern: settings.CustomPattern, Cursor: cursor, IncludeRotated: includeRotated, NormalizeQuery: settings.NormalizeQuery, CollectEntries: settings.SaveEntries, MaxEntries: settings.MaxEntries})
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "扫描访问日志失败: "+err.Error(), nil)
	}
	if err := s.repo.SaveScanResult(ctx, siteID, site.AccessLogPath, jobID, result.Cursor, result, settings, durationMS); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	jobFinalized = true
	_ = s.repo.CleanupSite(ctx, siteID, settings)
	_ = s.opRepo.UpdateStatusContext(ctx, opID, "success")
	return &ScanResponse{JobID: jobID, Status: "success", ScannedLines: result.ScannedLines, SkippedLines: result.SkippedLines, Truncated: result.Truncated, DurationMS: durationMS, Truncation: result.Truncation}, nil
}

func (s *Service) Settings(ctx context.Context, siteID string) (*Settings, error) {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return nil, err
	}
	settings, err := s.repo.GetSettings(ctx, siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return s.applyTaskScheduleToSettings(ctx, settings)
}

func (s *Service) SaveSettings(ctx context.Context, siteID string, settings *Settings, requestID string) (*Settings, error) {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return nil, err
	}
	settings.SiteID = siteID
	if err := validateSettings(settings); err != nil {
		return nil, err
	}
	if err := s.repo.SaveSettings(ctx, settings); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := s.syncSettingsTask(ctx, settings); err != nil {
		return nil, err
	}
	opID := app.NewOperationID()
	_ = s.opRepo.Create(&repo.Operation{ID: opID, Action: "site.access_analysis.settings", TargetType: "site", TargetID: siteID, Status: "success", RequestID: requestID, Actor: "admin", Message: "保存访问分析设置", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	return s.repo.GetSettings(ctx, siteID)
}

func (s *Service) Paths(ctx context.Context, siteID string, q Query) (*Page[PathStat], error) {
	if err := s.validateQuerySite(ctx, siteID, &q); err != nil {
		return nil, err
	}
	return s.repo.Paths(ctx, siteID, q)
}
func (s *Service) IPs(ctx context.Context, siteID string, q Query) (*Page[IPStat], error) {
	if err := s.validateQuerySite(ctx, siteID, &q); err != nil {
		return nil, err
	}
	return s.repo.IPs(ctx, siteID, q)
}
func (s *Service) Entries(ctx context.Context, siteID string, q Query) (*Page[Entry], error) {
	if err := s.validateQuerySite(ctx, siteID, &q); err != nil {
		return nil, err
	}
	return s.repo.Entries(ctx, siteID, q)
}
func (s *Service) Anomalies(ctx context.Context, siteID string, q Query) ([]Anomaly, error) {
	if err := s.validateQuerySite(ctx, siteID, &q); err != nil {
		return nil, err
	}
	return s.repo.Anomalies(ctx, siteID, q)
}
func (s *Service) Jobs(ctx context.Context, siteID string, page, pageSize int) (*Page[Job], error) {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return nil, err
	}
	normalizePage(&page, &pageSize)
	return s.repo.Jobs(ctx, siteID, page, pageSize)
}

func (s *Service) DetectFormat(ctx context.Context, siteID string, sample string) (*FormatDetectResponse, error) {
	site, err := s.requireSiteContext(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if sample != "" {
		resp := DetectFormatFromSample(sample)
		return &resp, nil
	}
	return s.agent.AccessAnalysisFormatDetect(ctx, &AgentFormatDetectRequest{Path: site.AccessLogPath, MaxLines: 20})
}

func (s *Service) TestFormat(pattern, sample string) (*FormatDetectResponse, error) {
	resp := TestCustomPattern(pattern, sample)
	return &resp, nil
}

func (s *Service) OptimizeFormat(ctx context.Context, siteID, requestID string) (*OptimizeResponse, error) {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return nil, err
	}
	settings, err := s.repo.GetSettings(ctx, siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	settings.LogFormat = string(FormatNxpanelJSON)
	settings.CustomPattern = ""
	if err := s.repo.SaveSettings(ctx, settings); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	opID := app.NewOperationID()
	_ = s.opRepo.CreateContext(ctx, &repo.Operation{ID: opID, Action: "site.access_analysis.optimize_format", TargetType: "site", TargetID: siteID, Status: "success", RequestID: requestID, Actor: "admin", Message: "优化访问分析日志格式设置为 nxpanel_json", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	return &OptimizeResponse{RecommendedConf: RecommendedNxpanelJSON, OperationID: opID}, nil
}

func (s *Service) ExportCSV(ctx context.Context, w io.Writer, kind, siteID string, q Query) error {
	if kind != "paths" && kind != "ips" && kind != "entries" {
		return app.NewAppError(app.ErrBadRequest, "kind 必须是 paths、ips 或 entries", nil)
	}
	if err := s.validateQuerySite(ctx, siteID, &q); err != nil {
		return err
	}
	q.Page, q.PageSize = 1, exportPageSize
	cw := csv.NewWriter(w)
	defer cw.Flush()
	headings := map[string][]string{
		"paths":   {"date", "path", "requests", "unique_ips", "status_2xx", "status_3xx", "status_4xx", "status_5xx", "bytes", "last_seen_at"},
		"ips":     {"date", "ip", "requests", "unique_paths", "error_requests", "bytes", "first_seen_at", "last_seen_at", "sample_user_agent"},
		"entries": {"ts", "ip", "method", "path", "status", "bytes", "referer", "user_agent", "anomaly"},
	}
	if err := cw.Write(headings[kind]); err != nil {
		return err
	}
	written := 0
	for written < exportMaxRows {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.exportPage(ctx, cw, kind, siteID, q, exportMaxRows-written)
		if err != nil {
			return err
		}
		written += rows
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
		if rows < q.PageSize {
			return nil
		}
		q.Page++
	}
	return nil
}

func (s *Service) exportPage(ctx context.Context, cw *csv.Writer, kind, siteID string, q Query, remaining int) (int, error) {
	write := func(row []string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return cw.Write(row)
	}
	switch kind {
	case "paths":
		page, err := s.repo.Paths(ctx, siteID, q)
		if err != nil {
			return 0, err
		}
		for i, item := range page.Items {
			if i >= remaining {
				return i, nil
			}
			if err := write([]string{safeCSV(item.Date), safeCSV(item.Path), fmt.Sprint(item.Requests), fmt.Sprint(item.UniqueIPs), fmt.Sprint(item.Status2xx), fmt.Sprint(item.Status3xx), fmt.Sprint(item.Status4xx), fmt.Sprint(item.Status5xx), fmt.Sprint(item.Bytes), safeCSV(item.LastSeenAt)}); err != nil {
				return i, err
			}
		}
		return len(page.Items), nil
	case "ips":
		page, err := s.repo.IPs(ctx, siteID, q)
		if err != nil {
			return 0, err
		}
		for i, item := range page.Items {
			if i >= remaining {
				return i, nil
			}
			if err := write([]string{safeCSV(item.Date), safeCSV(item.IP), fmt.Sprint(item.Requests), fmt.Sprint(item.UniquePaths), fmt.Sprint(item.ErrorRequests), fmt.Sprint(item.Bytes), safeCSV(item.FirstSeenAt), safeCSV(item.LastSeenAt), safeCSV(item.SampleUserAgent)}); err != nil {
				return i, err
			}
		}
		return len(page.Items), nil
	default:
		page, err := s.repo.Entries(ctx, siteID, q)
		if err != nil {
			return 0, err
		}
		for i, item := range page.Items {
			if i >= remaining {
				return i, nil
			}
			if err := write([]string{safeCSV(item.TS), safeCSV(item.IP), safeCSV(item.Method), safeCSV(item.Path), fmt.Sprint(item.Status), fmt.Sprint(item.Bytes), safeCSV(item.Referer), safeCSV(item.UserAgent), safeCSV(item.AnomalyReason)}); err != nil {
				return i, err
			}
		}
		return len(page.Items), nil
	}
}

func (s *Service) requireSiteContext(ctx context.Context, siteID string) (*repo.Site, error) {
	if siteID == "" {
		return nil, app.NewAppError(app.ErrBadRequest, "site_id 不能为空", nil)
	}
	site, err := s.siteRepo.GetByIDContext(ctx, siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	return site, nil
}

func (s *Service) validateQuerySite(ctx context.Context, siteID string, q *Query) error {
	if _, err := s.requireSiteContext(ctx, siteID); err != nil {
		return err
	}
	from, to, err := normalizeQueryRange(*q)
	if err != nil {
		return err
	}
	q.From, q.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
	q.IP = truncateUTF8(q.IP, MaxIPBytes)
	q.Path = truncateUTF8(q.Path, MaxPathBytes)
	q.Method = truncateUTF8(q.Method, MaxMethodBytes)
	normalizePage(&q.Page, &q.PageSize)
	return nil
}

func resolveScanRange(req ScanRequest) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch req.Range {
	case "", "today":
		return today, today.AddDate(0, 0, 1), nil
	case "yesterday":
		return today.AddDate(0, 0, -1), today, nil
	case "7d":
		return today.AddDate(0, 0, -6), today.AddDate(0, 0, 1), nil
	case "custom":
		from, err := parseDateOrTime(req.From)
		if err != nil {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrBadRequest, "from 时间格式无效", nil)
		}
		to, err := parseDateOrTime(req.To)
		if err != nil {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrBadRequest, "to 时间格式无效", nil)
		}
		if !to.After(from) {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrValidationFailed, "结束时间必须晚于开始时间", nil)
		}
		if to.Sub(from) > 30*24*time.Hour {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrValidationFailed, "扫描范围最多 30 天", nil)
		}
		return from, to, nil
	default:
		return time.Time{}, time.Time{}, app.NewAppError(app.ErrBadRequest, "range 必须是 today、yesterday、7d 或 custom", nil)
	}
}

func normalizeQueryRange(q Query) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -7)
	var err error
	if q.From != "" {
		from, err = parseDateOrTime(q.From)
		if err != nil {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrBadRequest, "from 时间格式无效", nil)
		}
	}
	if q.To != "" {
		to, err = parseDateOrTime(q.To)
		if err != nil {
			return time.Time{}, time.Time{}, app.NewAppError(app.ErrBadRequest, "to 时间格式无效", nil)
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, app.NewAppError(app.ErrValidationFailed, "结束时间不能早于开始时间", nil)
	}
	if to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, app.NewAppError(app.ErrValidationFailed, "查询范围最多 366 天", nil)
	}
	return from, to, nil
}

func parseDateOrTime(value string) (time.Time, error) {
	if len(value) == 10 {
		return time.ParseInLocation("2006-01-02", value, time.UTC)
	}
	return time.Parse(time.RFC3339, value)
}

func validateSettings(settings *Settings) error {
	if _, err := time.Parse("15:04", settings.ScanTime); err != nil {
		return app.NewAppError(app.ErrValidationFailed, "scan_time 格式必须是 HH:mm", nil)
	}
	if settings.RetentionDays < 1 || settings.RetentionDays > 365 {
		return app.NewAppError(app.ErrValidationFailed, "retention_days 必须在 1 到 365 之间", nil)
	}
	if settings.EntriesRetentionDays < 1 || settings.EntriesRetentionDays > 30 {
		return app.NewAppError(app.ErrValidationFailed, "entries_retention_days 必须在 1 到 30 之间", nil)
	}
	if settings.MaxEntries < 1000 || settings.MaxEntries > 500000 {
		return app.NewAppError(app.ErrValidationFailed, "max_entries 必须在 1000 到 500000 之间", nil)
	}
	if settings.PathTopN < 100 || settings.PathTopN > 50000 {
		return app.NewAppError(app.ErrValidationFailed, "path_top_n 必须在 100 到 50000 之间", nil)
	}
	if settings.IPTopN < 100 || settings.IPTopN > 50000 {
		return app.NewAppError(app.ErrValidationFailed, "ip_top_n 必须在 100 到 50000 之间", nil)
	}
	switch LogFormat(settings.LogFormat) {
	case FormatCommon, FormatCombined, FormatNxpanelJSON:
	case FormatCustom:
		if _, err := NewParser(settings.LogFormat, settings.CustomPattern, settings.NormalizeQuery); err != nil {
			return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	default:
		return app.NewAppError(app.ErrValidationFailed, "log_format 无效", nil)
	}
	return nil
}

func normalizePage(page, pageSize *int) {
	if *page <= 0 {
		*page = 1
	}
	if *pageSize <= 0 {
		*pageSize = 20
	}
	if *pageSize > 200 {
		*pageSize = 200
	}
	if *page > 1000000 {
		*page = 1000000
	}
}

func safeCSV(value string) string {
	if value == "" {
		return value
	}
	if strings.ContainsAny(value[:1], "=+-@") {
		return "'" + value
	}
	return value
}
