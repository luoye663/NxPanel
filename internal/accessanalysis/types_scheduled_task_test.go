package accessanalysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

type fakeAnalysisAgent struct{}

func (fakeAnalysisAgent) AccessAnalysisScan(ctx context.Context, req *AgentScanRequest) (*AgentScanResponse, error) {
	return &AgentScanResponse{ScannedLines: 7, SkippedLines: 1, Cursor: Cursor{Offset: 128}}, nil
}

func (fakeAnalysisAgent) AccessAnalysisFormatDetect(ctx context.Context, req *AgentFormatDetectRequest) (*FormatDetectResponse, error) {
	return &FormatDetectResponse{Format: string(FormatCombined), Parseable: true}, nil
}

func TestAccessAnalysisMigrateSettingsToTasks(t *testing.T) {
	database, svc, taskRepo, err := newAccessAnalysisTaskTestService()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	settings := defaultSettings("site_1")
	settings.Enabled = true
	settings.ScanTime = "04:15"
	if err := svc.repo.SaveSettings(settings); err != nil {
		t.Fatalf("写入访问分析设置失败: %v", err)
	}
	if err := svc.MigrateSettingsToTasks(context.Background()); err != nil {
		t.Fatalf("迁移访问分析设置失败: %v", err)
	}
	task, err := taskRepo.GetBySource(context.Background(), accessAnalysisSourceType, "site_1")
	if err != nil {
		t.Fatalf("查询迁移任务失败: %v", err)
	}
	if task == nil || task.Type != ScheduledTaskTypeAccessAnalysisScan || task.ScheduleKind != scheduledtask.ScheduleDaily || task.ScheduleExpr != "04:15" {
		t.Fatalf("迁移任务字段错误: %+v", task)
	}
	var params AccessAnalysisParams
	if err := json.Unmarshal(task.ParamsJSON, &params); err != nil {
		t.Fatalf("解析任务参数失败: %v", err)
	}
	if params.SiteID != "site_1" || params.Range != "today" {
		t.Fatalf("任务参数错误: %+v", params)
	}
	if err := svc.MigrateSettingsToTasks(context.Background()); err != nil {
		t.Fatalf("第二次迁移应保持幂等: %v", err)
	}
	tasks, err := taskRepo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("查询任务列表失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("重复迁移不应创建新任务，实际数量: %d", len(tasks))
	}
}

func TestAccessAnalysisSaveSettingsSyncsTask(t *testing.T) {
	database, svc, taskRepo, err := newAccessAnalysisTaskTestService()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	settings := defaultSettings("site_1")
	settings.Enabled = true
	settings.ScanTime = "05:20"
	if _, err := svc.SaveSettings("site_1", settings, "req_1"); err != nil {
		t.Fatalf("保存访问分析设置失败: %v", err)
	}
	task, err := taskRepo.GetBySource(context.Background(), accessAnalysisSourceType, "site_1")
	if err != nil {
		t.Fatalf("查询同步任务失败: %v", err)
	}
	if task == nil || !task.Enabled || task.ScheduleExpr != "05:20" {
		t.Fatalf("保存设置未同步创建启用任务: %+v", task)
	}
	settings.Enabled = false
	settings.ScanTime = "06:30"
	if _, err := svc.SaveSettings("site_1", settings, "req_2"); err != nil {
		t.Fatalf("保存禁用设置失败: %v", err)
	}
	task, err = taskRepo.GetBySource(context.Background(), accessAnalysisSourceType, "site_1")
	if err != nil {
		t.Fatalf("查询更新任务失败: %v", err)
	}
	if task == nil || task.Enabled || task.ScheduleExpr != "06:30" || task.Status != scheduledtask.TaskStatusDisabled {
		t.Fatalf("保存禁用设置未同步更新任务: %+v", task)
	}
	loaded, err := svc.Settings("site_1")
	if err != nil {
		t.Fatalf("读取访问分析设置失败: %v", err)
	}
	if loaded.Enabled || loaded.ScanTime != "06:30" {
		t.Fatalf("旧设置接口未返回计划任务事实状态: %+v", loaded)
	}
}

func TestAccessAnalysisScheduledRun(t *testing.T) {
	database, svc, _, err := newAccessAnalysisTaskTestService()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := svc.RunScheduledScan(context.Background(), AccessAnalysisParams{SiteID: "site_1", Range: "today"}, scheduledtask.RunContext{RunID: "run_1"}); err != nil {
		t.Fatalf("计划任务扫描应复用访问分析业务链路: %v", err)
	}
	jobs, err := svc.repo.Jobs("site_1", 1, 10)
	if err != nil {
		t.Fatalf("查询扫描任务失败: %v", err)
	}
	if len(jobs.Items) != 1 || jobs.Items[0].Trigger != "scheduler" || jobs.Items[0].Status != "success" {
		t.Fatalf("计划任务扫描 job 状态错误: %+v", jobs.Items)
	}
}

func newAccessAnalysisTaskTestService() (*sql.DB, *Service, *scheduledtask.Repo, error) {
	database, err := db.Open(":memory:")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := db.RunMigrations(database); err != nil {
		database.Close()
		return nil, nil, nil, err
	}
	site := &repo.Site{ID: "site_1", PrimaryDomain: "example.com", DomainsJSON: `[]`, Status: "enabled", HTTPPort: 80, HTTPSPort: 443, RootPath: "/www/example.com", AccessLogPath: "/var/log/nginx/example.access.log", ErrorLogPath: "/var/log/nginx/example.error.log", ConfigPath: "/etc/nginx/conf.d/example.conf", EnabledPath: "/etc/nginx/conf.d/example.conf", RewritePath: "/etc/nginx/rewrite/example.conf"}
	if err := repo.NewSiteRepo(database).Create(site); err != nil {
		database.Close()
		return nil, nil, nil, err
	}
	taskRepo := scheduledtask.NewRepo(database)
	registry := scheduledtask.NewRegistry()
	runner := scheduledtask.NewRunner(taskRepo, registry, app.NewID("runner"), 1)
	taskSvc := scheduledtask.NewService(taskRepo, registry, runner, nil)
	svc := NewService(repo.NewSiteRepo(database), NewRepo(database), repo.NewOperationRepo(database), fakeAnalysisAgent{})
	if err := svc.AttachScheduledTasks(taskSvc); err != nil {
		database.Close()
		return nil, nil, nil, err
	}
	return database, svc, taskRepo, nil
}
