package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

const (
	ScheduledTaskTypeNginxLogRotation = "nginx_log_rotation"
	nginxLogRotationSourceType        = "system"
	nginxLogRotationSourceID          = "nginx_log_rotation"
	maxNginxLogRotationSize           = 10 * 1024 * 1024 * 1024

	defaultNginxLogRotationEnabled  = true
	defaultNginxLogRotationInterval = "1h"
	defaultNginxLogRotationMaxCount = 30
	defaultNginxLogRotationMaxAge   = "720h"
	defaultNginxLogRotationMinSize  = "100M"
)

type NginxLogRotationParams struct {
	MinSize  string `json:"min_size"`
	MaxCount int    `json:"max_count"`
	MaxAge   string `json:"max_age"`
}

type ScheduledTaskService interface {
	Register(handler scheduledtask.TaskHandler) error
	GetBySource(ctx context.Context, sourceType, sourceID string) (*scheduledtask.Task, error)
	CreateSourceIfMissing(ctx context.Context, sourceType, sourceID string, system bool, lastRunAt *time.Time, req scheduledtask.CreateTaskRequest) (bool, error)
	UpsertSource(ctx context.Context, sourceType, sourceID string, system bool, req scheduledtask.CreateTaskRequest) (scheduledtask.TaskListItem, error)
}

type NginxLogRotationTaskHandler struct {
	service *Service
}

func NewNginxLogRotationTaskHandler(service *Service) *NginxLogRotationTaskHandler {
	return &NginxLogRotationTaskHandler{service: service}
}

func (h *NginxLogRotationTaskHandler) Type() string { return ScheduledTaskTypeNginxLogRotation }

func (h *NginxLogRotationTaskHandler) Definition() scheduledtask.TaskDefinition {
	return scheduledtask.TaskDefinition{
		Type:              ScheduledTaskTypeNginxLogRotation,
		Label:             "Nginx 网站日志切割",
		Description:       "按计划切割 Nginx 站点 access/error 日志，随后执行 nginx -s reopen 并按数量和时间双条件清理历史日志。",
		System:            true,
		SupportsManualRun: true,
		DefaultSchedule:   scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: defaultNginxLogRotationInterval, Timezone: "UTC"},
		ParamSchema: map[string]any{
			"min_size":  map[string]any{"type": "string", "default": defaultNginxLogRotationMinSize, "label": "最小切割大小"},
			"max_count": map[string]any{"type": "number", "min": 1, "max": 1000, "default": defaultNginxLogRotationMaxCount, "label": "最大保留数量"},
			"max_age":   map[string]any{"type": "string", "default": defaultNginxLogRotationMaxAge, "label": "最小保留时间"},
		},
	}
}

func (h *NginxLogRotationTaskHandler) DefaultParams() json.RawMessage {
	data, _ := marshalNginxLogRotationParams(h.service.defaultNginxLogRotationParams())
	return data
}

func (h *NginxLogRotationTaskHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	params, err := h.service.decodeNginxLogRotationParams(raw)
	if err != nil {
		return nil, err
	}
	return marshalNginxLogRotationParams(params)
}

func (h *NginxLogRotationTaskHandler) Run(ctx context.Context, task scheduledtask.Task, run scheduledtask.RunContext) error {
	params, err := h.service.decodeNginxLogRotationParams(task.ParamsJSON)
	if err != nil {
		return err
	}
	return h.service.RunScheduledNginxLogRotation(ctx, params, run)
}

func (svc *Service) EnsureNginxLogRotationSystemTask(ctx context.Context) error {
	if svc.taskSvc == nil {
		return nil
	}
	req, err := svc.nginxLogRotationTaskRequest(defaultNginxLogRotationSettings())
	if err != nil {
		return err
	}
	_, err = svc.taskSvc.CreateSourceIfMissing(ctx, nginxLogRotationSourceType, nginxLogRotationSourceID, true, nil, req)
	return err
}

func (svc *Service) SyncNginxLogRotationSystemTask(ctx context.Context, settings LogRotateSettings) error {
	if svc.taskSvc == nil {
		return nil
	}
	// 设置页保存后只更新计划任务中心，避免继续把旧 log_rotate_* 字段写回 YAML。
	req, err := svc.nginxLogRotationTaskRequest(settings)
	if err != nil {
		return err
	}
	_, err = svc.taskSvc.UpsertSource(ctx, nginxLogRotationSourceType, nginxLogRotationSourceID, true, req)
	return err
}

func (svc *Service) RunScheduledNginxLogRotation(ctx context.Context, params NginxLogRotationParams, run scheduledtask.RunContext) error {
	if svc.agent == nil {
		return app.NewAppError(app.ErrAgentUnavailable, "Agent 未配置，无法执行 Nginx 日志切割", nil)
	}
	resp, err := svc.agent.NginxLogRotateRun(ctx, &agentclient.NginxLogRotateRunRequest{
		MinSize:  params.MinSize,
		MaxCount: params.MaxCount,
		MaxAge:   params.MaxAge,
	})
	if err != nil {
		return err
	}
	if !resp.ReopenOK {
		return fmt.Errorf("Nginx 日志切割 reopen 失败: %s", resp.Message)
	}
	return nil
}

func (svc *Service) nginxLogRotationTaskRequest(settings LogRotateSettings) (scheduledtask.CreateTaskRequest, error) {
	params, err := marshalNginxLogRotationParams(NginxLogRotationParams{
		MinSize:  settings.MinSize,
		MaxCount: settings.MaxCount,
		MaxAge:   settings.MaxAge,
	})
	if err != nil {
		return scheduledtask.CreateTaskRequest{}, err
	}
	interval := strings.TrimSpace(settings.Interval)
	if interval == "" {
		interval = defaultNginxLogRotationInterval
	}
	if _, err := scheduledtask.CompileSchedule(scheduledtask.ScheduleInterval, interval, "UTC"); err != nil {
		// 启动迁移遇到旧 YAML 异常值时回退默认周期，避免升级后服务无法启动。
		interval = defaultNginxLogRotationInterval
	}
	return scheduledtask.CreateTaskRequest{
		Type:              ScheduledTaskTypeNginxLogRotation,
		Name:              "Nginx 网站日志切割",
		Enabled:           settings.Enabled,
		Schedule:          scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: interval, Timezone: "UTC"},
		Params:            params,
		ConcurrencyPolicy: scheduledtask.ConcurrencySkip,
		MissedPolicy:      scheduledtask.MissedRunOnce,
		TimeoutSeconds:    300,
		MaxRetries:        0,
		RetryDelaySeconds: 60,
	}, nil
}

func (svc *Service) defaultNginxLogRotationParams() NginxLogRotationParams {
	return NginxLogRotationParams{MinSize: defaultNginxLogRotationMinSize, MaxCount: defaultNginxLogRotationMaxCount, MaxAge: defaultNginxLogRotationMaxAge}
}

func defaultNginxLogRotationSettings() LogRotateSettings {
	return LogRotateSettings{
		Enabled:  defaultNginxLogRotationEnabled,
		Interval: defaultNginxLogRotationInterval,
		MaxCount: defaultNginxLogRotationMaxCount,
		MaxAge:   defaultNginxLogRotationMaxAge,
		MinSize:  defaultNginxLogRotationMinSize,
	}
}

func (svc *Service) decodeNginxLogRotationParams(raw json.RawMessage) (NginxLogRotationParams, error) {
	defaults := svc.defaultNginxLogRotationParams()
	if len(raw) == 0 || string(raw) == "null" {
		return defaults, nil
	}
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(raw), 16*1024))
	dec.DisallowUnknownFields()
	params := defaults
	if err := dec.Decode(&params); err != nil {
		return NginxLogRotationParams{}, fmt.Errorf("Nginx 日志切割参数格式错误: %w", err)
	}
	params.MinSize = strings.TrimSpace(params.MinSize)
	params.MaxAge = strings.TrimSpace(params.MaxAge)
	if _, err := parseLogRotationSize(params.MinSize); err != nil {
		return NginxLogRotationParams{}, err
	}
	if params.MaxCount < 1 || params.MaxCount > 1000 {
		return NginxLogRotationParams{}, fmt.Errorf("max_count 必须在 1-1000 之间")
	}
	maxAge, err := time.ParseDuration(params.MaxAge)
	if err != nil {
		return NginxLogRotationParams{}, fmt.Errorf("max_age 格式无效，例如: 720h")
	}
	if maxAge < time.Hour || maxAge > 8760*time.Hour {
		return NginxLogRotationParams{}, fmt.Errorf("max_age 必须在 1h-8760h 之间")
	}
	return params, nil
}

func marshalNginxLogRotationParams(params NginxLogRotationParams) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	return json.RawMessage(data), err
}

func parseLogRotationSize(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("min_size 不能为空")
	}
	s := strings.ToUpper(strings.TrimSpace(value))
	multiplier := int64(1)
	if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}
	switch {
	case strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "K")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("min_size 格式无效，例如: 0, 100M, 1G")
	}
	size := n * multiplier
	if size > maxNginxLogRotationSize {
		return 0, fmt.Errorf("min_size 不能超过 10G")
	}
	return size, nil
}
