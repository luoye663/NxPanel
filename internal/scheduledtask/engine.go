package scheduledtask

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"time"
)

type Engine struct {
	repo   *Repo
	runner *Runner

	mu     sync.Mutex
	items  taskHeap
	reload chan string
	wake   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewEngine(repo *Repo, runner *Runner) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{repo: repo, runner: runner, reload: make(chan string, 32), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}

func (e *Engine) Start() error {
	if _, err := e.repo.MarkExpiredRunningAbandoned(e.ctx, time.Now().UTC()); err != nil {
		return err
	}
	if err := e.loadEnabled(e.ctx); err != nil {
		return err
	}
	e.wg.Add(1)
	go e.loop()
	return nil
}

func (e *Engine) Stop() {
	e.cancel()
	e.wg.Wait()
}

func (e *Engine) ReloadTask(taskID string) {
	select {
	case e.reload <- taskID:
	case <-e.ctx.Done():
	default:
		// reload channel 满时唤醒主循环，让它尽快重新判断堆顶，不阻塞 API 写请求。
		e.notifyWake()
	}
}

func (e *Engine) loadEnabled(ctx context.Context) error {
	tasks, err := e.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		next, err := NextRunAt(*task, now)
		if err != nil {
			_ = e.repo.UpdateNextRun(ctx, task.ID, time.Time{}, TaskStatusError, err.Error())
			continue
		}
		if task.NextRunAt == nil || !task.NextRunAt.Equal(next) {
			_ = e.repo.UpdateNextRun(ctx, task.ID, next, TaskStatusIdle, "")
			task.NextRunAt = &next
		}
		e.push(task.ID, next, task.Version)
	}
	return nil
}

func (e *Engine) loop() {
	defer e.wg.Done()
	for {
		nextAt, ok := e.peekNext()
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
			if timer != nil {
				timer.Stop()
			}
			return
		case taskID := <-e.reload:
			if timer != nil {
				timer.Stop()
			}
			e.reloadTask(taskID)
		case <-e.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-timerC:
			e.dispatchDue()
		}
	}
}

func (e *Engine) reloadTask(taskID string) {
	task, err := e.repo.Get(e.ctx, taskID)
	if err != nil || task == nil || !task.Enabled {
		return
	}
	next, err := NextRunAt(*task, time.Now().UTC())
	if err != nil {
		_ = e.repo.UpdateNextRun(e.ctx, task.ID, time.Time{}, TaskStatusError, err.Error())
		return
	}
	_ = e.repo.UpdateNextRun(e.ctx, task.ID, next, TaskStatusIdle, "")
	e.push(task.ID, next, task.Version+1)
	e.notifyWake()
}

func (e *Engine) dispatchDue() {
	now := time.Now().UTC()
	for {
		item, ok := e.popDue(now)
		if !ok {
			return
		}
		task, err := e.repo.Get(e.ctx, item.taskID)
		if err != nil || task == nil || !task.Enabled || task.NextRunAt == nil || task.NextRunAt.After(now) {
			continue
		}
		// 执行前重新读取 DB，heap 只作为轻量提醒，不作为任务状态源。
		go func(taskID string) {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Warn("计划任务 runner panic", "task_id", taskID, "panic", recovered)
				}
			}()
			e.runner.Run(e.ctx, taskID, TriggerSchedule)
		}(task.ID)
	}
}

func (e *Engine) push(taskID string, next time.Time, version int) {
	if next.IsZero() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	heap.Push(&e.items, heapItem{taskID: taskID, nextRunAt: next, version: version})
	e.notifyWake()
}

func (e *Engine) peekNext() (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.items) == 0 {
		return time.Time{}, false
	}
	return e.items[0].nextRunAt, true
}

func (e *Engine) popDue(now time.Time) (heapItem, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.items) == 0 || e.items[0].nextRunAt.After(now) {
		return heapItem{}, false
	}
	return heap.Pop(&e.items).(heapItem), true
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
}

type taskHeap []heapItem

func (h taskHeap) Len() int           { return len(h) }
func (h taskHeap) Less(i, j int) bool { return h[i].nextRunAt.Before(h[j].nextRunAt) }
func (h taskHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *taskHeap) Push(x any) { *h = append(*h, x.(heapItem)) }

func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
