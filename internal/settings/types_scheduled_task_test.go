package settings

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

func TestEnsureNginxLogRotationSystemTask_DefaultEnabled(t *testing.T) {
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
	svc := &Service{}

	if err := svc.AttachScheduledTasks(taskSvc); err != nil {
		t.Fatalf("注册 Nginx 日志切割 handler 失败: %v", err)
	}
	if err := svc.EnsureNginxLogRotationSystemTask(context.Background()); err != nil {
		t.Fatalf("创建 Nginx 日志切割系统任务失败: %v", err)
	}

	task, err := taskRepo.GetBySource(context.Background(), nginxLogRotationSourceType, nginxLogRotationSourceID)
	if err != nil {
		t.Fatalf("查询系统任务失败: %v", err)
	}
	if task == nil || !task.System || task.Type != ScheduledTaskTypeNginxLogRotation {
		t.Fatalf("系统任务字段错误: %+v", task)
	}
	if !task.Enabled || task.Status != scheduledtask.TaskStatusIdle || task.NextRunAt == nil {
		t.Fatalf("首次安装默认应启用并生成下次执行时间: %+v", task)
	}
	if task.ScheduleKind != scheduledtask.ScheduleInterval || task.ScheduleExpr != defaultNginxLogRotationInterval || task.Timezone != "UTC" {
		t.Fatalf("默认调度错误: kind=%s expr=%s timezone=%s", task.ScheduleKind, task.ScheduleExpr, task.Timezone)
	}

	var params NginxLogRotationParams
	if err := json.Unmarshal(task.ParamsJSON, &params); err != nil {
		t.Fatalf("解析系统任务参数失败: %v", err)
	}
	if params.MinSize != defaultNginxLogRotationMinSize || params.MaxCount != defaultNginxLogRotationMaxCount || params.MaxAge != defaultNginxLogRotationMaxAge {
		t.Fatalf("默认参数错误: %+v", params)
	}

	if err := svc.EnsureNginxLogRotationSystemTask(context.Background()); err != nil {
		t.Fatalf("第二次创建应保持幂等: %v", err)
	}
	tasks, err := taskRepo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("查询计划任务列表失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("重复启动不应创建多个系统任务，实际数量: %d", len(tasks))
	}
}
