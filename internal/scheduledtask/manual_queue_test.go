package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
)

type blockingTaskHandler struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingTaskHandler) Type() string { return "blocking" }
func (*blockingTaskHandler) Definition() TaskDefinition {
	return TaskDefinition{Type: "blocking", SupportsManualRun: true}
}
func (*blockingTaskHandler) DefaultParams() json.RawMessage { return json.RawMessage(`{}`) }
func (*blockingTaskHandler) ValidateParams(raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (h *blockingTaskHandler) Run(ctx context.Context, _ Task, _ RunContext) error {
	h.started <- struct{}{}
	select {
	case <-h.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRunNowUsesBoundedQueueAndFixedWorker(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	repo := NewRepo(database)
	registry := NewRegistry()
	handler := &blockingTaskHandler{started: make(chan struct{}, 2), release: make(chan struct{})}
	if err := registry.Register(handler); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(repo, registry, "runner-test", 2)
	svc := NewService(context.Background(), repo, registry, runner, nil, 1, 1)
	defer svc.Close()
	task := &Task{ID: "task-1", Type: handler.Type(), Name: "blocking", Enabled: true, Status: TaskStatusIdle, ScheduleKind: ScheduleInterval, ScheduleExpr: "1h", Timezone: "UTC", ParamsJSON: json.RawMessage(`{}`), ConcurrencyPolicy: ConcurrencySkip, MissedPolicy: MissedRunOnce, TimeoutSeconds: 60}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunNow(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	<-handler.started
	if err := svc.RunNow(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunNow(context.Background(), task.ID); !manualQueueBusy(err) {
		t.Fatalf("third RunNow() error = %v, want BUSY", err)
	}
	runs, err := repo.ListRuns(context.Background(), task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("rejected queue admission created run state: runs=%d", len(runs))
	}
	close(handler.release)
}

func TestManualWorkerUsesRootCancellation(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	repo := NewRepo(database)
	registry := NewRegistry()
	handler := &blockingTaskHandler{started: make(chan struct{}, 1), release: make(chan struct{})}
	if err := registry.Register(handler); err != nil {
		t.Fatal(err)
	}
	svc := NewService(root, repo, registry, NewRunner(repo, registry, "runner-test", 1), nil, 1, 1)
	task := &Task{ID: "task-1", Type: handler.Type(), Name: "blocking", Enabled: true, Status: TaskStatusIdle, ScheduleKind: ScheduleInterval, ScheduleExpr: "1h", Timezone: "UTC", ParamsJSON: json.RawMessage(`{}`), ConcurrencyPolicy: ConcurrencySkip, MissedPolicy: MissedRunOnce, TimeoutSeconds: 60}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunNow(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	<-handler.started
	cancel()
	done := make(chan struct{})
	go func() {
		svc.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manual worker did not stop after root cancellation")
	}
	runs, err := repo.ListRuns(context.Background(), task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != RunStatusCancelled || runs[0].FinishedAt == nil {
		t.Fatalf("canceled run was not finalized: %+v", runs)
	}
	stored, err := repo.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == TaskStatusRunning || stored.LastStatus == nil || *stored.LastStatus != RunStatusCancelled || stored.LockedBy != "" {
		t.Fatalf("task remained running or locked after cancellation: %+v", stored)
	}
}

func manualQueueBusy(err error) bool {
	var appErr *app.AppError
	return errors.As(err, &appErr) && appErr.Code == app.ErrBusy
}
