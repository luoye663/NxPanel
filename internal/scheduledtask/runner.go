package scheduledtask

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const runFinalizationTimeout = 5 * time.Second

type Runner struct {
	repo     *Repo
	registry *Registry
	runnerID string
	sem      chan struct{}
	mu       sync.RWMutex
	reloader taskReloader
}

func NewRunner(repo *Repo, registry *Registry, runnerID string, maxConcurrent int) *Runner {
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	return &Runner{repo: repo, registry: registry, runnerID: runnerID, sem: make(chan struct{}, maxConcurrent)}
}

func (r *Runner) Run(ctx context.Context, taskID, trigger string) {
	r.RunVersion(ctx, taskID, trigger, 0)
}

func (r *Runner) SetReloader(reloader taskReloader) {
	r.mu.Lock()
	r.reloader = reloader
	r.mu.Unlock()
}

func (r *Runner) reload(taskID string) {
	r.mu.RLock()
	reloader := r.reloader
	r.mu.RUnlock()
	if reloader != nil {
		reloader.ReloadTask(taskID)
	}
}

func (r *Runner) RunVersion(ctx context.Context, taskID, trigger string, expectedVersion int) {
	defer r.reload(taskID)
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return
	}
	r.run(ctx, taskID, trigger, expectedVersion)
}

func (r *Runner) run(ctx context.Context, taskID, trigger string, expectedVersion int) {
	now := time.Now().UTC()
	task, run, locked, err := r.repo.BeginRunVersion(ctx, taskID, trigger, r.runnerID, expectedVersion, now)
	if err != nil {
		slog.Warn("计划任务抢锁失败", "task_id", taskID, "error", err)
		return
	}
	if !locked || task == nil || run == nil {
		return
	}
	status := RunStatusSuccess
	errText := ""
	finishedAt := time.Now().UTC()
	defer func() {
		if recovered := recover(); recovered != nil {
			status = RunStatusFailed
			errText = fmt.Sprintf("任务 panic: %v", recovered)
		}
		finishedAt = time.Now().UTC()
		next, nextErr := r.nextAfterFinish(*task, finishedAt)
		if nextErr != nil && errText == "" {
			status = RunStatusFailed
			errText = nextErr.Error()
		}
		finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), runFinalizationTimeout)
		defer cancelFinalize()
		outcome, err := r.repo.FinishRun(finalizeCtx, *task, *run, status, errText, next, finishedAt)
		if err != nil {
			slog.Warn("计划任务完成状态写入失败", "task_id", task.ID, "run_id", run.ID, "error", err)
		} else if outcome.Stale {
			slog.Debug("计划任务完成结果已过期", "task_id", task.ID, "run_id", run.ID, "run_finalized", outcome.RunFinalized)
		}
	}()
	handler, ok := r.registry.Get(task.Type)
	if !ok {
		status = RunStatusFailed
		errText = "计划任务类型未注册: " + task.Type
		return
	}
	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	// 所有任务都通过 timeout context 执行，避免业务卡死后长期占用锁。
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = handler.Run(runCtx, *task, RunContext{RunID: run.ID, Trigger: trigger, Attempt: run.Attempt})
	if err == nil {
		return
	}
	status = RunStatusFailed
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		status = RunStatusTimeout
	} else if errors.Is(runCtx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		status = RunStatusCancelled
	}
	errText = err.Error()
}

func (r *Runner) nextAfterFinish(task Task, finishedAt time.Time) (time.Time, error) {
	task.NextRunAt = nil
	compiled, err := CompileSchedule(task.ScheduleKind, task.ScheduleExpr, task.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return compiled.Next(finishedAt), nil
}
