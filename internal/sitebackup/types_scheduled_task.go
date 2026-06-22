package sitebackup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

const (
	ScheduledTaskTypeSiteBackup = "site_backup"
	siteBackupSourceType        = "site_backup_schedule"
)

type SiteBackupParams struct {
	SiteID         string `json:"site_id"`
	BackupType     string `json:"backup_type"`
	BackupDir      string `json:"backup_dir,omitempty"`
	RetentionCount int    `json:"retention_count"`
}

type ScheduledTaskService interface {
	Register(handler scheduledtask.TaskHandler) error
	GetBySource(ctx context.Context, sourceType, sourceID string) (*scheduledtask.Task, error)
	UpsertSource(ctx context.Context, sourceType, sourceID string, system bool, req scheduledtask.CreateTaskRequest) (scheduledtask.TaskListItem, error)
	CreateSourceIfMissing(ctx context.Context, sourceType, sourceID string, system bool, lastRunAt *time.Time, req scheduledtask.CreateTaskRequest) (bool, error)
}

type SiteBackupTaskHandler struct {
	service *Service
}

func NewSiteBackupTaskHandler(service *Service) *SiteBackupTaskHandler {
	return &SiteBackupTaskHandler{service: service}
}

func (h *SiteBackupTaskHandler) Type() string { return ScheduledTaskTypeSiteBackup }

func (h *SiteBackupTaskHandler) Definition() scheduledtask.TaskDefinition {
	return scheduledtask.TaskDefinition{
		Type:              ScheduledTaskTypeSiteBackup,
		Label:             "站点备份",
		Description:       "按计划创建站点配置、根目录或 SSL 证书备份，并自动清理超出保留数量的历史备份。",
		SupportsManualRun: true,
		DefaultSchedule:   scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleDaily, Expr: "02:00", Timezone: "UTC"},
		ParamSchema: map[string]any{
			"site_id":         map[string]any{"type": "string", "required": true, "label": "站点 ID"},
			"backup_type":     map[string]any{"type": "select", "options": []string{"config", "root", "ssl", "full"}, "default": "full", "label": "备份范围"},
			"backup_dir":      map[string]any{"type": "string", "required": false, "label": "备份目录"},
			"retention_count": map[string]any{"type": "number", "min": 1, "max": 365, "default": 7, "label": "保留数量"},
		},
	}
}

func (h *SiteBackupTaskHandler) DefaultParams() json.RawMessage {
	return json.RawMessage(`{"site_id":"","backup_type":"full","retention_count":7}`)
}

func (h *SiteBackupTaskHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	params, err := decodeSiteBackupParams(raw)
	if err != nil {
		return nil, err
	}
	if _, err := h.service.loadSite(params.SiteID); err != nil {
		return nil, err
	}
	return marshalSiteBackupParams(params)
}

func (h *SiteBackupTaskHandler) Run(ctx context.Context, task scheduledtask.Task, run scheduledtask.RunContext) error {
	params, err := decodeSiteBackupParams(task.ParamsJSON)
	if err != nil {
		return err
	}
	// 任务执行复用站点备份原有业务链路，继续写 operation 审计并由 Agent 做路径白名单校验。
	return h.service.RunScheduled(ctx, params.SiteID, params.BackupType, params.BackupDir, params.RetentionCount, run)
}

func (svc *Service) AttachScheduledTasks(taskSvc ScheduledTaskService) error {
	svc.scheduledTaskSvc = taskSvc
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Register(NewSiteBackupTaskHandler(svc))
}

func (svc *Service) MigrateSchedulesToTasks(ctx context.Context) error {
	if svc.scheduledTaskSvc == nil {
		return nil
	}
	items, err := svc.scheduleRepo.All()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, item := range items {
		req, lastRunAt, err := svc.scheduleToTaskRequest(item)
		if err != nil {
			return err
		}
		// 迁移使用 source_type/source_id 做幂等键，重启不会重复创建同一站点的备份任务。
		if _, err := svc.scheduledTaskSvc.CreateSourceIfMissing(ctx, siteBackupSourceType, item.SiteID, false, lastRunAt, req); err != nil {
			return err
		}
	}
	return nil
}

func (svc *Service) scheduleToTaskRequest(item *repo.SiteBackupSchedule) (scheduledtask.CreateTaskRequest, *time.Time, error) {
	if item == nil {
		return scheduledtask.CreateTaskRequest{}, nil, app.ErrBadRequestMsg("定时备份配置不能为空")
	}
	site, err := svc.loadSite(item.SiteID)
	if err != nil {
		return scheduledtask.CreateTaskRequest{}, nil, err
	}
	params, err := marshalSiteBackupParams(SiteBackupParams{SiteID: item.SiteID, BackupType: item.BackupType, BackupDir: item.BackupDir, RetentionCount: item.RetentionCount})
	if err != nil {
		return scheduledtask.CreateTaskRequest{}, nil, err
	}
	schedule := legacyScheduleToDTO(item)
	var lastRunAt *time.Time
	if item.LastRunAt != nil && *item.LastRunAt != "" {
		if parsed, err := time.Parse(time.RFC3339, *item.LastRunAt); err == nil {
			utc := parsed.UTC()
			lastRunAt = &utc
		}
	}
	return scheduledtask.CreateTaskRequest{
		Type:              ScheduledTaskTypeSiteBackup,
		Name:              "备份网站 - " + site.PrimaryDomain,
		Enabled:           item.Enabled,
		Schedule:          schedule,
		Params:            params,
		ConcurrencyPolicy: scheduledtask.ConcurrencySkip,
		MissedPolicy:      scheduledtask.MissedRunOnce,
		TimeoutSeconds:    1800,
		MaxRetries:        0,
		RetryDelaySeconds: 60,
	}, lastRunAt, nil
}

func (svc *Service) taskToScheduleResponse(task *scheduledtask.Task) (*ScheduleResponse, error) {
	if task == nil {
		return nil, nil
	}
	params, err := decodeSiteBackupParams(task.ParamsJSON)
	if err != nil {
		return nil, err
	}
	scheduleType, scheduleTime, weekday, monthDay := dtoToLegacySchedule(task.ScheduleKind, task.ScheduleExpr)
	lastRunAt := scheduledTaskTimeToString(task.LastRunAt)
	return &ScheduleResponse{Enabled: task.Enabled, BackupType: params.BackupType, BackupDir: params.BackupDir, RetentionCount: params.RetentionCount, ScheduleType: scheduleType, ScheduleTime: scheduleTime, Weekday: weekday, MonthDay: monthDay, LastRunAt: lastRunAt}, nil
}

func (svc *Service) saveScheduleTask(ctx context.Context, item *repo.SiteBackupSchedule) (*ScheduleResponse, error) {
	req, _, err := svc.scheduleToTaskRequest(item)
	if err != nil {
		return nil, err
	}
	result, err := svc.scheduledTaskSvc.UpsertSource(ctx, siteBackupSourceType, item.SiteID, false, req)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result.Params)
	if err != nil {
		return nil, err
	}
	task := &scheduledtask.Task{Enabled: result.Enabled, ScheduleKind: result.Schedule.Kind, ScheduleExpr: result.Schedule.Expr, ParamsJSON: data}
	return svc.taskToScheduleResponse(task)
}

func legacyScheduleToDTO(item *repo.SiteBackupSchedule) scheduledtask.ScheduleDTO {
	scheduleTime := item.ScheduleTime
	if scheduleTime == "" {
		scheduleTime = "02:00"
	}
	schedule := scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleDaily, Expr: scheduleTime, Timezone: "UTC"}
	switch item.ScheduleType {
	case scheduledtask.ScheduleWeekly:
		schedule.Kind = scheduledtask.ScheduleWeekly
		schedule.Expr = fmt.Sprintf("%d %s", item.Weekday, scheduleTime)
	case scheduledtask.ScheduleMonthly:
		schedule.Kind = scheduledtask.ScheduleMonthly
		schedule.Expr = fmt.Sprintf("%d %s", item.MonthDay, scheduleTime)
	}
	return schedule
}

func dtoToLegacySchedule(kind, expr string) (string, string, int, int) {
	scheduleType, scheduleTime, weekday, monthDay := "daily", "02:00", 1, 1
	fields := strings.Fields(expr)
	switch kind {
	case scheduledtask.ScheduleWeekly:
		scheduleType = "weekly"
		if len(fields) == 2 {
			_, _ = fmt.Sscanf(fields[0], "%d", &weekday)
			scheduleTime = fields[1]
		}
	case scheduledtask.ScheduleMonthly:
		scheduleType = "monthly"
		if len(fields) == 2 {
			_, _ = fmt.Sscanf(fields[0], "%d", &monthDay)
			scheduleTime = fields[1]
		}
	default:
		if expr != "" {
			scheduleTime = expr
		}
	}
	return scheduleType, scheduleTime, weekday, monthDay
}

func decodeSiteBackupParams(raw json.RawMessage) (SiteBackupParams, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(raw), 16*1024))
	dec.DisallowUnknownFields()
	var params SiteBackupParams
	if err := dec.Decode(&params); err != nil {
		return SiteBackupParams{}, fmt.Errorf("站点备份参数格式错误: %w", err)
	}
	params.SiteID = strings.TrimSpace(params.SiteID)
	params.BackupType = strings.TrimSpace(params.BackupType)
	if params.BackupType == "" {
		params.BackupType = "full"
	}
	params.BackupDir = strings.TrimSpace(params.BackupDir)
	if params.RetentionCount <= 0 {
		params.RetentionCount = 7
	}
	if params.SiteID == "" {
		return SiteBackupParams{}, fmt.Errorf("站点 ID 不能为空")
	}
	if !validBackupType(params.BackupType) {
		return SiteBackupParams{}, fmt.Errorf("备份类型只允许 config、root、ssl、full")
	}
	if params.RetentionCount < 1 || params.RetentionCount > 365 {
		return SiteBackupParams{}, fmt.Errorf("备份保留数量必须在 1-365 之间")
	}
	return params, nil
}

func marshalSiteBackupParams(params SiteBackupParams) (json.RawMessage, error) {
	params, err := decodeSiteBackupParams(mustMarshalSiteBackupParams(params))
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func mustMarshalSiteBackupParams(params SiteBackupParams) json.RawMessage {
	data, _ := json.Marshal(params)
	return data
}

func scheduledTaskTimeToString(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
