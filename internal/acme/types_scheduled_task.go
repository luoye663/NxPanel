package acme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

const (
	ScheduledTaskTypeACMERenewal = "acme_renewal"
	acmeRenewalSourceType        = "system"
	acmeRenewalSourceID          = "acme_renewal"
	acmeRenewalLogFile           = "acme_renewal.log"
)

type ACMERenewalParams struct {
	RenewBeforeDays int      `json:"renew_before_days"`
	SiteIDs         []string `json:"site_ids,omitempty"`
}

type ACMEAutoRenewSummary struct {
	Checked int
	Success int
	Failed  int
}

type acmeRenewalRunner interface {
	DefaultRenewBeforeDays() int
	RunScheduledRenewal(ctx context.Context, params ACMERenewalParams, run scheduledtask.RunContext) (ACMEAutoRenewSummary, error)
}

type ACMERenewalTaskHandler struct {
	runner acmeRenewalRunner
}

func NewACMERenewalTaskHandler(runner acmeRenewalRunner) *ACMERenewalTaskHandler {
	return &ACMERenewalTaskHandler{runner: runner}
}

func (h *ACMERenewalTaskHandler) Type() string { return ScheduledTaskTypeACMERenewal }

func (h *ACMERenewalTaskHandler) Definition() scheduledtask.TaskDefinition {
	return scheduledtask.TaskDefinition{
		Type:              ScheduledTaskTypeACMERenewal,
		Label:             "SSL 自动续签检查",
		Description:       "定期扫描即将过期且开启自动续签的 ACME 证书订单，并复用现有申请与部署链路续签证书。",
		System:            true,
		SupportsManualRun: true,
		DefaultSchedule:   scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: "12h", Timezone: "UTC"},
		ParamSchema: map[string]any{
			"renew_before_days": map[string]any{"type": "number", "min": 1, "max": 90, "default": h.runner.DefaultRenewBeforeDays(), "label": "到期前续签天数"},
			"site_ids":          map[string]any{"type": "string_array", "required": false, "label": "限定站点 ID"},
		},
	}
}

func (h *ACMERenewalTaskHandler) DefaultParams() json.RawMessage {
	data, _ := marshalACMERenewalParams(ACMERenewalParams{RenewBeforeDays: h.runner.DefaultRenewBeforeDays()})
	return data
}

func (h *ACMERenewalTaskHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	params, err := decodeACMERenewalParams(raw, h.runner.DefaultRenewBeforeDays())
	if err != nil {
		return nil, err
	}
	return marshalACMERenewalParams(params)
}

func (h *ACMERenewalTaskHandler) Run(ctx context.Context, task scheduledtask.Task, run scheduledtask.RunContext) error {
	params, err := decodeACMERenewalParams(task.ParamsJSON, h.runner.DefaultRenewBeforeDays())
	if err != nil {
		return err
	}
	_, err = h.runner.RunScheduledRenewal(ctx, params, run)
	return err
}

func (svc *Service) AttachScheduledTasks(taskSvc ScheduledTaskService) error {
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Register(NewACMERenewalTaskHandler(svc))
}

func (svc *Service) EnsureRenewalSystemTask(ctx context.Context, taskSvc ScheduledTaskService) error {
	if taskSvc == nil {
		return nil
	}
	params, err := marshalACMERenewalParams(ACMERenewalParams{RenewBeforeDays: svc.DefaultRenewBeforeDays()})
	if err != nil {
		return err
	}
	// ACME 续签调度只由计划任务中心维护；新环境缺失系统任务时使用固定默认周期。
	scheduleExpr := "12h"
	// 系统任务只在缺失时创建，避免覆盖用户在计划任务中心调整过的周期和参数。
	_, err = taskSvc.CreateSourceIfMissing(ctx, acmeRenewalSourceType, acmeRenewalSourceID, true, nil, scheduledtask.CreateTaskRequest{
		Type:              ScheduledTaskTypeACMERenewal,
		Name:              "SSL 自动续签检查",
		Enabled:           true,
		Schedule:          scheduledtask.ScheduleDTO{Kind: scheduledtask.ScheduleInterval, Expr: scheduleExpr, Timezone: "UTC"},
		Params:            params,
		ConcurrencyPolicy: scheduledtask.ConcurrencySkip,
		MissedPolicy:      scheduledtask.MissedRunOnce,
		TimeoutSeconds:    1800,
		MaxRetries:        1,
		RetryDelaySeconds: 300,
	})
	return err
}

func (svc *Service) DefaultRenewBeforeDays() int {
	days := svc.cfg.ACME.AutoRenewDays
	if days <= 0 {
		return 30
	}
	if days > 90 {
		return 90
	}
	return days
}

func (svc *Service) RunScheduledRenewal(ctx context.Context, params ACMERenewalParams, run scheduledtask.RunContext) (ACMEAutoRenewSummary, error) {
	params, err := decodeACMERenewalParams(mustMarshalACMERenewalParams(params), svc.DefaultRenewBeforeDays())
	if err != nil {
		return ACMEAutoRenewSummary{}, err
	}
	logger := newACMERenewalLogger(svc.cfg.TaskLogDir())
	threshold := time.Now().Add(time.Duration(params.RenewBeforeDays) * 24 * time.Hour).Format(time.RFC3339)
	orders, err := svc.acmeRepo.ListExpiringOrders(threshold)
	if err != nil {
		logger.write("error", fmt.Sprintf("查询即将过期订单失败: %v", err))
		return ACMEAutoRenewSummary{}, err
	}
	siteFilter := make(map[string]struct{}, len(params.SiteIDs))
	for _, siteID := range params.SiteIDs {
		siteFilter[siteID] = struct{}{}
	}
	logger.write("info", fmt.Sprintf("开始 ACME 自动续签检查 run_id=%s, renew_before_days=%d, orders=%d", run.RunID, params.RenewBeforeDays, len(orders)))
	summary := ACMEAutoRenewSummary{}
	for _, order := range orders {
		if len(siteFilter) > 0 {
			if _, ok := siteFilter[order.SiteID]; !ok {
				continue
			}
		}
		summary.Checked++
		if err := ctx.Err(); err != nil {
			logger.write("error", fmt.Sprintf("续签检查被中断: %v", err))
			return summary, err
		}
		if err := svc.renewOrderForScheduledTask(ctx, order, logger); err != nil {
			summary.Failed++
			continue
		}
		summary.Success++
	}
	logger.write("info", fmt.Sprintf("ACME 自动续签检查完成: checked=%d, success=%d, failed=%d", summary.Checked, summary.Success, summary.Failed))
	if summary.Failed > 0 {
		return summary, fmt.Errorf("ACME 自动续签部分失败: checked=%d success=%d failed=%d", summary.Checked, summary.Success, summary.Failed)
	}
	return summary, nil
}

func (svc *Service) renewOrderForScheduledTask(ctx context.Context, order *repo.ACMEOrder, logger acmeRenewalLogger) error {
	var domains []string
	if order.DomainsJSON != "" {
		_ = json.Unmarshal([]byte(order.DomainsJSON), &domains)
	}
	if len(domains) == 0 {
		err := fmt.Errorf("订单域名为空")
		logger.write("error", fmt.Sprintf("跳过续签 order_id=%s: %v", order.ID, err))
		return err
	}
	logger.write("info", fmt.Sprintf("开始续签 order_id=%s, site_id=%s, domains=%v", order.ID, order.SiteID, domains))
	_, err := svc.ApplyCertificate(ctx, &ApplyRequest{SiteID: order.SiteID, Domains: domains, ChallengeType: order.ChallengeType, Email: order.Email}, "scheduled-acme-renewal")
	if err != nil {
		slog.Error("计划任务 ACME 自动续签失败", "order_id", order.ID, "error", err)
		logger.write("error", fmt.Sprintf("续签失败 order_id=%s: %v", order.ID, err))
		return err
	}
	if err := svc.acmeRepo.UpdateOrderRenewed(order.ID); err != nil {
		logger.write("error", fmt.Sprintf("更新旧订单续签时间失败 order_id=%s: %v", order.ID, err))
		return err
	}
	logger.write("info", fmt.Sprintf("续签已提交 order_id=%s, domains=%v", order.ID, domains))
	return nil
}

func decodeACMERenewalParams(raw json.RawMessage, defaultDays int) (ACMERenewalParams, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(raw), 16*1024))
	dec.DisallowUnknownFields()
	var params ACMERenewalParams
	if err := dec.Decode(&params); err != nil {
		return ACMERenewalParams{}, fmt.Errorf("ACME 自动续签参数格式错误: %w", err)
	}
	if params.RenewBeforeDays <= 0 {
		params.RenewBeforeDays = defaultDays
	}
	if params.RenewBeforeDays < 1 || params.RenewBeforeDays > 90 {
		return ACMERenewalParams{}, fmt.Errorf("到期前续签天数必须在 1-90 之间")
	}
	seen := make(map[string]struct{}, len(params.SiteIDs))
	cleaned := make([]string, 0, len(params.SiteIDs))
	for _, siteID := range params.SiteIDs {
		siteID = strings.TrimSpace(siteID)
		if siteID == "" {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		cleaned = append(cleaned, siteID)
	}
	params.SiteIDs = cleaned
	return params, nil
}

func marshalACMERenewalParams(params ACMERenewalParams) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func mustMarshalACMERenewalParams(params ACMERenewalParams) json.RawMessage {
	data, _ := json.Marshal(params)
	return data
}

type acmeRenewalLogger struct {
	path string
}

func newACMERenewalLogger(logDir string) acmeRenewalLogger {
	if logDir == "" {
		return acmeRenewalLogger{}
	}
	return acmeRenewalLogger{path: filepath.Join(logDir, acmeRenewalLogFile)}
}

func (l acmeRenewalLogger) write(level, message string) {
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

type ScheduledTaskService interface {
	Register(handler scheduledtask.TaskHandler) error
	CreateSourceIfMissing(ctx context.Context, sourceType, sourceID string, system bool, lastRunAt *time.Time, req scheduledtask.CreateTaskRequest) (bool, error)
}
