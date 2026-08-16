package scheduledtask

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/db"
)

type engineTestHandler struct {
	started chan string
	release <-chan struct{}
	runs    atomic.Int64
}

func (*engineTestHandler) Type() string { return "engine-test" }
func (*engineTestHandler) Definition() TaskDefinition {
	return TaskDefinition{Type: "engine-test", SupportsManualRun: true}
}
func (*engineTestHandler) DefaultParams() json.RawMessage { return json.RawMessage(`{}`) }
func (*engineTestHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (h *engineTestHandler) Run(ctx context.Context, task Task, _ RunContext) error {
	h.runs.Add(1)
	if h.started != nil {
		select {
		case h.started <- task.ID:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if h.release != nil {
		select {
		case <-h.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func newEngineTest(t *testing.T, handler TaskHandler, cfg EngineConfig) (*Repo, *Engine, func()) {
	t.Helper()
	database, err := db.Open(db.DSNFromPath(filepath.Join(t.TempDir(), "panel.db"), 5000))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	repo := NewRepo(database)
	registry := NewRegistry()
	if handler != nil {
		if err := registry.Register(handler); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngineWithConfig(ctx, repo, NewRunner(repo, registry, "engine-runner", 2), cfg)
	cleanup := func() {
		cancel()
		engine.Stop()
		database.Close()
	}
	return repo, engine, cleanup
}

func engineTask(id string, next time.Time) *Task {
	task := testTask(id, next)
	task.Type = "engine-test"
	task.ScheduleExpr = "1m"
	return task
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}

func engineItemCount(engine *Engine) int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return len(engine.items)
}

func engineHasTask(engine *Engine, taskID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	_, ok := engine.byID[taskID]
	return ok
}

func TestEngineIndexedHeapStableUnderRepeatedEdits(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{})
	defer cleanup()
	task := engineTask("stable", time.Now().UTC().Add(24*time.Hour))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		next := time.Now().UTC().Add(time.Duration(i+2) * time.Hour)
		if _, err := repo.db.Exec(`UPDATE scheduled_tasks SET next_run_at = ?, version = version + 1 WHERE id = ?`, formatTime(next), task.ID); err != nil {
			t.Fatal(err)
		}
		engine.ReloadTask(task.ID)
		waitFor(t, time.Second, func() bool {
			engine.mu.Lock()
			defer engine.mu.Unlock()
			item := engine.byID[task.ID]
			return len(engine.dirty) == 0 && len(engine.items) == 1 && item != nil && item.index == 0
		}, fmt.Sprintf("edit %d was not indexed", i))
	}
	if got := engineItemCount(engine); got != 1 {
		t.Fatalf("heap size=%d, want 1", got)
	}
}

func TestEngineRemovesDisabledAndDeletedTasks(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{})
	defer cleanup()
	for _, id := range []string{"disable", "delete"} {
		if err := repo.Create(context.Background(), engineTask(id, time.Now().UTC().Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetEnabled(context.Background(), "disable", false, nil); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTask("disable")
	if err := repo.Delete(context.Background(), "delete"); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTask("delete")
	waitFor(t, time.Second, func() bool { return !engineHasTask(engine, "disable") && !engineHasTask(engine, "delete") }, "disabled/deleted tasks remained in heap")
}

func TestEngineReloadBurstHasNoLoss(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{})
	defer cleanup()
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	const count = 80
	for i := range count {
		id := fmt.Sprintf("burst-%03d", i)
		if err := repo.Create(context.Background(), engineTask(id, time.Now().UTC().Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
		engine.ReloadTask(id)
	}
	waitFor(t, 2*time.Second, func() bool { return engineItemCount(engine) == count }, "reload burst lost task IDs")
}

func TestEngineReloadBeforeStartIsApplied(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{})
	defer cleanup()
	task := engineTask("before-start", time.Now().UTC().Add(time.Hour))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTask(task.ID)
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return engineHasTask(engine, task.ID) }, "pre-start reload was not applied")
}

func TestEnginePeriodicReconcileRecoversDirectDBChange(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{ReconcileInterval: 20 * time.Millisecond})
	defer cleanup()
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	task := engineTask("direct-db", time.Now().UTC().Add(time.Hour))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return engineHasTask(engine, task.ID) }, "periodic reconciliation did not discover direct DB task")
}

func TestEngineReconcileSkipsRunningAndActiveLockedTasks(t *testing.T) {
	handler := &engineTestHandler{started: make(chan string, 4)}
	repo, engine, cleanup := newEngineTest(t, handler, EngineConfig{ReconcileInterval: 15 * time.Millisecond})
	defer cleanup()
	now := time.Now().UTC()
	for _, id := range []string{"status-running", "active-lock"} {
		if err := repo.Create(context.Background(), engineTask(id, now.Add(-time.Minute))); err != nil {
			t.Fatal(err)
		}
		if _, _, locked, err := repo.BeginRun(context.Background(), id, TriggerSchedule, "other-runner", now); err != nil || !locked {
			t.Fatalf("BeginRun(%s) locked=%v err=%v", id, locked, err)
		}
	}
	if _, err := repo.db.Exec(`UPDATE scheduled_tasks SET status = 'idle' WHERE id = 'active-lock'`); err != nil {
		t.Fatal(err)
	}
	versions := map[string]int{}
	for _, id := range []string{"status-running", "active-lock"} {
		stored, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		versions[id] = stored.Version
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if handler.runs.Load() != 0 || len(engine.dispatch) != 0 || engineItemCount(engine) != 0 {
		t.Fatalf("active tasks were redispatched: runs=%d queued=%d heap=%d", handler.runs.Load(), len(engine.dispatch), engineItemCount(engine))
	}
	for id, version := range versions {
		stored, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Version != version {
			t.Fatalf("active task %s churned version: got=%d want=%d", id, stored.Version, version)
		}
		runs, err := repo.ListRuns(context.Background(), id, 10)
		if err != nil || len(runs) != 1 || runs[0].Status != RunStatusRunning {
			t.Fatalf("active task %s run records changed: runs=%+v err=%v", id, runs, err)
		}
	}
}

func TestEnginePeriodicReconcileRecoversExpiredRunAndReschedules(t *testing.T) {
	handler := &engineTestHandler{started: make(chan string, 1)}
	repo, engine, cleanup := newEngineTest(t, handler, EngineConfig{ReconcileInterval: 15 * time.Millisecond, DispatchWorkers: 1})
	defer cleanup()
	now := time.Now().UTC()
	task := engineTask("periodic-expired", now.Add(-time.Minute))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, _, locked, err := repo.BeginRun(context.Background(), task.ID, TriggerSchedule, "dead-runner", now); err != nil || !locked {
		t.Fatalf("BeginRun() locked=%v err=%v", locked, err)
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if engineHasTask(engine, task.ID) {
		t.Fatal("active running task was indexed before expiry")
	}
	if _, err := repo.db.Exec(`UPDATE scheduled_tasks SET locked_until = ? WHERE id = ?`, formatTime(now.Add(-time.Second)), task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-handler.started:
		if id != task.ID {
			t.Fatalf("unexpected task started: %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("periodic reconciliation did not reschedule expired task")
	}
	waitFor(t, 2*time.Second, func() bool {
		runs, err := repo.ListRuns(context.Background(), task.ID, 10)
		if err != nil || len(runs) != 2 {
			return false
		}
		statuses := map[string]int{}
		for _, run := range runs {
			statuses[run.Status]++
		}
		return statuses[RunStatusAbandoned] == 1 && statuses[RunStatusSuccess] == 1
	}, "expired run was not abandoned before recurring execution completed")
}

func TestEngineRejectsStaleHeapVersionAndNextRun(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{})
	defer cleanup()
	task := engineTask("stale-heap", time.Now().UTC().Add(-time.Second))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := engine.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := repo.db.Exec(`UPDATE scheduled_tasks SET next_run_at = ?, version = version + 1 WHERE id = ?`, formatTime(future), task.ID); err != nil {
		t.Fatal(err)
	}
	if full := engine.dispatchDue(); full {
		t.Fatal("empty dispatch queue reported full")
	}
	if len(engine.dispatch) != 0 {
		t.Fatal("stale heap item was dispatched")
	}
	engine.mu.Lock()
	item := engine.byID[task.ID]
	engine.mu.Unlock()
	if item == nil || item.version != task.Version+1 || !item.nextRunAt.Equal(future) {
		t.Fatalf("stale item was not refreshed: %+v", item)
	}
}

func TestEngineStartsAfterHandlerRegistrationAndReschedulesRecurringTask(t *testing.T) {
	handler := &engineTestHandler{started: make(chan string, 1)}
	repo, engine, cleanup := newEngineTest(t, handler, EngineConfig{DispatchWorkers: 1, DispatchQueueSize: 2})
	defer cleanup()
	task := engineTask("recurring", time.Now().UTC().Add(-time.Second))
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("registered handler did not run after engine start")
	}
	waitFor(t, 2*time.Second, func() bool {
		stored, err := repo.Get(context.Background(), task.ID)
		return err == nil && stored != nil && stored.Status != TaskStatusRunning && stored.NextRunAt != nil && stored.NextRunAt.After(time.Now()) && engineHasTask(engine, task.ID)
	}, "recurring task was not returned to heap after completion")
}

func TestEngineScheduledDispatchIsBounded(t *testing.T) {
	release := make(chan struct{})
	handler := &engineTestHandler{started: make(chan string, 4), release: release}
	repo, engine, cleanup := newEngineTest(t, handler, EngineConfig{DispatchWorkers: 1, DispatchQueueSize: 1})
	defer cleanup()
	for i := range 3 {
		if err := repo.Create(context.Background(), engineTask(fmt.Sprintf("bounded-%d", i), time.Now().UTC().Add(-time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("scheduled worker did not start")
	}
	waitFor(t, time.Second, func() bool { return engineItemCount(engine) >= 1 && len(engine.dispatch) == 1 }, "queue capacity did not retain excess due task")
	if handler.runs.Load() != 1 {
		t.Fatalf("running handlers=%d, want 1", handler.runs.Load())
	}
	close(release)
	waitFor(t, 2*time.Second, func() bool { return handler.runs.Load() == 3 }, "retained due task was not retried")
}

func TestEngineShutdownRace(t *testing.T) {
	repo, engine, cleanup := newEngineTest(t, &engineTestHandler{}, EngineConfig{ReconcileInterval: 10 * time.Millisecond})
	if err := repo.Create(context.Background(), engineTask("shutdown", time.Now().UTC().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				engine.ReloadTask("shutdown")
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		engine.Stop()
		close(done)
	}()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("engine shutdown leaked a goroutine")
	}
	for range 100 {
		engine.ReloadTask("shutdown")
	}
	engine.mu.Lock()
	dirtyCount := len(engine.dirty)
	engine.mu.Unlock()
	if dirtyCount != 0 {
		t.Fatalf("reloads after shutdown retained %d dirty IDs", dirtyCount)
	}
	cleanup()
}
