import { Text } from '@mantine/core'

export function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return value.replace('T', ' ').substring(0, 19)
}

export function TimeCell({ value }: { value?: string | null }) {
  return (
    <Text size="sm" c={value ? undefined : 'dimmed'} style={{ whiteSpace: 'nowrap' }}>
      {formatDateTime(value)}
    </Text>
  )
}
