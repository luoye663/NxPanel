package sitebackup

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

type fakeSiteRepo struct {
	sites map[string]*repo.Site
}

func (r fakeSiteRepo) GetByID(id string) (*repo.Site, error) {
	return r.sites[id], nil
}

func TestMigrateSchedulesToTasks(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	scheduleRepo := repo.NewSiteBackupScheduleRepo(database)
	site := &repo.Site{ID: "site_1", PrimaryDomain: "example.com", DomainsJSON: `[]`, Status: "enabled", HTTPPort: 80, HTTPSPort: 443, RootPath: "/www/example.com"}
	if err := repo.NewSiteRepo(database).Create(site); err != nil {
		t.Fatalf("写入测试站点失败: %v", err)
	}
	lastRunAt := "2026-06-09T01:02:03Z"
	legacy := &repo.SiteBackupSchedule{
		SiteID: "site_1", Enabled: true, BackupType: "full", BackupDir: "/backup/site1", RetentionCount: 9,
		ScheduleType: "weekly", ScheduleTime: "03:30", Weekday: 2, MonthDay: 1, LastRunAt: &lastRunAt,
	}
	if err := scheduleRepo.Upsert(legacy); err != nil {
		t.Fatalf("写入旧定时备份配置失败: %v", err)
	}

	taskRepo := scheduledtask.NewRepo(database)
	// 测试场景只验证迁移建档，不启动调度引擎，避免后台 goroutine 影响断言稳定性。
	registry := scheduledtask.NewRegistry()
	runner := scheduledtask.NewRunner(taskRepo, registry, app.NewID("runner"), 1)
	taskSvc := scheduledtask.NewService(taskRepo, registry, runner, nil)

	svc := NewService(fakeSiteRepo{sites: map[string]*repo.Site{"site_1": site}}, repo.NewSiteBackupRepo(database), scheduleRepo, nil, nil, nil, "/panel", nil)
	if err := svc.AttachScheduledTasks(taskSvc); err != nil {
		t.Fatalf("注册站点备份 handler 失败: %v", err)
	}
	if err := svc.MigrateSchedulesToTasks(context.Background()); err != nil {
		t.Fatalf("迁移旧定时备份配置失败: %v", err)
	}

	task, err := taskRepo.GetBySource(context.Background(), siteBackupSourceType, "site_1")
	if err != nil {
		t.Fatalf("查询迁移后的计划任务失败: %v", err)
	}
	if task == nil {
		t.Fatal("迁移后应生成 site_backup 计划任务")
	}
	if task.Type != ScheduledTaskTypeSiteBackup || task.ScheduleKind != scheduledtask.ScheduleWeekly || task.ScheduleExpr != "2 03:30" {
		t.Fatalf("计划任务字段映射错误: type=%s kind=%s expr=%s", task.Type, task.ScheduleKind, task.ScheduleExpr)
	}
	if task.LastRunAt == nil || task.LastRunAt.Format(time.RFC3339) != lastRunAt {
		t.Fatalf("last_run_at 未正确迁移: %#v", task.LastRunAt)
	}
	var params SiteBackupParams
	if err := json.Unmarshal(task.ParamsJSON, &params); err != nil {
		t.Fatalf("解析迁移参数失败: %v", err)
	}
	if params.SiteID != "site_1" || params.BackupType != "full" || params.BackupDir != "/backup/site1" || params.RetentionCount != 9 {
		t.Fatalf("站点备份参数映射错误: %+v", params)
	}

	if err := svc.MigrateSchedulesToTasks(context.Background()); err != nil {
		t.Fatalf("第二次迁移应保持幂等: %v", err)
	}
	tasks, err := taskRepo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("查询计划任务列表失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("重复迁移不应创建新任务，实际数量: %d", len(tasks))
	}
}
