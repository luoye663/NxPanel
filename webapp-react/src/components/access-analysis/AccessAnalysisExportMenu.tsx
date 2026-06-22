import { Button, Group, Text } from '@mantine/core'
import { accessAnalysisExportURL, type AccessAnalysisQuery } from '@/api/accessAnalysis'

export function AccessAnalysisExportMenu({ siteId, query }: { siteId: string; query: AccessAnalysisQuery }) {
  function open(kind: 'paths' | 'ips' | 'entries') {
    window.open(accessAnalysisExportURL(siteId, kind, query), '_blank', 'noopener,noreferrer')
  }
  return (
    <Group justify="space-between">
      <Text size="sm" c="dimmed">导出会复用当前时间筛选，CSV 已做公式注入转义。</Text>
      <Group gap="xs"><Button variant="light" onClick={() => open('paths')}>导出路径</Button><Button variant="light" onClick={() => open('ips')}>导出 IP</Button><Button variant="light" onClick={() => open('entries')}>导出明细</Button></Group>
    </Group>
  )
}
