import { Text } from '@mantine/core'
import { SectionCard } from '@/components/common/SectionCard'
import type { AccessAnalysisScanResponse } from '@/api/types'

export function AccessAnalysisScanPanel({ lastResult, embedded }: { lastResult?: AccessAnalysisScanResponse; embedded?: boolean }) {
  const content = lastResult ? <Text size="sm" c="dimmed">最近扫描: {lastResult.scanned_lines} 行，跳过 {lastResult.skipped_lines} 行，耗时 {lastResult.duration_ms}ms{lastResult.truncated ? '，已触发限制截断' : ''}</Text> : null
  if (embedded) return content
  return (
    <SectionCard title="扫描控制" description="手动扫描只传日期范围，日志路径始终由后端根据站点记录读取。">
      {content}
    </SectionCard>
  )
}
