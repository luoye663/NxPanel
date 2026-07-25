-- Retention cleanup indexes. Keep terminal run indexes partial so active rows
-- are not included in maintenance scans.
CREATE INDEX IF NOT EXISTS idx_login_audit_retention
  ON login_audit(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_terminal_finished
  ON scheduled_task_runs(COALESCE(NULLIF(finished_at, ''), created_at), id)
  WHERE status != 'running';

CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_terminal_task_created
  ON scheduled_task_runs(task_id, created_at DESC, id DESC)
  WHERE status != 'running';
