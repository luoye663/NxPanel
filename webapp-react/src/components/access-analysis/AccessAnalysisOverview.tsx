import { SimpleGrid, Stack, Text } from '@mantine/core'
import { SectionCard } from '@/components/common/SectionCard'
import { TimeCell } from '@/components/common/TimeCell'
import type { AccessAnalysisSummary } from '@/api/types'

export function AccessAnalysisOverview({ summary }: { summary?: AccessAnalysisSummary }) {
  const items = [
    ['今日访问量', summary?.today_requests || 0],
    ['独立 IP', summary?.unique_ips || 0],
    ['4xx', summary?.status_4xx || 0],
    ['5xx', summary?.status_5xx || 0],
    ['Top 路径', summary?.top_path || '-'],
    ['最后扫描', summary?.last_scan_at ? <TimeCell key="time" value={summary.last_scan_at} /> : '-'],
  ]
  return (
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 6 }} spacing="sm">
      {items.map(([label, value]) => (
        <SectionCard key={String(label)} title={String(label)}>
          <Stack gap={4}><Text fw={700} size="lg" lineClamp={1}>{value}</Text></Stack>
        </SectionCard>
      ))}
    </SimpleGrid>
  )
}
