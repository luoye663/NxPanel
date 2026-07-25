package scheduledtask

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Get(ctx context.Context, id string) (*Task, error) {
	row := r.db.QueryRowContext(ctx, selectTaskSQL+` WHERE id = ?`, id)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询计划任务失败 id=%s: %w", id, err)
	}
	return task, nil
}

func (r *Repo) GetBySource(ctx context.Context, sourceType, sourceID string) (*Task, error) {
	row := r.db.QueryRowContext(ctx, selectTaskSQL+` WHERE source_type = ? AND source_id = ?`, sourceType, sourceID)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("按来源查询计划任务失败 source=%s/%s: %w", sourceType, sourceID, err)
	}
	return task, nil
}

func (r *Repo) ListEnabled(ctx context.Context) ([]*Task, error) {
	rows, err := r.db.QueryContext(ctx, selectTaskSQL+` WHERE enabled = 1 ORDER BY next_run_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询启用计划任务失败: %w", err)
	}
	defer rows.Close()
	var result []*Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *Repo) ListAll(ctx context.Context) ([]*Task, error) {
	rows, err := r.db.QueryContext(ctx, selectTaskSQL+` ORDER BY system DESC, created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询计划任务列表失败: %w", err)
	}
	defer rows.Close()
	var result []*Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *Repo) Create(ctx context.Context, task *Task) error {
	now := time.Now().UTC()
	if task.ID == "" {
		task.ID = app.NewID("schtask")
	}
	if task.Status == "" {
		task.Status = TaskStatusIdle
	}
	if task.Version <= 0 {
		task.Version = 1
	}
	if !task.Enabled {
		task.Status = TaskStatusDisabled
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, `INSERT INTO scheduled_tasks (
		id, type, name, enabled, system, source_type, source_id, status,
		schedule_kind, schedule_expr, timezone, params_json, concurrency_policy, missed_policy,
		timeout_seconds, max_retries, retry_delay_seconds, next_run_at, last_run_at,
		last_finished_at, last_status, last_error, last_duration_ms, last_run_id,
		locked_by, locked_until, version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, taskArgs(task)...)
	if err != nil {
		return fmt.Errorf("创建计划任务失败 id=%s: %w", task.ID, err)
	}
	return nil
}

func (r *Repo) Update(ctx context.Context, task *Task, expectedVersion int) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE scheduled_tasks
		SET name = ?, enabled = ?, status = ?, schedule_kind = ?, schedule_expr = ?, timezone = ?, params_json = ?,
			concurrency_policy = ?, missed_policy = ?, timeout_seconds = ?, max_retries = ?, retry_delay_seconds = ?,
			next_run_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, task.Name, boolToInt(task.Enabled), task.Status, task.ScheduleKind, task.ScheduleExpr, task.Timezone, string(task.ParamsJSON),
		task.ConcurrencyPolicy, task.MissedPolicy, task.TimeoutSeconds, task.MaxRetries, task.RetryDelaySeconds,
		formatTimePtr(task.NextRunAt), formatTime(now), task.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("更新计划任务失败 id=%s: %w", task.ID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return app.NewAppError(app.ErrConflict, "计划任务已被其他操作修改，请刷新后重试", nil)
	}
	return nil
}

func (r *Repo) SetEnabled(ctx context.Context, taskID string, enabled bool, nextRunAt *time.Time) error {
	status := TaskStatusDisabled
	if enabled {
		status = TaskStatusIdle
	}
	if !enabled {
		nextRunAt = nil
	}
	_, err := r.db.ExecContext(ctx, `UPDATE scheduled_tasks
		SET enabled = ?, status = ?, next_run_at = ?, last_error = '', version = version + 1, updated_at = ?
		WHERE id = ?`, boolToInt(enabled), status, formatTimePtr(nextRunAt), formatTime(time.Now().UTC()), taskID)
	if err != nil {
		return fmt.Errorf("切换计划任务启用状态失败 id=%s: %w", taskID, err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, taskID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("删除计划任务失败 id=%s: %w", taskID, err)
	}
	return nil
}

func (r *Repo) ListRuns(ctx context.Context, taskID string, limit int) ([]*Run, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, selectRunSQL+` WHERE task_id = ? ORDER BY created_at DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询计划任务执行历史失败 task_id=%s: %w", taskID, err)
	}
	defer rows.Close()
	var result []*Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (r *Repo) PruneRuns(ctx context.Context, cutoff time.Time, maxPerTask int) (int64, error) {
	if maxPerTask < 1 {
		return 0, fmt.Errorf("计划任务执行记录每任务最大保留数必须大于 0")
	}
	result, err := r.db.ExecContext(ctx, `WITH ranked AS (
		SELECT id,
			ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at DESC, id DESC) AS row_num
		FROM scheduled_task_runs
		WHERE status != 'running'
	), over_limit AS (
		SELECT id FROM ranked WHERE row_num > ?
	)
	DELETE FROM scheduled_task_runs
	WHERE status != 'running' AND (COALESCE(NULLIF(finished_at, ''), created_at) < ? OR id IN (SELECT id FROM over_limit))`, maxPerTask, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("清理计划任务执行历史失败: %w", err)
	}
	return result.RowsAffected()
}

func (r *Repo) UpdateNextRun(ctx context.Context, taskID string, expectedVersion int, next time.Time, status string, errText string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE scheduled_tasks
		SET next_run_at = ?, status = ?, last_error = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, formatTimePtr(&next), status, errText, formatTime(time.Now().UTC()), taskID, expectedVersion)
	if err != nil {
		return false, fmt.Errorf("更新计划任务下次执行时间失败 id=%s: %w", taskID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

func (r *Repo) DueTasks(ctx context.Context, now time.Time, limit int) ([]*Task, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, selectTaskSQL+` WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at ASC LIMIT ?`, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("查询到期待执行任务失败: %w", err)
	}
	defer rows.Close()
	var result []*Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (r *Repo) MarkExpiredRunningAbandoned(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.recoverExpiredLocksTx(ctx, tx, now, "")
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

func (r *Repo) recoverExpiredLocksTx(ctx context.Context, tx *sql.Tx, now time.Time, taskID string) (int64, error) {
	const message = "任务锁已过期，标记为异常退出"
	if _, err := tx.ExecContext(ctx, `UPDATE scheduled_task_runs
		SET status = 'abandoned', finished_at = ?,
			duration_ms = MAX(0, CAST((julianday(?) - julianday(started_at)) * 86400000 AS INTEGER)), error_message = ?
		WHERE status = 'running' AND id IN (
			SELECT last_run_id FROM scheduled_tasks
			WHERE locked_by != '' AND locked_until IS NOT NULL
				AND julianday(locked_until) IS NOT NULL AND julianday(locked_until) < julianday(?)
				AND (? = '' OR id = ?)
		)`, formatTime(now), formatTime(now), message, formatTime(now), taskID, taskID); err != nil {
		return 0, fmt.Errorf("恢复过期任务执行记录失败: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks
		SET status = CASE WHEN enabled = 0 THEN 'disabled' ELSE 'error' END,
			last_finished_at = ?, last_status = 'abandoned', last_error = ?,
			locked_by = '', locked_until = NULL, version = version + 1, updated_at = ?
		WHERE locked_by != '' AND locked_until IS NOT NULL
			AND julianday(locked_until) IS NOT NULL AND julianday(locked_until) < julianday(?)
			AND (? = '' OR id = ?)`,
		formatTime(now), message, formatTime(now), formatTime(now), taskID, taskID)
	if err != nil {
		return 0, fmt.Errorf("恢复过期运行任务失败: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return changed, nil
}

func (r *Repo) BeginRun(ctx context.Context, taskID, trigger, runnerID string, now time.Time) (*Task, *Run, bool, error) {
	return r.BeginRunVersion(ctx, taskID, trigger, runnerID, 0, now)
}

func (r *Repo) BeginRunVersion(ctx context.Context, taskID, trigger, runnerID string, expectedVersion int, now time.Time) (*Task, *Run, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := r.recoverExpiredLocksTx(ctx, tx, now, taskID); err != nil {
		return nil, nil, false, err
	}

	run := &Run{
		ID:        app.NewID("schrun"),
		Trigger:   trigger,
		Status:    RunStatusRunning,
		Attempt:   1,
		StartedAt: now,
		CreatedAt: now,
	}

	whereScheduleDue := ""
	args := []any{runnerID, now.Unix(), formatTime(now), run.ID, formatTime(now), taskID, formatTime(now)}
	if trigger == TriggerSchedule {
		whereScheduleDue = " AND next_run_at IS NOT NULL AND next_run_at <= ?"
		args = append(args, formatTime(now))
		if expectedVersion > 0 {
			whereScheduleDue += " AND version = ?"
			args = append(args, expectedVersion)
		}
	}
	// A nonempty owner with a NULL or malformed expiry is ambiguous. Deny the
	// claim fail-closed until an operator repairs the lock metadata.
	row := tx.QueryRowContext(ctx, `UPDATE scheduled_tasks
		SET status = 'running', locked_by = ?, locked_until = strftime('%Y-%m-%dT%H:%M:%fZ', ?, 'unixepoch', '+' || (timeout_seconds + 30) || ' seconds'), last_run_at = ?, last_run_id = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND enabled = 1 AND (
			locked_by = '' OR (
				locked_until IS NOT NULL AND julianday(locked_until) IS NOT NULL
				AND julianday(locked_until) < julianday(?)
			)
		)`+whereScheduleDue+`
		RETURNING `+taskColumns, args...)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		task, err := r.getTx(ctx, tx, taskID)
		if err != nil {
			return nil, nil, false, err
		}
		// Expired-lock recovery may have changed state even when this claim is
		// rejected (notably a scheduled claim with a now-stale version).
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		return task, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("抢占计划任务锁失败 id=%s: %w", taskID, err)
	}
	run.TaskID = task.ID
	run.TaskType = task.Type
	run.TaskName = task.Name
	run.TaskVersion = task.Version
	run.RunnerID = runnerID
	if _, err := tx.ExecContext(ctx, `INSERT INTO scheduled_task_runs (id, task_id, task_type, task_name, trigger, status, attempt, task_version, runner_id, started_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.TaskID, run.TaskType, run.TaskName, run.Trigger, run.Status, run.Attempt, run.TaskVersion, run.RunnerID, formatTime(run.StartedAt), formatTime(run.CreatedAt)); err != nil {
		return nil, nil, false, fmt.Errorf("创建计划任务执行记录失败 run_id=%s: %w", run.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	task.LastRunID = run.ID
	return task, run, true, nil
}

func (r *Repo) FinishRun(ctx context.Context, task Task, run Run, status string, errText string, nextRunAt time.Time, finishedAt time.Time) (FinishOutcome, error) {
	durationMillis := finishedAt.Sub(run.StartedAt).Milliseconds()
	if durationMillis < 0 {
		durationMillis = 0
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return FinishOutcome{}, err
	}
	defer func() { _ = tx.Rollback() }()
	runResult, err := tx.ExecContext(ctx, `UPDATE scheduled_task_runs
		SET status = ?, finished_at = ?, duration_ms = ?, error_message = ?
		WHERE id = ? AND status = 'running' AND task_version = ? AND runner_id = ?`,
		status, formatTime(finishedAt), durationMillis, errText, run.ID, run.TaskVersion, run.RunnerID)
	if err != nil {
		return FinishOutcome{}, fmt.Errorf("更新计划任务执行记录失败 run_id=%s: %w", run.ID, err)
	}
	runChanged, err := runResult.RowsAffected()
	if err != nil {
		return FinishOutcome{}, err
	}
	if runChanged != 1 {
		if err := r.releaseMatchingLock(ctx, tx, task.ID, run, finishedAt); err != nil {
			return FinishOutcome{}, err
		}
		if err := tx.Commit(); err != nil {
			return FinishOutcome{}, err
		}
		return FinishOutcome{Stale: true}, nil
	}
	nextStatus := TaskStatusIdle
	if !task.Enabled {
		nextStatus = TaskStatusDisabled
	} else if status != RunStatusSuccess {
		nextStatus = TaskStatusError
	}
	lastStatus := status
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks
		SET status = ?, next_run_at = ?, last_finished_at = ?, last_status = ?, last_error = ?, last_duration_ms = ?, locked_by = '', locked_until = NULL, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND last_run_id = ? AND locked_by = ?`,
		nextStatus, formatTimePtr(&nextRunAt), formatTime(finishedAt), lastStatus, errText, durationMillis, formatTime(finishedAt),
		task.ID, run.TaskVersion, run.ID, run.RunnerID)
	if err != nil {
		return FinishOutcome{}, fmt.Errorf("更新计划任务完成状态失败 id=%s: %w", task.ID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return FinishOutcome{}, err
	}
	if changed == 0 {
		if err := r.releaseMatchingLock(ctx, tx, task.ID, run, finishedAt); err != nil {
			return FinishOutcome{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FinishOutcome{}, err
	}
	return FinishOutcome{RunFinalized: true, ScheduleUpdated: changed == 1, Stale: changed == 0}, nil
}

func (r *Repo) releaseMatchingLock(ctx context.Context, tx *sql.Tx, taskID string, run Run, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks
		SET locked_by = '', locked_until = NULL, updated_at = ?
		WHERE id = ? AND last_run_id = ? AND locked_by = ?`, formatTime(now), taskID, run.ID, run.RunnerID); err != nil {
		return fmt.Errorf("释放过期计划任务锁失败 id=%s: %w", taskID, err)
	}
	return nil
}

func (r *Repo) getTx(ctx context.Context, tx *sql.Tx, id string) (*Task, error) {
	row := tx.QueryRowContext(ctx, selectTaskSQL+` WHERE id = ?`, id)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return task, err
}

const taskColumns = `id, type, name, enabled, system, source_type, source_id, status,
	schedule_kind, schedule_expr, timezone, params_json, concurrency_policy, missed_policy,
	timeout_seconds, max_retries, retry_delay_seconds, next_run_at, last_run_at,
	last_finished_at, last_status, last_error, last_duration_ms, last_run_id,
	locked_by, locked_until, version, created_at, updated_at`

const selectTaskSQL = `SELECT ` + taskColumns + ` FROM scheduled_tasks`

const selectRunSQL = `SELECT id, task_id, task_type, task_name, trigger, status, attempt, task_version, runner_id,
	started_at, finished_at, duration_ms, error_message, log_file, operation_id, request_id, created_at FROM scheduled_task_runs`

type scanner interface{ Scan(dest ...any) error }

func scanTask(row scanner) (*Task, error) {
	task := &Task{}
	var enabled, system int
	var paramsJSON string
	var nextRunAt, lastRunAt, lastFinishedAt, lastStatus, lockedUntil, createdAt, updatedAt sql.NullString
	err := row.Scan(&task.ID, &task.Type, &task.Name, &enabled, &system, &task.SourceType, &task.SourceID, &task.Status,
		&task.ScheduleKind, &task.ScheduleExpr, &task.Timezone, &paramsJSON, &task.ConcurrencyPolicy, &task.MissedPolicy,
		&task.TimeoutSeconds, &task.MaxRetries, &task.RetryDelaySeconds, &nextRunAt, &lastRunAt,
		&lastFinishedAt, &lastStatus, &task.LastError, &task.LastDurationMillis, &task.LastRunID,
		&task.LockedBy, &lockedUntil, &task.Version, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	task.Enabled = enabled == 1
	task.System = system == 1
	task.ParamsJSON = []byte(paramsJSON)
	task.NextRunAt = parseNullTime(nextRunAt)
	task.LastRunAt = parseNullTime(lastRunAt)
	task.LastFinishedAt = parseNullTime(lastFinishedAt)
	task.LockedUntil = parseNullTime(lockedUntil)
	if parsed := parseNullTime(createdAt); parsed != nil {
		task.CreatedAt = *parsed
	}
	if parsed := parseNullTime(updatedAt); parsed != nil {
		task.UpdatedAt = *parsed
	}
	if lastStatus.Valid {
		task.LastStatus = &lastStatus.String
	}
	return task, nil
}

func scanRun(row scanner) (*Run, error) {
	run := &Run{}
	var startedAt, finishedAt, createdAt sql.NullString
	err := row.Scan(&run.ID, &run.TaskID, &run.TaskType, &run.TaskName, &run.Trigger, &run.Status, &run.Attempt, &run.TaskVersion, &run.RunnerID,
		&startedAt, &finishedAt, &run.DurationMillis, &run.ErrorMessage, &run.LogFile, &run.OperationID, &run.RequestID, &createdAt)
	if err != nil {
		return nil, err
	}
	if parsed := parseNullTime(startedAt); parsed != nil {
		run.StartedAt = *parsed
	}
	run.FinishedAt = parseNullTime(finishedAt)
	if parsed := parseNullTime(createdAt); parsed != nil {
		run.CreatedAt = *parsed
	}
	return run, nil
}

func taskArgs(task *Task) []any {
	return []any{task.ID, task.Type, task.Name, boolToInt(task.Enabled), boolToInt(task.System), task.SourceType, task.SourceID, task.Status,
		task.ScheduleKind, task.ScheduleExpr, task.Timezone, string(task.ParamsJSON), task.ConcurrencyPolicy, task.MissedPolicy,
		task.TimeoutSeconds, task.MaxRetries, task.RetryDelaySeconds, formatTimePtr(task.NextRunAt), formatTimePtr(task.LastRunAt),
		formatTimePtr(task.LastFinishedAt), nullableString(task.LastStatus), task.LastError, task.LastDurationMillis, task.LastRunID,
		task.LockedBy, formatTimePtr(task.LockedUntil), task.Version, formatTime(task.CreatedAt), formatTime(task.UpdatedAt)}
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func formatTimePtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
