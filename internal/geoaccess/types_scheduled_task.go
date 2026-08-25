package geoaccess

import (
	"context"
	"encoding/json"
	"time"

	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

const ScheduledTaskTypeGeoIPUpdate = "geoip_update"

type geoIPUpdateHandler struct{ service *Service }

func (h *geoIPUpdateHandler) Type() string { return ScheduledTaskTypeGeoIPUpdate }
func (h *geoIPUpdateHandler) Definition() scheduledtask.TaskDefinition {
	return scheduledtask.TaskDefinition{Type: ScheduledTaskTypeGeoIPUpdate, Label: "GeoLite2 国家库更新",
		Description: "下载并校验 GeoLite2 Country 数据库；存在启用策略时原子更新 Nginx 地域配置。",
		System:      true, SupportsManualRun: true,
		DefaultSchedule: scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: "24h", Timezone: "UTC"}}
}
func (h *geoIPUpdateHandler) DefaultParams() json.RawMessage { return json.RawMessage(`{}`) }
func (h *geoIPUpdateHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (h *geoIPUpdateHandler) Run(ctx context.Context, _ scheduledtask.Task, run scheduledtask.RunContext) error {
	settings, err := h.service.repo.GetGeoIPSettings()
	if err != nil || !settings.AutoUpdate {
		return err
	}
	_, err = h.service.UpdateDatabase(ctx, run.RunID)
	return err
}

type ScheduledTaskService interface {
	Register(handler scheduledtask.TaskHandler) error
	CreateSourceIfMissing(context.Context, string, string, bool, *time.Time, scheduledtask.CreateTaskRequest) (bool, error)
}

func (s *Service) AttachScheduledTasks(tasks ScheduledTaskService) error {
	if tasks == nil {
		return nil
	}
	return tasks.Register(&geoIPUpdateHandler{service: s})
}

func (s *Service) EnsureUpdateSystemTask(ctx context.Context, tasks ScheduledTaskService) error {
	if tasks == nil {
		return nil
	}
	_, err := tasks.CreateSourceIfMissing(ctx, "system", ScheduledTaskTypeGeoIPUpdate, true, nil, scheduledtask.CreateTaskRequest{
		Type: ScheduledTaskTypeGeoIPUpdate, Name: "GeoLite2 国家库更新", Enabled: true,
		Schedule: scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: "24h", Timezone: "UTC"},
		Params:   json.RawMessage(`{}`), ConcurrencyPolicy: scheduledtask.ConcurrencySkip,
		MissedPolicy: scheduledtask.MissedRunOnce, TimeoutSeconds: 600, MaxRetries: 1, RetryDelaySeconds: 300,
	})
	return err
}
