import { Badge } from '@mantine/core'

type StatusKind = 'operation' | 'login' | 'task' | 'site'

const operationLabels: Record<string, string> = {
  success: '成功',
  failed: '失败',
  running: '进行中',
  pending: '等待中',
  rolled_back: '已回滚',
}

const siteLabels: Record<string, string> = {
  enabled: '已启用',
  disabled: '已禁用',
  failed: '失败',
  drifted: '配置漂移',
}

function statusColor(kind: StatusKind, value: string | boolean): string {
  if (kind === 'login') return value ? 'green' : 'red'
  if (kind === 'site') {
    if (value === 'enabled') return 'green'
    if (value === 'disabled') return 'gray'
    if (value === 'failed') return 'red'
    if (value === 'drifted') return 'yellow'
    return 'blue'
  }
  if (kind === 'task') {
    if (value === 'ssl_renewal') return 'yellow'
    if (value === 'log_rotation') return 'green'
    return 'blue'
  }
  if (value === 'success') return 'green'
  if (value === 'failed') return 'red'
  if (value === 'running') return 'yellow'
  return 'gray'
}

interface StatusBadgeProps {
  kind: StatusKind
  value: string | boolean
  label?: string
}

export function StatusBadge({ kind, value, label }: StatusBadgeProps) {
  const text = label ?? (kind === 'login' ? (value ? '成功' : '失败') : kind === 'site' ? siteLabels[String(value)] ?? String(value) : operationLabels[String(value)] ?? String(value))

  return (
    <Badge color={statusColor(kind, value)} variant="light" radius="sm">
      {text}
    </Badge>
  )
}
