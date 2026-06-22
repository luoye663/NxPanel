import { del, get, post, put } from './client'

export interface ScheduledTaskSchedule {
  kind: string
  expr: string
  timezone: string
}

export interface ScheduledTaskDefinition {
  type: string
  label: string
  description: string
  system: boolean
  supports_manual_run: boolean
  default_schedule: ScheduledTaskSchedule
  param_schema?: Record<string, unknown>
}

export interface ScheduledTaskItem {
  id: string
  type: string
  name: string
  enabled: boolean
  system: boolean
  status: string
  schedule: ScheduledTaskSchedule
  params: Record<string, unknown>
  next_run_at?: string | null
  last_run_at?: string | null
  last_status?: string | null
  last_error: string
  last_run_id: string
  concurrency_policy: string
  missed_policy: string
  timeout_seconds: number
  max_retries: number
  retry_delay_seconds: number
  version: number
}

export interface ScheduledTaskRunItem {
  id: string
  task_id: string
  task_type: string
  task_name: string
  trigger: string
  status: string
  attempt: number
  started_at: string
  finished_at?: string | null
  duration_ms: number
  error_message: string
  log_file: string
  operation_id: string
  request_id: string
  created_at: string
}

export interface CreateScheduledTaskPayload {
  type: string
  name: string
  enabled: boolean
  schedule: ScheduledTaskSchedule
  params: Record<string, unknown>
  concurrency_policy: string
  missed_policy: string
  timeout_seconds: number
  max_retries: number
  retry_delay_seconds: number
}

export interface UpdateScheduledTaskPayload {
  name: string
  enabled: boolean
  schedule: ScheduledTaskSchedule
  params: Record<string, unknown>
  concurrency_policy: string
  missed_policy: string
  timeout_seconds: number
  max_retries: number
  retry_delay_seconds: number
  version?: number
}

export function getScheduledTaskDefinitions(): Promise<{ items: ScheduledTaskDefinition[] }> {
  return get('/scheduled-tasks/definitions')
}

export function getScheduledTasks(): Promise<{ items: ScheduledTaskItem[] }> {
  return get('/scheduled-tasks')
}

export function createScheduledTask(data: CreateScheduledTaskPayload): Promise<ScheduledTaskItem> {
  return post('/scheduled-tasks', data)
}

export function updateScheduledTask(taskId: string, data: UpdateScheduledTaskPayload): Promise<ScheduledTaskItem> {
  return put(`/scheduled-tasks/${taskId}`, data)
}

export function toggleScheduledTask(taskId: string, enabled: boolean): Promise<{ ok: boolean }> {
  return post(`/scheduled-tasks/${taskId}/toggle`, { enabled })
}

export function runScheduledTaskNow(taskId: string): Promise<{ queued: boolean }> {
  return post(`/scheduled-tasks/${taskId}/run`)
}

export function getScheduledTaskRuns(taskId: string, limit = 50): Promise<{ items: ScheduledTaskRunItem[] }> {
  return get(`/scheduled-tasks/${taskId}/runs`, { params: { limit } })
}

export function deleteScheduledTask(taskId: string): Promise<{ ok: boolean }> {
  return del(`/scheduled-tasks/${taskId}`)
}
