package scheduledtask

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

type taskReloader interface {
	ReloadTask(taskID string)
}

type Service struct {
	repo     *Repo
	registry *Registry
	runner   *Runner
	reloader taskReloader
}

func NewService(repo *Repo, registry *Registry, runner *Runner, reloader taskReloader) *Service {
	return &Service{repo: repo, registry: registry, runner: runner, reloader: reloader}
}

func (s *Service) Definitions() []TaskDefinition {
	return s.registry.Definitions()
}

func (s *Service) Register(handler TaskHandler) error {
	return s.registry.Register(handler)
}

func (s *Service) List(ctx context.Context) ([]TaskListItem, error) {
	tasks, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]TaskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskToListItem(*task))
	}
	return items, nil
}

func (s *Service) Create(ctx context.Context, req CreateTaskRequest) (TaskListItem, error) {
	task, err := s.buildTask(req.Type, req.Name, req.Enabled, req.Schedule, req.Params, req.ConcurrencyPolicy, req.MissedPolicy, req.TimeoutSeconds, req.MaxRetries, req.RetryDelaySeconds)
	if err != nil {
		return TaskListItem{}, err
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return TaskListItem{}, err
	}
	s.reload(task.ID)
	return taskToListItem(*task), nil
}

func (s *Service) GetBySource(ctx context.Context, sourceType, sourceID string) (*Task, error) {
	return s.repo.GetBySource(ctx, sourceType, sourceID)
}

func (s *Service) UpsertSource(ctx context.Context, sourceType, sourceID string, system bool, req CreateTaskRequest) (TaskListItem, error) {
	existing, err := s.repo.GetBySource(ctx, sourceType, sourceID)
	if err != nil {
		return TaskListItem{}, err
	}
	task, err := s.buildTask(req.Type, req.Name, req.Enabled, req.Schedule, req.Params, req.ConcurrencyPolicy, req.MissedPolicy, req.TimeoutSeconds, req.MaxRetries, req.RetryDelaySeconds)
	if err != nil {
		return TaskListItem{}, err
	}
	task.System = system
	task.SourceType = sourceType
	task.SourceID = sourceID
	if existing == nil {
		if err := s.repo.Create(ctx, task); err != nil {
			return TaskListItem{}, err
		}
		s.reload(task.ID)
		return taskToListItem(*task), nil
	}
	task.ID = existing.ID
	task.System = existing.System
	task.LastRunAt = existing.LastRunAt
	task.LastFinishedAt = existing.LastFinishedAt
	task.LastStatus = existing.LastStatus
	task.LastError = existing.LastError
	task.LastDurationMillis = existing.LastDurationMillis
	task.LastRunID = existing.LastRunID
	if err := s.repo.Update(ctx, task, existing.Version); err != nil {
		return TaskListItem{}, err
	}
	s.reload(task.ID)
	updated, err := s.repo.Get(ctx, task.ID)
	if err != nil {
		return TaskListItem{}, err
	}
	return taskToListItem(*updated), nil
}

func (s *Service) CreateSourceIfMissing(ctx context.Context, sourceType, sourceID string, system bool, lastRunAt *time.Time, req CreateTaskRequest) (bool, error) {
	existing, err := s.repo.GetBySource(ctx, sourceType, sourceID)
	if err != nil || existing != nil {
		return false, err
	}
	task, err := s.buildTask(req.Type, req.Name, req.Enabled, req.Schedule, req.Params, req.ConcurrencyPolicy, req.MissedPolicy, req.TimeoutSeconds, req.MaxRetries, req.RetryDelaySeconds)
	if err != nil {
		return false, err
	}
	task.System = system
	task.SourceType = sourceType
	task.SourceID = sourceID
	task.LastRunAt = lastRunAt
	// 启动迁移只创建缺失任务，避免覆盖用户已在计划任务中心编辑过的配置。
	if err := s.repo.Create(ctx, task); err != nil {
		return false, err
	}
	s.reload(task.ID)
	return true, nil
}

func (s *Service) Update(ctx context.Context, taskID string, req UpdateTaskRequest) (TaskListItem, error) {
	existing, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return TaskListItem{}, err
	}
	if existing == nil {
		return TaskListItem{}, app.ErrNotFoundMsg("计划任务不存在")
	}
	if req.Version <= 0 {
		return TaskListItem{}, app.ErrBadRequestMsg("version 不能为空")
	}
	task, err := s.buildTask(existing.Type, req.Name, req.Enabled, req.Schedule, req.Params, req.ConcurrencyPolicy, req.MissedPolicy, req.TimeoutSeconds, req.MaxRetries, req.RetryDelaySeconds)
	if err != nil {
		return TaskListItem{}, err
	}
	task.ID = existing.ID
	task.System = existing.System
	task.SourceType = existing.SourceType
	task.SourceID = existing.SourceID
	if err := s.repo.Update(ctx, task, req.Version); err != nil {
		return TaskListItem{}, err
	}
	s.reload(task.ID)
	updated, err := s.repo.Get(ctx, task.ID)
	if err != nil {
		return TaskListItem{}, err
	}
	return taskToListItem(*updated), nil
}

func (s *Service) SetEnabled(ctx context.Context, taskID string, enabled bool) error {
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return app.ErrNotFoundMsg("计划任务不存在")
	}
	if enabled && task.NextRunAt == nil {
		compiled, err := CompileSchedule(task.ScheduleKind, task.ScheduleExpr, task.Timezone)
		if err != nil {
			return app.ErrValidationFailedMsg(err.Error(), nil)
		}
		next := compiled.Next(time.Now().UTC())
		task.NextRunAt = &next
	}
	if err := s.repo.SetEnabled(ctx, taskID, enabled); err != nil {
		return err
	}
	if enabled && task.NextRunAt != nil {
		if err := s.repo.UpdateNextRun(ctx, taskID, *task.NextRunAt, TaskStatusIdle, ""); err != nil {
			return err
		}
	}
	s.reload(taskID)
	return nil
}

func (s *Service) Delete(ctx context.Context, taskID string) error {
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return app.ErrNotFoundMsg("计划任务不存在")
	}
	if task.System {
		return app.NewAppError(app.ErrForbidden, "系统内置任务不能删除", nil)
	}
	return s.repo.Delete(ctx, taskID)
}

func (s *Service) RunNow(ctx context.Context, taskID string) error {
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return app.ErrNotFoundMsg("计划任务不存在")
	}
	if !task.Enabled {
		return app.ErrBadRequestMsg("计划任务已停用，不能立即执行")
	}
	if _, ok := s.registry.Get(task.Type); !ok {
		return app.ErrBadRequestMsg("计划任务类型尚未接入执行器")
	}
	// 手动执行仍交给 Runner，确保锁、超时、执行记录和调度执行共用同一套流程。
	go s.runner.Run(context.Background(), taskID, TriggerManual)
	return nil
}

func (s *Service) ListRuns(ctx context.Context, taskID string, limit int) ([]RunListItem, error) {
	if task, err := s.repo.Get(ctx, taskID); err != nil {
		return nil, err
	} else if task == nil {
		return nil, app.ErrNotFoundMsg("计划任务不存在")
	}
	runs, err := s.repo.ListRuns(ctx, taskID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunListItem, 0, len(runs))
	for _, run := range runs {
		items = append(items, runToListItem(*run))
	}
	return items, nil
}

func (s *Service) buildTask(taskType, name string, enabled bool, schedule ScheduleDTO, params json.RawMessage, concurrencyPolicy, missedPolicy string, timeoutSeconds, maxRetries, retryDelaySeconds int) (*Task, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, app.ErrBadRequestMsg("任务名称不能为空")
	}
	if strings.TrimSpace(taskType) == "" {
		return nil, app.ErrBadRequestMsg("任务类型不能为空")
	}
	if schedule.Timezone == "" {
		schedule.Timezone = "UTC"
	}
	if _, err := CompileSchedule(schedule.Kind, schedule.Expr, schedule.Timezone); err != nil {
		return nil, app.ErrValidationFailedMsg(err.Error(), nil)
	}
	if concurrencyPolicy == "" {
		concurrencyPolicy = ConcurrencySkip
	}
	if concurrencyPolicy != ConcurrencySkip {
		return nil, app.ErrBadRequestMsg("当前仅支持 skip 并发策略")
	}
	if missedPolicy == "" {
		missedPolicy = MissedRunOnce
	}
	if missedPolicy != MissedRunOnce && missedPolicy != MissedSkip {
		return nil, app.ErrBadRequestMsg("错过执行策略无效")
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	if timeoutSeconds < 60 {
		return nil, app.ErrBadRequestMsg("超时时间不能小于 60 秒")
	}
	if retryDelaySeconds <= 0 {
		retryDelaySeconds = 60
	}
	if maxRetries < 0 || maxRetries > 10 {
		return nil, app.ErrBadRequestMsg("重试次数必须在 0-10 之间")
	}
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}
	handler, ok := s.registry.Get(taskType)
	if !ok {
		return nil, app.ErrBadRequestMsg("计划任务类型尚未注册")
	}
	validated, err := handler.ValidateParams(params)
	if err != nil {
		return nil, app.ErrValidationFailedMsg(err.Error(), nil)
	}
	params = validated
	now := time.Now().UTC()
	status := TaskStatusIdle
	if !enabled {
		status = TaskStatusDisabled
	}
	task := &Task{
		Type: taskType, Name: name, Enabled: enabled, Status: status,
		ScheduleKind: schedule.Kind, ScheduleExpr: schedule.Expr, Timezone: schedule.Timezone,
		ParamsJSON: params, ConcurrencyPolicy: concurrencyPolicy, MissedPolicy: missedPolicy,
		TimeoutSeconds: timeoutSeconds, MaxRetries: maxRetries, RetryDelaySeconds: retryDelaySeconds,
	}
	if enabled {
		compiled, _ := CompileSchedule(schedule.Kind, schedule.Expr, schedule.Timezone)
		next := compiled.Next(now)
		task.NextRunAt = &next
	}
	return task, nil
}

func (s *Service) reload(taskID string) {
	if s.reloader != nil {
		s.reloader.ReloadTask(taskID)
	}
}

func taskToListItem(task Task) TaskListItem {
	var params any = map[string]any{}
	if len(task.ParamsJSON) > 0 {
		_ = json.Unmarshal(task.ParamsJSON, &params)
	}
	return TaskListItem{
		ID: task.ID, Type: task.Type, Name: task.Name, Enabled: task.Enabled, System: task.System, Status: task.Status,
		Schedule:  ScheduleDTO{Kind: task.ScheduleKind, Expr: task.ScheduleExpr, Timezone: task.Timezone},
		Params:    params,
		NextRunAt: timePtrToString(task.NextRunAt), LastRunAt: timePtrToString(task.LastRunAt), LastStatus: task.LastStatus,
		LastError: task.LastError, LastRunID: task.LastRunID,
		ConcurrencyPolicy: task.ConcurrencyPolicy, MissedPolicy: task.MissedPolicy, TimeoutSeconds: task.TimeoutSeconds,
		MaxRetries: task.MaxRetries, RetryDelaySeconds: task.RetryDelaySeconds, Version: task.Version,
	}
}

func runToListItem(run Run) RunListItem {
	return RunListItem{
		ID: run.ID, TaskID: run.TaskID, TaskType: run.TaskType, TaskName: run.TaskName, Trigger: run.Trigger,
		Status: run.Status, Attempt: run.Attempt, StartedAt: formatTime(run.StartedAt), FinishedAt: timePtrToString(run.FinishedAt),
		DurationMillis: run.DurationMillis, ErrorMessage: run.ErrorMessage, LogFile: run.LogFile, OperationID: run.OperationID,
		RequestID: run.RequestID, CreatedAt: formatTime(run.CreatedAt),
	}
}

func timePtrToString(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}
