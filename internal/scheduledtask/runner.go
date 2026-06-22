package scheduledtask

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Runner struct {
	repo     *Repo
	registry *Registry
	runnerID string
	sem      chan struct{}
}

func NewRunner(repo *Repo, registry *Registry, runnerID string, maxConcurrent int) *Runner {
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	return &Runner{repo: repo, registry: registry, runnerID: runnerID, sem: make(chan struct{}, maxConcurrent)}
}

func (r *Runner) Run(ctx context.Context, taskID, trigger string) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return
	}
	r.run(ctx, taskID, trigger)
}

func (r *Runner) run(ctx context.Context, taskID, trigger string) {
	now := time.Now().UTC()
	task, run, locked, err := r.repo.BeginRun(ctx, taskID, trigger, r.runnerID, now)
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
		if err := r.repo.FinishRun(context.Background(), *task, *run, status, errText, next, finishedAt); err != nil {
			slog.Warn("计划任务完成状态写入失败", "task_id", task.ID, "run_id", run.ID, "error", err)
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
