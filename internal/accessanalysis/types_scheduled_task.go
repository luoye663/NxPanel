package accessanalysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

const (
	ScheduledTaskTypeAccessAnalysisScan = "access_analysis_scan"
	accessAnalysisSourceType            = "access_analysis_setting"
	accessAnalysisLogFile               = "access_analysis_scan.log"
)

type AccessAnalysisParams struct {
	SiteID string `json:"site_id"`
	Range  string `json:"range"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type ScheduledTaskService interface {
	Register(handler scheduledtask.TaskHandler) error
	GetBySource(ctx context.Context, sourceType, sourceID string) (*scheduledtask.Task, error)
	UpsertSource(ctx context.Context, sourceType, sourceID string, system bool, req scheduledtask.CreateTaskRequest) (scheduledtask.TaskListItem, error)
	CreateSourceIfMissing(ctx context.Context, sourceType, sourceID string, system bool, lastRunAt *time.Time, req scheduledtask.CreateTaskRequest) (bool, error)
}

type AccessAnalysisTaskHandler struct {
	service *Service
}

func NewAccessAnalysisTaskHandler(service *Service) *AccessAnalysisTaskHandler {
	return &AccessAnalysisTaskHandler{service: service}
}

func (h *AccessAnalysisTaskHandler) Type() string { return ScheduledTaskTypeAccessAnalysisScan }

func (h *AccessAnalysisTaskHandler) Definition() scheduledtask.TaskDefinition {
	return scheduledtask.TaskDefinition{
		Type:              ScheduledTaskTypeAccessAnalysisScan,
		Label:             "访问分析扫描",
		Description:       "按站点访问分析设置定时扫描 access log，并写入访问趋势、路径、IP 和异常聚合结果。",
		SupportsManualRun: true,
		DefaultSchedule:   scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleDaily, Expr: "03:00", Timezone: "UTC"},
		ParamSchema: map[string]any{
			"site_id": map[string]any{"type": "string", "required": true, "label": "站点名称"},
			"range":   map[string]any{"type": "select", "options": []string{"today", "yesterday", "7d", "custom"}, "default": "today", "label": "扫描范围"},
			"from":    map[string]any{"type": "string", "required": false, "label": "开始日期"},
			"to":      map[string]any{"type": "string", "required": false, "label": "结束日期"},
		},
	}
}

func (h *AccessAnalysisTaskHandler) DefaultParams() json.RawMessage {
	return json.RawMessage(`{"site_id":"","range":"today"}`)
}

func (h *AccessAnalysisTaskHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	params, err := h.service.decodeAccessAnalysisParams(raw)
	if err != nil {
		return nil, err
	}
	return marshalAccessAnalysisParams(params)
}

func (h *AccessAnalysisTaskHandler) Run(ctx context.Context, task scheduledtask.Task, run scheduledtask.RunContext) error {
	params, err := h.service.decodeAccessAnalysisParams(task.ParamsJSON)
	if err != nil {
		return err
	}
	return h.service.RunScheduledScan(ctx, params, run)
}

func (s *Service) AttachScheduledTasks(taskSvc ScheduledTaskService) error {
	s.scheduledTaskSvc = taskSvc
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Register(NewAccessAnalysisTaskHandler(s))
}

func (s *Service) MigrateSettingsToTasks(ctx context.Context) error {
	if s.scheduledTaskSvc == nil {
		return nil
	}
	settings, err := s.repo.EnabledSettings()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, item := range settings {
		req, err := s.settingsToTaskRequest(&item)
		if err != nil {
			return err
		}
		// 访问分析迁移以站点设置为幂等来源，重启不会重复创建同一站点扫描任务。
		if _, err := s.scheduledTaskSvc.CreateSourceIfMissing(ctx, accessAnalysisSourceType, item.SiteID, false, nil, req); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RunScheduledScan(ctx context.Context, params AccessAnalysisParams, run scheduledtask.RunContext) error {
	logger := newAccessAnalysisLogger(s.taskLogDir)
	logger.write("info", fmt.Sprintf("开始访问分析扫描 run_id=%s, site_id=%s, range=%s", run.RunID, params.SiteID, params.Range))
	resp, err := s.scan(ctx, params.SiteID, ScanRequest{Range: params.Range, From: params.From, To: params.To}, "scheduled-access-analysis", "scheduler")
	if err != nil {
		logger.write("error", fmt.Sprintf("访问分析扫描失败 site_id=%s: %v", params.SiteID, err))
		return err
	}
	logger.write("info", fmt.Sprintf("访问分析扫描完成 site_id=%s, job_id=%s, scanned=%d, skipped=%d", params.SiteID, resp.JobID, resp.ScannedLines, resp.SkippedLines))
	return nil
}

func (s *Service) settingsToTaskRequest(settings *Settings) (scheduledtask.CreateTaskRequest, error) {
	if settings == nil {
		return scheduledtask.CreateTaskRequest{}, app.ErrBadRequestMsg("访问分析设置不能为空")
	}
	site, err := s.requireSite(settings.SiteID)
	if err != nil {
		return scheduledtask.CreateTaskRequest{}, err
	}
	params, err := marshalAccessAnalysisParams(AccessAnalysisParams{SiteID: settings.SiteID, Range: "today"})
	if err != nil {
		return scheduledtask.CreateTaskRequest{}, err
	}
	return scheduledtask.CreateTaskRequest{
		Type:              ScheduledTaskTypeAccessAnalysisScan,
		Name:              "访问分析扫描 - " + site.PrimaryDomain,
		Enabled:           settings.Enabled,
		Schedule:          scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleDaily, Expr: settings.ScanTime, Timezone: "UTC"},
		Params:            params,
		ConcurrencyPolicy: scheduledtask.ConcurrencySkip,
		MissedPolicy:      scheduledtask.MissedSkip,
		TimeoutSeconds:    300,
		MaxRetries:        0,
		RetryDelaySeconds: 60,
	}, nil
}

func (s *Service) syncSettingsTask(ctx context.Context, settings *Settings) error {
	if s.scheduledTaskSvc == nil {
		return nil
	}
	req, err := s.settingsToTaskRequest(settings)
	if err != nil {
		return err
	}
	_, err = s.scheduledTaskSvc.UpsertSource(ctx, accessAnalysisSourceType, settings.SiteID, false, req)
	return err
}

func (s *Service) applyTaskScheduleToSettings(ctx context.Context, settings *Settings) (*Settings, error) {
	if s.scheduledTaskSvc == nil || settings == nil {
		return settings, nil
	}
	task, err := s.scheduledTaskSvc.GetBySource(ctx, accessAnalysisSourceType, settings.SiteID)
	if err != nil || task == nil {
		return settings, err
	}
	// 旧设置接口继续返回 enabled/scan_time，但事实来源优先使用统一计划任务，避免页面展示过期周期。
	settings.Enabled = task.Enabled
	if task.ScheduleKind == scheduledtask.ScheduleDaily && task.ScheduleExpr != "" {
		settings.ScanTime = task.ScheduleExpr
	}
	return settings, nil
}

func (s *Service) decodeAccessAnalysisParams(raw json.RawMessage) (AccessAnalysisParams, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(raw), 16*1024))
	dec.DisallowUnknownFields()
	var params AccessAnalysisParams
	if err := dec.Decode(&params); err != nil {
		return AccessAnalysisParams{}, fmt.Errorf("访问分析扫描参数格式错误: %w", err)
	}
	params.SiteID = strings.TrimSpace(params.SiteID)
	params.Range = strings.TrimSpace(params.Range)
	params.From = strings.TrimSpace(params.From)
	params.To = strings.TrimSpace(params.To)
	if params.Range == "" {
		params.Range = "today"
	}
	if params.SiteID == "" {
		return AccessAnalysisParams{}, fmt.Errorf("站点名称不能为空")
	}
	if params.Range != "today" && params.Range != "yesterday" && params.Range != "7d" && params.Range != "custom" {
		return AccessAnalysisParams{}, fmt.Errorf("访问分析扫描范围必须是 today、yesterday、7d 或 custom")
	}
	if params.Range != "custom" {
		params.From = ""
		params.To = ""
	} else if params.From == "" || params.To == "" {
		return AccessAnalysisParams{}, fmt.Errorf("自定义扫描范围需要填写开始日期和结束日期")
	}
	if _, err := s.requireSite(params.SiteID); err != nil {
		return AccessAnalysisParams{}, err
	}
	return params, nil
}

func marshalAccessAnalysisParams(params AccessAnalysisParams) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return data, nil
}

type accessAnalysisLogger struct {
	path string
}

func newAccessAnalysisLogger(logDir string) accessAnalysisLogger {
	if logDir == "" {
		return accessAnalysisLogger{}
	}
	return accessAnalysisLogger{path: filepath.Join(logDir, accessAnalysisLogFile)}
}

func (l accessAnalysisLogger) write(level, message string) {
	if l.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), level, message))
}
