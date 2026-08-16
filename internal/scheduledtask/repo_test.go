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

func TestRepoBeginRunActiveLockSurvivesDisplayStatusEdit(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	task := testTask("lock-after-edit", time.Now().UTC().Add(-time.Minute))
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, _, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC()); err != nil || !locked {
		t.Fatalf("first BeginRun() locked=%v err=%v", locked, err)
	}
	current, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Status = TaskStatusIdle
	current.Name = "edited while running"
	if err := r.Update(context.Background(), current, current.Version); err != nil {
		t.Fatal(err)
	}
	if _, run, locked, err := r.BeginRun(context.Background(), task.ID, TriggerManual, "runner-b", time.Now().UTC()); err != nil {
		t.Fatal(err)
	} else if locked || run != nil {
		t.Fatalf("active lock overlapped after status edit: locked=%v run=%+v", locked, run)
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].RunnerID != "runner-a" || runs[0].Status != RunStatusRunning {
		t.Fatalf("unexpected runs after denied overlap: runs=%+v err=%v", runs, err)
	}
}

func TestRepoBeginRunFailsClosedForOwnedMalformedExpiry(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	for _, tc := range []struct {
		id     string
		expiry any
	}{
		{id: "owned-null-expiry", expiry: nil},
		{id: "owned-malformed-expiry", expiry: "not-a-timestamp"},
	} {
		task := testTask(tc.id, time.Now().UTC().Add(-time.Minute))
		if err := r.Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE scheduled_tasks SET status = 'idle', locked_by = 'unknown-owner', locked_until = ? WHERE id = ?`, tc.expiry, tc.id); err != nil {
			t.Fatal(err)
		}
		if _, run, locked, err := r.BeginRun(context.Background(), tc.id, TriggerManual, "runner-b", time.Now().UTC()); err != nil {
			t.Fatal(err)
		} else if locked || run != nil {
			t.Fatalf("%s claim did not fail closed: locked=%v run=%+v", tc.id, locked, run)
		}
	}
}

func TestRepoBeginRunManualRecoversExpiredLockBeforeClaim(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	now := time.Now().UTC()
	task := testTask("manual-expired-takeover", now.Add(-time.Hour))
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	_, oldRun, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "dead-runner", now.Add(-2*time.Minute))
	if err != nil || !locked {
		t.Fatalf("old BeginRun() locked=%v err=%v", locked, err)
	}
	claimed, newRun, locked, err := r.BeginRun(context.Background(), task.ID, TriggerManual, "manual-runner", now)
	if err != nil || !locked || claimed == nil || newRun == nil {
		t.Fatalf("manual recovery claim locked=%v task=%+v run=%+v err=%v", locked, claimed, newRun, err)
	}
	if newRun.ID == oldRun.ID {
		t.Fatal("manual recovery reused old run ID")
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("ListRuns() runs=%+v err=%v", runs, err)
	}
	statuses := map[string]int{}
	for _, run := range runs {
		statuses[run.Status]++
		if run.ID == oldRun.ID && (run.Status != RunStatusAbandoned || run.FinishedAt == nil) {
			t.Fatalf("old run was not abandoned: %+v", run)
		}
	}
	if statuses[RunStatusAbandoned] != 1 || statuses[RunStatusRunning] != 1 {
		t.Fatalf("manual recovery left unexpected runs: %+v", runs)
	}
	stored, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastRunID != newRun.ID || stored.LockedBy != "manual-runner" || stored.Status != TaskStatusRunning {
		t.Fatalf("new manual claim not installed: %+v", stored)
	}
}

func TestRepoBeginRunScheduledStaleVersionCommitsRecoveryThenReloadClaims(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	now := time.Now().UTC()
	task := testTask("scheduled-expired-stale", now.Add(-time.Hour))
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimedOld, oldRun, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "dead-runner", now.Add(-2*time.Minute))
	if err != nil || !locked {
		t.Fatalf("old BeginRun() locked=%v err=%v", locked, err)
	}
	if _, run, locked, err := r.BeginRunVersion(context.Background(), task.ID, TriggerSchedule, "scheduled-runner", claimedOld.Version, now); err != nil {
		t.Fatal(err)
	} else if locked || run != nil {
		t.Fatalf("stale scheduled version claimed after recovery: locked=%v run=%+v", locked, run)
	}
	afterRecovery, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRecovery.Version != claimedOld.Version+1 || afterRecovery.LockedBy != "" || afterRecovery.Status != TaskStatusError || afterRecovery.LastRunID != oldRun.ID {
		t.Fatalf("scheduled recovery was not committed: %+v", afterRecovery)
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != RunStatusAbandoned || runs[0].FinishedAt == nil {
		t.Fatalf("old scheduled run was not abandoned: runs=%+v err=%v", runs, err)
	}
	if _, newRun, locked, err := r.BeginRunVersion(context.Background(), task.ID, TriggerSchedule, "scheduled-runner", afterRecovery.Version, now); err != nil || !locked || newRun == nil {
		t.Fatalf("reloaded scheduled version locked=%v run=%+v err=%v", locked, newRun, err)
	}
	runs, err = r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("ListRuns() after reload runs=%+v err=%v", runs, err)
	}
	var running, abandoned int
	for _, run := range runs {
		switch run.Status {
		case RunStatusRunning:
			running++
		case RunStatusAbandoned:
			abandoned++
		}
	}
	if running != 1 || abandoned != 1 {
		t.Fatalf("scheduled recovery left orphan run: running=%d abandoned=%d runs=%+v", running, abandoned, runs)
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
	if outcome, err := repo.FinishRun(context.Background(), *claimedTask, *run, RunStatusSuccess, "", next, finishedAt); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	} else if !outcome.RunFinalized || !outcome.ScheduleUpdated || outcome.Stale {
		t.Fatalf("FinishRun() outcome = %+v, want normal finalization", outcome)
	}
	got, err := repo.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != TaskStatusIdle || got.NextRunAt == nil || !got.NextRunAt.Equal(next) || got.LastStatus == nil || *got.LastStatus != RunStatusSuccess {
		t.Fatalf("unexpected finished task: status=%s next=%v last=%v", got.Status, got.NextRunAt, got.LastStatus)
	}
}

func TestRepoFinishRunDoesNotOverwriteConcurrentEdit(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	task := testTask("finish-cas-edit", time.Now().UTC().Add(-time.Minute))
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimed, run, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC())
	if err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v err=%v", locked, err)
	}
	current, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "concurrent edit"
	current.ScheduleExpr = "2h"
	current.Status = TaskStatusIdle
	newNext := time.Now().UTC().Add(2 * time.Hour).Round(0)
	current.NextRunAt = &newNext
	if err := r.Update(context.Background(), current, current.Version); err != nil {
		t.Fatal(err)
	}
	outcome, err := r.FinishRun(context.Background(), *claimed, *run, RunStatusSuccess, "", time.Now().UTC().Add(time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.RunFinalized || outcome.ScheduleUpdated || !outcome.Stale {
		t.Fatalf("concurrent edit outcome = %+v, want finalized stale schedule", outcome)
	}
	got, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "concurrent edit" || got.ScheduleExpr != "2h" || got.NextRunAt == nil || !got.NextRunAt.Equal(newNext) || got.LockedBy != "" {
		t.Fatalf("concurrent edit was not preserved: %+v", got)
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != RunStatusSuccess || runs[0].TaskVersion != run.TaskVersion || runs[0].RunnerID != "runner-a" {
		t.Fatalf("run was not finalized with claim metadata: runs=%+v err=%v", runs, err)
	}
}

func TestRepoFinishRunDoesNotReenableConcurrentDisable(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	task := testTask("finish-cas-disable", time.Now().UTC().Add(-time.Minute))
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimed, run, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC())
	if err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v err=%v", locked, err)
	}
	if err := r.SetEnabled(context.Background(), task.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	outcome, err := r.FinishRun(context.Background(), *claimed, *run, RunStatusSuccess, "", time.Now().UTC().Add(time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.RunFinalized || outcome.ScheduleUpdated || !outcome.Stale {
		t.Fatalf("concurrent disable outcome = %+v, want finalized stale schedule", outcome)
	}
	got, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Status != TaskStatusDisabled || got.NextRunAt != nil || got.LockedBy != "" {
		t.Fatalf("disabled task was changed by completion: %+v", got)
	}
}

func TestRepoFinishRunRejectsAlreadyTerminalClaim(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	due := time.Now().UTC().Add(-time.Minute)
	task := testTask("finish-terminal-stale", due)
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimed, run, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "runner-a", time.Now().UTC())
	if err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v err=%v", locked, err)
	}
	before, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	abandonedAt := time.Now().UTC()
	if _, err := database.Exec(`UPDATE scheduled_task_runs SET status = 'abandoned', finished_at = ?, error_message = 'recovered' WHERE id = ?`, formatTime(abandonedAt), run.ID); err != nil {
		t.Fatal(err)
	}
	outcome, err := r.FinishRun(context.Background(), *claimed, *run, RunStatusSuccess, "", time.Now().UTC().Add(time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.RunFinalized || outcome.ScheduleUpdated || !outcome.Stale {
		t.Fatalf("already-terminal outcome = %+v, want stale claim", outcome)
	}
	after, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Status != before.Status || after.NextRunAt == nil || before.NextRunAt == nil || !after.NextRunAt.Equal(*before.NextRunAt) {
		t.Fatalf("stale finalization changed task schedule: before=%+v after=%+v", before, after)
	}
	if after.LockedBy != "" || after.LockedUntil != nil {
		t.Fatalf("stale finalization did not release exact matching lock: %+v", after)
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != RunStatusAbandoned || runs[0].ErrorMessage != "recovered" {
		t.Fatalf("already-terminal run was overwritten: runs=%+v err=%v", runs, err)
	}
}

func TestRepoExpiredLockRecoveryAbandonsRunTransactionally(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	now := time.Now().UTC()
	task := testTask("expired-run", now.Add(-time.Hour))
	task.TimeoutSeconds = 60
	if err := r.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	_, run, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "dead-runner", now.Add(-2*time.Minute))
	if err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v err=%v", locked, err)
	}
	changed, err := r.MarkExpiredRunningAbandoned(context.Background(), now)
	if err != nil || changed != 1 {
		t.Fatalf("MarkExpiredRunningAbandoned() changed=%d err=%v", changed, err)
	}
	got, err := r.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != TaskStatusError || got.LastStatus == nil || *got.LastStatus != RunStatusAbandoned || got.LockedBy != "" {
		t.Fatalf("expired task not recovered: %+v", got)
	}
	runs, err := r.ListRuns(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID || runs[0].Status != RunStatusAbandoned || runs[0].FinishedAt == nil {
		t.Fatalf("expired run not abandoned: runs=%+v err=%v", runs, err)
	}
}

func TestRepoExpiredRecoveryPreservesEditedAndDisabledTasks(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	now := time.Now().UTC()
	edited := testTask("expired-edited-idle", now.Add(-time.Minute))
	disabled := testTask("expired-disabled", now.Add(-time.Minute))
	for _, task := range []*Task{edited, disabled} {
		if err := r.Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		if _, _, locked, err := r.BeginRun(context.Background(), task.ID, TriggerSchedule, "dead-"+task.ID, now); err != nil || !locked {
			t.Fatalf("BeginRun(%s) locked=%v err=%v", task.ID, locked, err)
		}
	}
	current, err := r.Get(context.Background(), edited.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Status = TaskStatusIdle
	current.Name = "edited name"
	current.ScheduleExpr = "2h"
	current.ParamsJSON = []byte(`{"edited":true}`)
	editedNext := now.Add(2 * time.Hour).Round(0)
	current.NextRunAt = &editedNext
	if err := r.Update(context.Background(), current, current.Version); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(context.Background(), disabled.ID, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE scheduled_tasks SET locked_until = ? WHERE id IN (?, ?)`, formatTime(now.Add(-time.Second)), edited.ID, disabled.ID); err != nil {
		t.Fatal(err)
	}
	changed, err := r.MarkExpiredRunningAbandoned(context.Background(), now)
	if err != nil || changed != 2 {
		t.Fatalf("MarkExpiredRunningAbandoned() changed=%d err=%v", changed, err)
	}
	gotEdited, err := r.Get(context.Background(), edited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotEdited.Enabled || gotEdited.Status != TaskStatusError || gotEdited.Name != "edited name" || gotEdited.ScheduleExpr != "2h" || string(gotEdited.ParamsJSON) != `{"edited":true}` || gotEdited.NextRunAt == nil || !gotEdited.NextRunAt.Equal(editedNext) || gotEdited.LockedBy != "" || gotEdited.LockedUntil != nil {
		t.Fatalf("edited task was not safely recovered: %+v", gotEdited)
	}
	gotDisabled, err := r.Get(context.Background(), disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDisabled.Enabled || gotDisabled.Status != TaskStatusDisabled || gotDisabled.NextRunAt != nil || gotDisabled.LockedBy != "" || gotDisabled.LockedUntil != nil {
		t.Fatalf("disabled task was not preserved: %+v", gotDisabled)
	}
	for _, taskID := range []string{edited.ID, disabled.ID} {
		runs, err := r.ListRuns(context.Background(), taskID, 10)
		if err != nil || len(runs) != 1 || runs[0].Status != RunStatusAbandoned || runs[0].FinishedAt == nil {
			t.Fatalf("task %s exact run was not abandoned: runs=%+v err=%v", taskID, runs, err)
		}
	}
	var running int
	if err := database.QueryRow(`SELECT COUNT(*) FROM scheduled_task_runs WHERE task_id IN (?, ?) AND status = 'running'`, edited.ID, disabled.ID).Scan(&running); err != nil {
		t.Fatal(err)
	}
	if running != 0 {
		t.Fatalf("expired recovery left %d orphan running runs", running)
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
	if err := repo.SetEnabled(context.Background(), first.ID, false, nil); err != nil {
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
	if got.Version != task.Version+1 {
		t.Fatalf("enable should update state in one version increment: got=%d want=%d", got.Version, task.Version+1)
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
