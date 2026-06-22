export const scheduleKindOptions = [
  { value: 'daily', label: '每日' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'interval', label: '固定间隔' },
  { value: 'cron', label: '高级 cron' },
]

export const taskStatusLabels: Record<string, string> = {
  idle: '空闲',
  running: '运行中',
  disabled: '已停用',
  error: '异常',
}

export const runStatusLabels: Record<string, string> = {
  running: '运行中',
  success: '成功',
  failed: '失败',
  timeout: '超时',
  cancelled: '已取消',
  skipped: '已跳过',
  abandoned: '异常退出',
}

export const triggerLabels: Record<string, string> = {
  schedule: '定时',
  manual: '手动',
  retry: '重试',
  system: '系统',
}

export function formatSchedule(kind: string, expr: string, timezone: string) {
  const label = scheduleKindOptions.find((item) => item.value === kind)?.label ?? kind
  return `${label} / ${expr} / ${timezone || 'UTC'}`
}

export function statusColor(value?: string | null) {
  if (value === 'success' || value === 'idle') return 'green'
  if (value === 'running') return 'yellow'
  if (value === 'disabled' || value === 'skipped') return 'gray'
  if (value === 'failed' || value === 'timeout' || value === 'error' || value === 'abandoned') return 'red'
  return 'blue'
}
