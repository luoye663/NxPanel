package scheduledtask

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/db"
)

func TestRepoBeginRunClaimsLockOnce(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := NewRepo(database)
	now := time.Now().UTC().Add(-time.Minute)
	task := testTask("lock-once", now)
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, run, locked, err := repo.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("BeginRun() error = %v", err)
	}
	if !locked || run == nil {
		t.Fatalf("first BeginRun locked=%v run=%v", locked, run)
	}
	_, run, locked, err = repo.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-b", time.Now().UTC())
	if err != nil {
		t.Fatalf("second BeginRun() error = %v", err)
	}
	if locked || run != nil {
		t.Fatalf("second BeginRun locked=%v run=%v, want unlocked", locked, run)
	}
}

func TestRepoBeginRunConcurrentClaimsOnlyOnce(t *testing.T) {
	database, err := db.Open(db.DSNFromPath(filepath.Join(t.TempDir(), "panel.db"), 5000))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := NewRepo(database)
	task := testTask("concurrent-lock", time.Now().UTC().Add(-time.Minute))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errors := make(chan error, 16)
	var lockedCount int64
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, locked, err := repo.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner", time.Now().UTC())
			if err != nil {
				errors <- err
				return
			}
			if locked {
				atomic.AddInt64(&lockedCount, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("BeginRun() concurrent error = %v", err)
	}
	if lockedCount != 1 {
		t.Fatalf("locked count = %d, want 1", lockedCount)
	}
}

func TestRepoFinishRunUpdatesNextRun(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := NewRepo(database)
	now := time.Now().UTC().Add(-time.Minute)
	task := testTask("finish", now)
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	claimedTask, run, locked, err := repo.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC())
	if err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v error=%v", locked, err)
	}
	next := time.Now().UTC().Add(time.Hour)
	finishedAt := time.Now().UTC()
	if err := repo.FinishRun(context.Background(), *claimedTask, *run, RunStatusSuccess, "", next, finishedAt); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
	got, err := repo.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != TaskStatusIdle || got.NextRunAt == nil || !got.NextRunAt.Equal(next) || got.LastStatus == nil || *got.LastStatus != RunStatusSuccess {
		t.Fatalf("unexpected finished task: status=%s next=%v last=%v", got.Status, got.NextRunAt, got.LastStatus)
	}
}

func TestRepoListAllKeepsStableOrderAfterUpdate(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	repo := NewRepo(database)
	now := time.Now().UTC().Add(time.Hour)
	first := testTask("first", now)
	second := testTask("second", now)
	if err := repo.Create(context.Background(), first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := repo.Create(context.Background(), second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if err := repo.SetEnabled(context.Background(), first.ID, false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	tasks, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != first.ID || tasks[1].ID != second.ID {
		t.Fatalf("ListAll order = %v, want first then second", taskIDs(tasks))
	}
}

func TestRepoPruneRunsAgeCountPerTaskAndKeepsRunning(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	r := NewRepo(database)
	now := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	for _, taskID := range []string{"prune-a", "prune-b", "prune-c"} {
		if err := r.Create(context.Background(), testTask(taskID, now.Add(time.Hour))); err != nil {
			t.Fatalf("Create(%s) error = %v", taskID, err)
		}
	}
	insertRun := func(id, taskID, status string, created time.Time, finished *time.Time) {
		t.Helper()
		if _, err := database.Exec(`INSERT INTO scheduled_task_runs
			(id, task_id, task_type, task_name, trigger, status, started_at, finished_at, created_at)
			VALUES (?, ?, 'test', 'test', 'manual', ?, ?, ?, ?)`, id, taskID, status, formatTime(created), formatTimePtr(finished), formatTime(created)); err != nil {
			t.Fatalf("insert run %s: %v", id, err)
		}
	}
	oldFinished := now.Add(-48 * time.Hour)
	recentFinished := now.Add(-time.Hour)
	insertRun("old-terminal", "prune-a", RunStatusSuccess, now.Add(-72*time.Hour), &oldFinished)
	insertRun("old-running", "prune-a", RunStatusRunning, now.Add(-72*time.Hour), nil)
	insertRun("tie-a", "prune-a", RunStatusSuccess, now.Add(-time.Hour), &recentFinished)
	insertRun("tie-b", "prune-a", RunStatusFailed, now.Add(-time.Hour), &recentFinished)
	insertRun("tie-c", "prune-a", RunStatusSuccess, now.Add(-time.Hour), &recentFinished)
	insertRun("other-a", "prune-b", RunStatusSuccess, now.Add(-time.Hour), &recentFinished)
	insertRun("other-b", "prune-b", RunStatusSuccess, now.Add(-time.Hour), &recentFinished)
	insertRun("long-recent", "prune-c", RunStatusSuccess, now.Add(-72*time.Hour), &recentFinished)
	insertRun("legacy-old", "prune-c", RunStatusFailed, now.Add(-48*time.Hour), nil)

	deleted, err := r.PruneRuns(context.Background(), now.Add(-24*time.Hour), 2)
	if err != nil {
		t.Fatalf("PruneRuns() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("PruneRuns() deleted = %d, want 3", deleted)
	}
	rows, err := database.Query(`SELECT id FROM scheduled_task_runs ORDER BY id`)
	if err != nil {
		t.Fatalf("query remaining runs: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan run: %v", err)
		}
		ids = append(ids, id)
	}
	want := []string{"long-recent", "old-running", "other-a", "other-b", "tie-b", "tie-c"}
	if len(ids) != len(want) {
		t.Fatalf("remaining runs = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("remaining runs = %v, want %v", ids, want)
		}
	}
}

func TestServiceSetEnabledPopulatesNextRunWhenMissing(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	repo := NewRepo(database)
	registry := NewRegistry()
	runner := NewRunner(repo, registry, "runner-test", 1)
	svc := NewService(context.Background(), repo, registry, runner, nil, 4, 1)
	t.Cleanup(svc.Close)
	task := testTask("enable-populates-next-run", time.Now().UTC().Add(time.Hour))
	task.Enabled = false
	task.Status = TaskStatusDisabled
	task.NextRunAt = nil
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.SetEnabled(context.Background(), task.ID, true); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	got, err := repo.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.Enabled || got.Status != TaskStatusIdle {
		t.Fatalf("enabled task state unexpected: enabled=%v status=%s", got.Enabled, got.Status)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("enabled task next_run_at should be populated in future, got=%v", got.NextRunAt)
	}
	if got.LastError != "" {
		t.Fatalf("enabled task should clear last_error, got=%q", got.LastError)
	}
}

func TestServiceSetEnabledKeepsExistingNextRun(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	repo := NewRepo(database)
	registry := NewRegistry()
	runner := NewRunner(repo, registry, "runner-test", 1)
	svc := NewService(context.Background(), repo, registry, runner, nil, 4, 1)
	t.Cleanup(svc.Close)
	next := time.Now().UTC().Add(2 * time.Hour).Round(0)
	task := testTask("enable-keeps-next-run", next)
	task.Enabled = false
	task.Status = TaskStatusDisabled
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.SetEnabled(context.Background(), task.ID, true); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	got, err := repo.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(next) {
		t.Fatalf("existing next_run_at should be preserved, got=%v want=%v", got.NextRunAt, next)
	}
}

func testTask(id string, next time.Time) *Task {
	return &Task{
		ID:                id,
		Type:              "test",
		Name:              "测试任务",
		Enabled:           true,
		Status:            TaskStatusIdle,
		ScheduleKind:      ScheduleInterval,
		ScheduleExpr:      "1h",
		Timezone:          "UTC",
		ParamsJSON:        []byte("{}"),
		ConcurrencyPolicy: ConcurrencySkip,
		MissedPolicy:      MissedRunOnce,
		TimeoutSeconds:    60,
		NextRunAt:         &next,
		Version:           1,
	}
}

func taskIDs(tasks []*Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
