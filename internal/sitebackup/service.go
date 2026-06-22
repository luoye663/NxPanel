package sitebackup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
	"github.com/luoye663/nxpanel/internal/sse"
)

type siteRepo interface {
	GetByID(id string) (*repo.Site, error)
}

type sslRepo interface {
	GetBySiteID(siteID string) (*repo.SiteSSL, error)
	Upsert(s *repo.SiteSSL) error
}

type opRepo interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

type agentClient interface {
	SiteBackupCreate(ctx context.Context, req *agentclient.SiteBackupCreateRequest) (*agentclient.SiteBackupCreateResponse, error)
	SiteBackupDownload(ctx context.Context, path string) (*http.Response, error)
	SiteBackupRestore(ctx context.Context, req *agentclient.SiteBackupRestoreRequest) error
	SiteBackupRemove(ctx context.Context, path string) error
	SSLInspectFiles(ctx context.Context, req *agentclient.SSLInspectFilesRequest) (*agentclient.SSLInspectResponse, error)
}

type Service struct {
	siteRepo         siteRepo
	backupRepo       *repo.SiteBackupRepo
	scheduleRepo     *repo.SiteBackupScheduleRepo
	sslRepo          sslRepo
	opRepo           opRepo
	agent            agentClient
	panelDir         string
	taskLogDir       string
	hub              *sse.Hub
	scheduledTaskSvc ScheduledTaskService
	tasksMu          sync.RWMutex
	tasks            map[string]*TaskResponse
}

type BackupResponse struct {
	ID         string  `json:"id"`
	SiteID     string  `json:"site_id"`
	BackupType string  `json:"backup_type"`
	Name       string  `json:"name"`
	SizeBytes  int64   `json:"size_bytes"`
	Status     string  `json:"status"`
	Message    string  `json:"message"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	FinishedAt *string `json:"finished_at"`
}

type CreateRequest struct {
	BackupType string `json:"backup_type"`
	Name       string `json:"name"`
	BackupDir  string `json:"backup_dir"`
}

type RestoreRequest struct {
	RestoreConfig bool `json:"restore_config"`
	RestoreRoot   bool `json:"restore_root"`
	RestoreSSL    bool `json:"restore_ssl"`
}

type TaskResponse struct {
	TaskID    string          `json:"task_id"`
	StreamID  string          `json:"stream_id"`
	Status    string          `json:"status"`
	Action    string          `json:"action"`
	Backup    *BackupResponse `json:"backup,omitempty"`
	Message   string          `json:"message"`
	Error     string          `json:"error,omitempty"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

type ScheduleResponse struct {
	Enabled        bool    `json:"enabled"`
	BackupType     string  `json:"backup_type"`
	BackupDir      string  `json:"backup_dir"`
	RetentionCount int     `json:"retention_count"`
	ScheduleType   string  `json:"schedule_type"`
	ScheduleTime   string  `json:"schedule_time"`
	Weekday        int     `json:"weekday"`
	MonthDay       int     `json:"month_day"`
	LastRunAt      *string `json:"last_run_at"`
}

type SaveScheduleRequest struct {
	Enabled        bool   `json:"enabled"`
	BackupType     string `json:"backup_type"`
	BackupDir      string `json:"backup_dir"`
	RetentionCount int    `json:"retention_count"`
	ScheduleType   string `json:"schedule_type"`
	ScheduleTime   string `json:"schedule_time"`
	Weekday        int    `json:"weekday"`
	MonthDay       int    `json:"month_day"`
	LastRunAt      *string `json:"last_run_at"`
}

func NewService(siteRepo siteRepo, backupRepo *repo.SiteBackupRepo, scheduleRepo *repo.SiteBackupScheduleRepo, sslRepo sslRepo, opRepo opRepo, agent agentClient, panelDir string, hub *sse.Hub) *Service {
	return &Service{siteRepo: siteRepo, backupRepo: backupRepo, scheduleRepo: scheduleRepo, sslRepo: sslRepo, opRepo: opRepo, agent: agent, panelDir: panelDir, hub: hub, tasks: make(map[string]*TaskResponse)}
}

func (svc *Service) SetTaskLogDir(dir string) {
	svc.taskLogDir = dir
}

func (svc *Service) List(siteID string) ([]*BackupResponse, error) {
	if err := svc.ensureSite(siteID); err != nil {
		return nil, err
	}
	backups, err := svc.backupRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*BackupResponse, 0, len(backups))
	for _, backup := range backups {
		result = append(result, toResponse(backup))
	}
	return result, nil
}

func (svc *Service) StartCreate(siteID string, req *CreateRequest, requestID string) (*TaskResponse, error) {
	if _, err := svc.loadSite(siteID); err != nil {
		return nil, err
	}
	task := svc.newTask("backup.create")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		backup, err := svc.Create(ctx, siteID, req, requestID)
		if err != nil {
			svc.finishTask(task.TaskID, "failed", "备份创建失败", err.Error(), nil)
			return
		}
		svc.finishTask(task.TaskID, "success", "备份创建完成", "", backup)
	}()
	return task, nil
}

func (svc *Service) Create(ctx context.Context, siteID string, req *CreateRequest, requestID string) (*BackupResponse, error) {
	site, err := svc.loadSite(siteID)
	if err != nil {
		return nil, err
	}
	backupType := strings.TrimSpace(req.BackupType)
	if !validBackupType(backupType) {
		return nil, app.NewAppError(app.ErrValidationFailed, "备份类型只允许 config、root、ssl、full", nil)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultBackupName(site.PrimaryDomain, backupType)
	}
	backupID := app.NewID("sitebak")
	backupDir := strings.TrimSpace(req.BackupDir)
	if backupDir == "" {
		backupDir = filepath.Join(svc.panelDir, "site-backups", site.PrimaryDomain)
	}
	backupPath := filepath.Join(backupDir, backupID+".tar.gz")
	backup := &repo.SiteBackup{ID: backupID, SiteID: siteID, BackupType: backupType, Name: name, BackupPath: backupPath, Status: "pending"}
	if err := svc.backupRepo.Create(backup); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	opID := svc.createOperation("site.backup.create", site, requestID, fmt.Sprintf("创建站点备份 %s", name))
	resp, err := svc.agent.SiteBackupCreate(ctx, &agentclient.SiteBackupCreateRequest{
		SiteID: site.ID, PrimaryDomain: site.PrimaryDomain, BackupType: backupType, OutputPath: backupPath,
		ConfigPaths: svc.configPaths(site), RootPath: site.RootPath, SSLPaths: svc.sslPaths(site.ID),
	})
	if err != nil {
		_ = svc.backupRepo.MarkFinished(backupID, "failed", err.Error(), 0)
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "创建站点备份失败: "+err.Error()+"；如果备份位置不在白名单内，请在 agent.allowed_roots 中加入该目录后重启 agent", nil)
	}
	_ = svc.backupRepo.MarkFinished(backupID, "success", "", resp.Size)
	_ = svc.opRepo.UpdateStatus(opID, "success")
	backup.SizeBytes = resp.Size
	backup.Status = "success"
	backup.Message = ""
	return toResponse(backup), nil
}

func (svc *Service) Download(ctx context.Context, siteID, backupID string) (*http.Response, string, error) {
	backup, err := svc.loadOwnedBackup(siteID, backupID)
	if err != nil {
		return nil, "", err
	}
	resp, err := svc.agent.SiteBackupDownload(ctx, backup.BackupPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "下载站点备份失败: "+err.Error(), nil)
	}
	return resp, safeDownloadName(backup), nil
}

func (svc *Service) StartRestore(siteID, backupID string, req *RestoreRequest, requestID string) (*TaskResponse, error) {
	if _, err := svc.loadOwnedBackup(siteID, backupID); err != nil {
		return nil, err
	}
	task := svc.newTask("backup.restore")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := svc.Restore(ctx, siteID, backupID, req, requestID); err != nil {
			svc.finishTask(task.TaskID, "failed", "备份恢复失败", err.Error(), nil)
			return
		}
		svc.finishTask(task.TaskID, "success", "备份恢复完成", "", nil)
	}()
	return task, nil
}

func (svc *Service) Restore(ctx context.Context, siteID, backupID string, req *RestoreRequest, requestID string) error {
	site, err := svc.loadSite(siteID)
	if err != nil {
		return err
	}
	backup, err := svc.loadOwnedBackup(siteID, backupID)
	if err != nil {
		return err
	}
	restoreConfig, restoreRoot, restoreSSL := normalizeRestoreOptions(backup.BackupType, req)
	if !restoreConfig && !restoreRoot && !restoreSSL {
		return app.NewAppError(app.ErrValidationFailed, "至少选择一个恢复范围", nil)
	}
	opID := svc.createOperation("site.backup.restore", site, requestID, fmt.Sprintf("恢复站点备份 %s", backup.Name))
	err = svc.agent.SiteBackupRestore(ctx, &agentclient.SiteBackupRestoreRequest{
		SiteID: site.ID, BackupPath: backup.BackupPath, RestoreConfig: restoreConfig, RestoreRoot: restoreRoot, RestoreSSL: restoreSSL,
		ConfigPaths: svc.configPaths(site), RootPath: site.RootPath, SSLPaths: svc.sslPaths(site.ID), ReloadNginx: site.Status == "enabled",
	})
	if err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "恢复站点备份失败: "+err.Error(), nil)
	}
	if restoreSSL {
		svc.refreshSSLMetadata(ctx, site.ID)
	}
	_ = svc.opRepo.UpdateStatus(opID, "success")
	return nil
}

func (svc *Service) GetSchedule(siteID string) (*ScheduleResponse, error) {
	if err := svc.ensureSite(siteID); err != nil {
		return nil, err
	}
	if svc.scheduledTaskSvc != nil {
		task, err := svc.scheduledTaskSvc.GetBySource(context.Background(), siteBackupSourceType, siteID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if task != nil {
			return svc.taskToScheduleResponse(task)
		}
	}
	item, err := svc.scheduleRepo.GetBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if item == nil {
		return &ScheduleResponse{BackupType: "full", RetentionCount: 7, ScheduleType: "daily", ScheduleTime: "02:00", Weekday: 1, MonthDay: 1}, nil
	}
	return scheduleToResponse(item), nil
}

func (svc *Service) SaveSchedule(siteID string, req *SaveScheduleRequest) (*ScheduleResponse, error) {
	if err := svc.ensureSite(siteID); err != nil {
		return nil, err
	}
	item, err := normalizeSchedule(siteID, req)
	if err != nil {
		return nil, err
	}
	if err := svc.scheduleRepo.Upsert(item); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if svc.scheduledTaskSvc != nil {
		return svc.saveScheduleTask(context.Background(), item)
	}
	return scheduleToResponse(item), nil
}

func (svc *Service) TaskStream(taskID string) (*sse.Stream, error) {
	stream := svc.hub.GetStream("site-backup:" + taskID)
	if stream == nil {
		return nil, app.NewAppError(app.ErrNotFound, "备份任务不存在", nil)
	}
	return stream, nil
}

func (svc *Service) Delete(ctx context.Context, siteID, backupID, requestID string) error {
	site, err := svc.loadSite(siteID)
	if err != nil {
		return err
	}
	backup, err := svc.loadOwnedBackup(siteID, backupID)
	if err != nil {
		return err
	}
	opID := svc.createOperation("site.backup.delete", site, requestID, fmt.Sprintf("删除站点备份 %s", backup.Name))
	if err := svc.agent.SiteBackupRemove(ctx, backup.BackupPath); err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "删除站点备份失败: "+err.Error(), nil)
	}
	if err := svc.backupRepo.MarkDeleted(backupID); err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, err.Error(), "")
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	_ = svc.opRepo.UpdateStatus(opID, "success")
	return nil
}

func (svc *Service) RunScheduled(ctx context.Context, siteID, backupType, backupDir string, retentionCount int, run scheduledtask.RunContext) error {
	if retentionCount <= 0 {
		retentionCount = 7
	}
	logger := newSiteBackupTaskLogger(svc.taskLogDir)
	site, err := svc.loadSite(siteID)
	if err != nil {
		logger.write("error", fmt.Sprintf("站点备份启动失败 run_id=%s, site_id=%s: %v", run.RunID, siteID, err))
		return err
	}
	logger.write("info", fmt.Sprintf("开始站点备份 run_id=%s, site=%s, backup_type=%s, retention_count=%d", run.RunID, site.PrimaryDomain, backupType, retentionCount))
	backup, err := svc.Create(ctx, siteID, &CreateRequest{BackupType: backupType, BackupDir: backupDir, Name: "自动备份"}, "scheduler")
	if err != nil {
		logger.write("error", fmt.Sprintf("站点备份失败 run_id=%s, site=%s: %v", run.RunID, site.PrimaryDomain, err))
		return err
	}
	logger.write("info", fmt.Sprintf("站点备份创建完成 run_id=%s, site=%s, backup=%s, size=%d", run.RunID, site.PrimaryDomain, backup.Name, backup.SizeBytes))
	if err := svc.cleanupRetention(ctx, siteID, retentionCount); err != nil {
		logger.write("error", fmt.Sprintf("站点备份保留清理失败 run_id=%s, site=%s: %v", run.RunID, site.PrimaryDomain, err))
		return err
	}
	logger.write("info", fmt.Sprintf("站点备份任务完成 run_id=%s, site=%s", run.RunID, site.PrimaryDomain))
	return nil
}

func (svc *Service) cleanupRetention(ctx context.Context, siteID string, retentionCount int) error {
	backups, err := svc.backupRepo.ListSuccessfulBySiteID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for index, backup := range backups {
		if index < retentionCount {
			continue
		}
		if err := svc.agent.SiteBackupRemove(ctx, backup.BackupPath); err != nil {
			slog.Warn("定时备份保留清理删除文件失败", "backup_id", backup.ID, "error", err)
			continue
		}
		_ = svc.backupRepo.MarkDeleted(backup.ID)
	}
	return nil
}

func (svc *Service) newTask(action string) *TaskResponse {
	now := time.Now().UTC().Format(time.RFC3339)
	taskID := app.NewID("baktask")
	task := &TaskResponse{TaskID: taskID, StreamID: "site-backup:" + taskID, Status: "running", Action: action, Message: "任务已启动", CreatedAt: now, UpdatedAt: now}
	svc.tasksMu.Lock()
	svc.tasks[taskID] = task
	svc.tasksMu.Unlock()
	stream := svc.hub.CreateStream(task.StreamID)
	stream.PublishData(marshalTask(task))
	return task
}

func (svc *Service) finishTask(taskID, status, message, errText string, backup *BackupResponse) {
	now := time.Now().UTC().Format(time.RFC3339)
	svc.tasksMu.Lock()
	task := svc.tasks[taskID]
	if task != nil {
		task.Status = status
		task.Message = message
		task.Error = errText
		task.Backup = backup
		task.UpdatedAt = now
	}
	svc.tasksMu.Unlock()
	stream := svc.hub.CreateStream("site-backup:" + taskID)
	if task != nil {
		stream.PublishData(marshalTask(task))
	}
	stream.PublishDone("")
}

func (svc *Service) ensureSite(siteID string) error {
	_, err := svc.loadSite(siteID)
	return err
}

func (svc *Service) loadSite(siteID string) (*repo.Site, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	return site, nil
}

func (svc *Service) loadOwnedBackup(siteID, backupID string) (*repo.SiteBackup, error) {
	backup, err := svc.backupRepo.GetByID(backupID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if backup == nil || backup.SiteID != siteID || backup.Status == "deleted" {
		return nil, app.NewAppError(app.ErrNotFound, "站点备份不存在", nil)
	}
	return backup, nil
}

func (svc *Service) configPaths(site *repo.Site) []string {
	paths := []string{site.ConfigPath, site.RewritePath, site.AccessLimitPath, site.HotlinkPath}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			result = append(result, path)
		}
	}
	return result
}

func (svc *Service) sslPaths(siteID string) []string {
	sslConfig, err := svc.sslRepo.GetBySiteID(siteID)
	if err != nil || sslConfig == nil || !sslConfig.Enabled {
		return nil
	}
	paths := []string{sslConfig.CertPath, sslConfig.KeyPath}
	result := make([]string, 0, 2)
	for _, path := range paths {
		if path != "" {
			result = append(result, path)
		}
	}
	return result
}

func (svc *Service) refreshSSLMetadata(ctx context.Context, siteID string) {
	sslConfig, err := svc.sslRepo.GetBySiteID(siteID)
	if err != nil || sslConfig == nil || sslConfig.CertPath == "" || sslConfig.KeyPath == "" {
		return
	}
	inspect, err := svc.agent.SSLInspectFiles(ctx, &agentclient.SSLInspectFilesRequest{CertPath: sslConfig.CertPath, KeyPath: sslConfig.KeyPath})
	if err != nil {
		return
	}
	sslConfig.Issuer = inspect.Issuer
	sslConfig.Subject = inspect.Subject
	sslConfig.CertSHA256 = inspect.CertSHA256
	sslConfig.KeySHA256 = inspect.KeySHA256
	// 证书恢复后只刷新证书元数据，私钥内容始终留在 agent 文件系统中，不进入 API 响应。
	dnsNamesJSON, _ := json.Marshal(inspect.DNSNames)
	sslConfig.DNSNamesJSON = string(dnsNamesJSON)
	_ = svc.sslRepo.Upsert(sslConfig)
}

type siteBackupTaskLogger struct {
	path string
}

func newSiteBackupTaskLogger(logDir string) siteBackupTaskLogger {
	if logDir == "" {
		return siteBackupTaskLogger{}
	}
	return siteBackupTaskLogger{path: filepath.Join(logDir, "site_backup.log")}
}

func (l siteBackupTaskLogger) write(level, message string) {
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

func (svc *Service) createOperation(action string, site *repo.Site, requestID, message string) string {
	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{ID: opID, Action: action, TargetType: "site", TargetID: site.ID, Status: "pending", RequestID: requestID, Actor: "admin", Message: message + " 站点 " + site.PrimaryDomain, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	return opID
}

func validBackupType(value string) bool {
	switch value {
	case "config", "root", "ssl", "full":
		return true
	default:
		return false
	}
}

func defaultBackupName(domain, backupType string) string {
	return fmt.Sprintf("%s-%s-%s", domain, backupType, time.Now().UTC().Format("20060102-150405"))
}

func safeDownloadName(backup *repo.SiteBackup) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, backup.Name)
	if !strings.HasSuffix(name, ".tar.gz") {
		name += ".tar.gz"
	}
	return name
}

func normalizeRestoreOptions(backupType string, req *RestoreRequest) (bool, bool, bool) {
	if req == nil || !req.RestoreConfig && !req.RestoreRoot && !req.RestoreSSL {
		switch backupType {
		case "config":
			return true, false, false
		case "root":
			return false, true, false
		case "ssl":
			return false, false, true
		case "full":
			return true, true, true
		}
	}
	return req.RestoreConfig, req.RestoreRoot, req.RestoreSSL
}

func normalizeSchedule(siteID string, req *SaveScheduleRequest) (*repo.SiteBackupSchedule, error) {
	backupType := strings.TrimSpace(req.BackupType)
	if backupType == "" {
		backupType = "full"
	}
	if !validBackupType(backupType) {
		return nil, app.NewAppError(app.ErrValidationFailed, "备份类型只允许 config、root、ssl、full", nil)
	}
	retention := req.RetentionCount
	if retention <= 0 {
		retention = 7
	}
	if retention > 365 {
		return nil, app.NewAppError(app.ErrValidationFailed, "备份保留数量不能超过 365", nil)
	}
	scheduleType := strings.TrimSpace(req.ScheduleType)
	if scheduleType == "" {
		scheduleType = "daily"
	}
	if scheduleType != "daily" && scheduleType != "weekly" && scheduleType != "monthly" {
		return nil, app.NewAppError(app.ErrValidationFailed, "定时类型只允许 daily、weekly、monthly", nil)
	}
	if !validScheduleTime(req.ScheduleTime) {
		return nil, app.NewAppError(app.ErrValidationFailed, "备份时间必须是 HH:MM 格式", nil)
	}
	weekday := req.Weekday
	if weekday < 0 || weekday > 6 {
		weekday = 1
	}
	monthDay := req.MonthDay
	if monthDay < 1 || monthDay > 31 {
		monthDay = 1
	}
	return &repo.SiteBackupSchedule{SiteID: siteID, Enabled: req.Enabled, BackupType: backupType, BackupDir: strings.TrimSpace(req.BackupDir), RetentionCount: retention, ScheduleType: scheduleType, ScheduleTime: req.ScheduleTime, Weekday: weekday, MonthDay: monthDay}, nil
}

func validScheduleTime(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func scheduleToResponse(item *repo.SiteBackupSchedule) *ScheduleResponse {
	return &ScheduleResponse{Enabled: item.Enabled, BackupType: item.BackupType, BackupDir: item.BackupDir, RetentionCount: item.RetentionCount, ScheduleType: item.ScheduleType, ScheduleTime: item.ScheduleTime, Weekday: item.Weekday, MonthDay: item.MonthDay, LastRunAt: item.LastRunAt}
}

func marshalTask(task *TaskResponse) string {
	data, err := json.Marshal(task)
	if err != nil {
		return `{"status":"failed","message":"任务状态序列化失败"}`
	}
	return string(data)
}

func toResponse(backup *repo.SiteBackup) *BackupResponse {
	return &BackupResponse{ID: backup.ID, SiteID: backup.SiteID, BackupType: backup.BackupType, Name: backup.Name, SizeBytes: backup.SizeBytes, Status: backup.Status, Message: backup.Message, CreatedAt: backup.CreatedAt, UpdatedAt: backup.UpdatedAt, FinishedAt: backup.FinishedAt}
}
