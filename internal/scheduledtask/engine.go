package scheduledtask

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultReconcileInterval = time.Minute
	defaultDispatchWorkers   = 2
	defaultDispatchQueueSize = 128
	dispatchQueueRetry       = 100 * time.Millisecond
)

type EngineConfig struct {
	ReconcileInterval time.Duration
	DispatchWorkers   int
	DispatchQueueSize int
}

type scheduledDispatch struct {
	taskID  string
	version int
}

type Engine struct {
	repo   *Repo
	runner *Runner

	mu       sync.Mutex
	items    taskHeap
	byID     map[string]*heapItem
	dirty    map[string]struct{}
	wake     chan struct{}
	dispatch chan scheduledDispatch

	reconcileInterval time.Duration
	dispatchWorkers   int
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	startOnce         sync.Once
	stopOnce          sync.Once
	startErr          error
}

func NewEngine(parent context.Context, repo *Repo, runner *Runner) *Engine {
	return NewEngineWithConfig(parent, repo, runner, EngineConfig{})
}

func NewEngineWithConfig(parent context.Context, repo *Repo, runner *Runner, cfg EngineConfig) *Engine {
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = defaultReconcileInterval
	}
	if cfg.DispatchWorkers <= 0 {
		cfg.DispatchWorkers = defaultDispatchWorkers
	}
	if cfg.DispatchQueueSize <= 0 {
		cfg.DispatchQueueSize = defaultDispatchQueueSize
	}
	ctx, cancel := context.WithCancel(parent)
	e := &Engine{
		repo: repo, runner: runner, byID: make(map[string]*heapItem), dirty: make(map[string]struct{}),
		wake: make(chan struct{}, 1), dispatch: make(chan scheduledDispatch, cfg.DispatchQueueSize),
		reconcileInterval: cfg.ReconcileInterval, dispatchWorkers: cfg.DispatchWorkers, ctx: ctx, cancel: cancel,
	}
	runner.SetReloader(e)
	return e
}

func (e *Engine) Start() error {
	e.startOnce.Do(func() {
		if err := e.reconcile(e.ctx); err != nil {
			e.startErr = err
			return
		}
		e.drainDirty()
		e.wg.Add(1 + e.dispatchWorkers)
		go e.loop()
		for range e.dispatchWorkers {
			go e.dispatchWorker()
		}
	})
	return e.startErr
}

func (e *Engine) Stop() {
	e.stopOnce.Do(e.cancel)
	e.wg.Wait()
	e.mu.Lock()
	clear(e.dirty)
	e.mu.Unlock()
}

// ReloadTask records the task before waking the loop. Calls made before Start
// remain in the dirty set and are applied by Start after its full reconciliation.
func (e *Engine) ReloadTask(taskID string) {
	if taskID == "" {
		return
	}
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.dirty[taskID] = struct{}{}
	e.mu.Unlock()
	e.notifyWake()
}

func (e *Engine) reconcile(ctx context.Context) error {
	now := time.Now().UTC()
	if _, err := e.repo.MarkExpiredRunningAbandoned(ctx, now); err != nil {
		return err
	}
	tasks, err := e.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		seen[task.ID] = struct{}{}
		e.loadTask(ctx, task, now)
	}
	e.mu.Lock()
	for taskID := range e.byID {
		if _, ok := seen[taskID]; !ok {
			e.removeLocked(taskID)
		}
	}
	e.mu.Unlock()
	return nil
}

func (e *Engine) loop() {
	defer e.wg.Done()
	reconcileTicker := time.NewTicker(e.reconcileInterval)
	defer reconcileTicker.Stop()
	var retryUntil time.Time
	for {
		nextAt, ok := e.peekNext()
		if !retryUntil.IsZero() && (!ok || retryUntil.After(nextAt)) {
			nextAt, ok = retryUntil, true
		}
		var timer *time.Timer
		var timerC <-chan time.Time
		if ok {
			delay := time.Until(nextAt)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			timerC = timer.C
		}
		select {
		case <-e.ctx.Done():
			stopTimer(timer)
			return
		case <-e.wake:
			stopTimer(timer)
			e.drainDirty()
			retryUntil = time.Time{}
		case <-reconcileTicker.C:
			stopTimer(timer)
			if err := e.reconcile(e.ctx); err != nil && e.ctx.Err() == nil {
				slog.Warn("计划任务权威对账失败", "error", err)
			}
			e.drainDirty()
			retryUntil = time.Time{}
		case <-timerC:
			if e.dispatchDue() {
				retryUntil = time.Now().Add(dispatchQueueRetry)
			} else {
				retryUntil = time.Time{}
			}
		}
	}
}

func stopTimer(timer *time.Timer) {
	if timer != nil {
		timer.Stop()
	}
}

func (e *Engine) drainDirty() {
	for {
		e.mu.Lock()
		if len(e.dirty) == 0 {
			e.mu.Unlock()
			return
		}
		ids := make([]string, 0, len(e.dirty))
		for taskID := range e.dirty {
			ids = append(ids, taskID)
		}
		e.dirty = make(map[string]struct{})
		e.mu.Unlock()
		for _, taskID := range ids {
			e.reloadTask(taskID)
		}
	}
}

func (e *Engine) reloadTask(taskID string) {
	task, err := e.repo.Get(e.ctx, taskID)
	if err != nil {
		if e.ctx.Err() == nil {
			slog.Warn("重载计划任务失败", "task_id", taskID, "error", err)
		}
		return
	}
	if task == nil || !task.Enabled {
		e.remove(taskID)
		return
	}
	e.loadTask(e.ctx, task, time.Now().UTC())
}

func (e *Engine) loadTask(ctx context.Context, task *Task, now time.Time) {
	if taskRunningOrLocked(*task, now) {
		e.remove(task.ID)
		return
	}
	next, err := NextRunAt(*task, now)
	if err != nil {
		_, _ = e.repo.UpdateNextRun(ctx, task.ID, task.Version, time.Time{}, TaskStatusError, err.Error())
		e.remove(task.ID)
		return
	}
	if task.NextRunAt == nil || !task.NextRunAt.Equal(next) {
		updated, err := e.repo.UpdateNextRun(ctx, task.ID, task.Version, next, TaskStatusIdle, "")
		if err != nil {
			return
		}
		if !updated {
			e.remove(task.ID)
			e.ReloadTask(task.ID)
			return
		}
		refreshed, err := e.repo.Get(ctx, task.ID)
		if err != nil || refreshed == nil || !refreshed.Enabled || refreshed.NextRunAt == nil {
			return
		}
		if taskRunningOrLocked(*refreshed, time.Now().UTC()) {
			e.remove(task.ID)
			return
		}
		task = refreshed
		next = *task.NextRunAt
	}
	e.upsert(task.ID, next, task.Version)
}

// dispatchDue returns true when the bounded queue is full. The current heap
// item remains indexed and is retried after a short backoff.
func (e *Engine) dispatchDue() bool {
	for {
		now := time.Now().UTC()
		item, ok := e.peekDue(now)
		if !ok {
			return false
		}
		task, err := e.repo.Get(e.ctx, item.taskID)
		if err != nil {
			e.remove(item.taskID)
			e.ReloadTask(item.taskID)
			continue
		}
		if task == nil || !task.Enabled || task.NextRunAt == nil {
			e.remove(item.taskID)
			continue
		}
		if taskRunningOrLocked(*task, now) {
			e.remove(item.taskID)
			continue
		}
		if task.Version != item.version || !task.NextRunAt.Equal(item.nextRunAt) || task.NextRunAt.After(now) {
			e.remove(item.taskID)
			e.loadTask(e.ctx, task, now)
			continue
		}
		select {
		case e.dispatch <- scheduledDispatch{taskID: task.ID, version: task.Version}:
			e.remove(task.ID)
		case <-e.ctx.Done():
			return false
		default:
			return true
		}
	}
}

func taskRunningOrLocked(task Task, now time.Time) bool {
	return task.Status == TaskStatusRunning || (task.LockedUntil != nil && task.LockedUntil.After(now))
}

func (e *Engine) dispatchWorker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case job := <-e.dispatch:
			e.runner.RunVersion(e.ctx, job.taskID, TriggerSchedule, job.version)
		}
	}
}

func (e *Engine) upsert(taskID string, next time.Time, version int) {
	if next.IsZero() {
		e.remove(taskID)
		return
	}
	e.mu.Lock()
	if item, ok := e.byID[taskID]; ok {
		item.nextRunAt = next
		item.version = version
		heap.Fix(&e.items, item.index)
	} else {
		item := &heapItem{taskID: taskID, nextRunAt: next, version: version, index: -1}
		heap.Push(&e.items, item)
		e.byID[taskID] = item
	}
	e.mu.Unlock()
	e.notifyWake()
}

func (e *Engine) remove(taskID string) {
	e.mu.Lock()
	e.removeLocked(taskID)
	e.mu.Unlock()
}

func (e *Engine) removeLocked(taskID string) {
	if item, ok := e.byID[taskID]; ok {
		heap.Remove(&e.items, item.index)
		delete(e.byID, taskID)
	}
}

func (e *Engine) peekNext() (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.items) == 0 {
		return time.Time{}, false
	}
	return e.items[0].nextRunAt, true
}

func (e *Engine) peekDue(now time.Time) (heapItem, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.items) == 0 || e.items[0].nextRunAt.After(now) {
		return heapItem{}, false
	}
	return *e.items[0], true
}

func (e *Engine) notifyWake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

type heapItem struct {
	taskID    string
	nextRunAt time.Time
	version   int
	index     int
}

type taskHeap []*heapItem

func (h taskHeap) Len() int           { return len(h) }
func (h taskHeap) Less(i, j int) bool { return h[i].nextRunAt.Before(h[j].nextRunAt) }
func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *taskHeap) Push(x any) {
	item := x.(*heapItem)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}
