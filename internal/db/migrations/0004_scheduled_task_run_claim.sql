-- Persist the exact task claim that owns each run so completion and expired
-- lock recovery cannot overwrite a concurrently edited task definition.
ALTER TABLE scheduled_task_runs ADD COLUMN task_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scheduled_task_runs ADD COLUMN runner_id TEXT NOT NULL DEFAULT '';
