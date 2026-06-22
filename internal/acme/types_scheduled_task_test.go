package acme

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

type fakeACMERenewalRunner struct {
	defaultDays int
	seenParams  ACMERenewalParams
	result      ACMEAutoRenewSummary
	err         error
}

func (r *fakeACMERenewalRunner) DefaultRenewBeforeDays() int { return r.defaultDays }

func (r *fakeACMERenewalRunner) RunScheduledRenewal(ctx context.Context, params ACMERenewalParams, run scheduledtask.RunContext) (ACMEAutoRenewSummary, error) {
	r.seenParams = params
	return r.result, r.err
}

func TestACMERenewalTaskHandlerValidateAndRun(t *testing.T) {
	runner := &fakeACMERenewalRunner{defaultDays: 25, result: ACMEAutoRenewSummary{Checked: 2, Success: 1, Failed: 1}, err: errors.New("partial failed")}
	handler := NewACMERenewalTaskHandler(runner)
	validated, err := handler.ValidateParams(json.RawMessage(`{"renew_before_days":0,"site_ids":[" site_1 ","site_1",""]}`))
	if err != nil {
		t.Fatalf("参数校验应使用默认续签天数并清理站点列表: %v", err)
	}
	var params ACMERenewalParams
	if err := json.Unmarshal(validated, &params); err != nil {
		t.Fatalf("解析校验后参数失败: %v", err)
	}
	if params.RenewBeforeDays != 25 || len(params.SiteIDs) != 1 || params.SiteIDs[0] != "site_1" {
		t.Fatalf("参数规整结果错误: %+v", params)
	}
	err = handler.Run(context.Background(), scheduledtask.Task{ParamsJSON: validated}, scheduledtask.RunContext{RunID: "run_1"})
	if err == nil {
		t.Fatal("模拟单订单失败时 handler 应把摘要错误交给 Runner 记录")
	}
	if runner.seenParams.RenewBeforeDays != 25 || len(runner.seenParams.SiteIDs) != 1 {
		t.Fatalf("handler 未把规整参数传给 runner: %+v", runner.seenParams)
	}
}

func TestEnsureRenewalSystemTask(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	taskRepo := scheduledtask.NewRepo(database)
	registry := scheduledtask.NewRegistry()
	runner := scheduledtask.NewRunner(taskRepo, registry, app.NewID("runner"), 1)
	taskSvc := scheduledtask.NewService(taskRepo, registry, runner, nil)
	svc := &Service{cfg: &app.Config{ACME: app.ACMEConfig{AutoRenewDays: 45}}}
	if err := svc.AttachScheduledTasks(taskSvc); err != nil {
		t.Fatalf("注册 ACME 自动续签 handler 失败: %v", err)
	}
	if err := svc.EnsureRenewalSystemTask(context.Background(), taskSvc); err != nil {
		t.Fatalf("创建 ACME 自动续签系统任务失败: %v", err)
	}
	task, err := taskRepo.GetBySource(context.Background(), acmeRenewalSourceType, acmeRenewalSourceID)
	if err != nil {
		t.Fatalf("查询系统任务失败: %v", err)
	}
	if task == nil || !task.System || task.Type != ScheduledTaskTypeACMERenewal {
		t.Fatalf("系统任务字段错误: %+v", task)
	}
	if task.ScheduleKind != scheduledtask.ScheduleInterval || task.ScheduleExpr != "12h" {
		t.Fatalf("非法旧配置应回退默认周期，实际 kind=%s expr=%s", task.ScheduleKind, task.ScheduleExpr)
	}
	var params ACMERenewalParams
	if err := json.Unmarshal(task.ParamsJSON, &params); err != nil {
		t.Fatalf("解析系统任务参数失败: %v", err)
	}
	if params.RenewBeforeDays != 45 {
		t.Fatalf("系统任务未从旧配置继承续签天数: %+v", params)
	}
	if err := svc.EnsureRenewalSystemTask(context.Background(), taskSvc); err != nil {
		t.Fatalf("第二次创建应保持幂等: %v", err)
	}
	tasks, err := taskRepo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("查询计划任务列表失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("重复启动不应创建多个 ACME 系统任务，实际数量: %d", len(tasks))
	}
}
