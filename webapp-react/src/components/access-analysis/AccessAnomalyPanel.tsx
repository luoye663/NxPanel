import { Badge, Group, Stack, Text } from '@mantine/core'
import { SectionCard } from '@/components/common/SectionCard'
import type { AccessAnomaly } from '@/api/types'

export function AccessAnomalyPanel({ anomalies }: { anomalies: AccessAnomaly[] }) {
  return (
    <SectionCard title="异常洞察" description="轻量规则标记高频 404、5xx、扫描器路径和异常 UA。">
      <Stack gap="xs">
        {anomalies.length === 0 ? <Text c="dimmed" size="sm">暂无异常洞察</Text> : anomalies.map((item) => (
          <Group key={`${item.kind}-${item.target}`} justify="space-between">
            <Text size="sm" lineClamp={1}>{item.target}</Text>
            <Group gap="xs"><Badge color={item.severity === 'high' ? 'red' : 'yellow'}>{item.reason}</Badge><Text size="xs" c="dimmed">{item.requests} 次</Text></Group>
          </Group>
        ))}
      </Stack>
    </SectionCard>
  )
}
