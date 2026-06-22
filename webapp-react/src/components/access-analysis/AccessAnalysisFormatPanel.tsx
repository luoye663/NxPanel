import { Button, Code, Group, Stack, Text, Textarea } from '@mantine/core'
import { useState } from 'react'
import { SectionCard } from '@/components/common/SectionCard'
import type { AccessAnalysisFormatDetectResponse } from '@/api/types'

export function AccessAnalysisFormatPanel({ detecting, testing, optimizing, result, onDetect, onTest, onOptimize }: { detecting: boolean; testing: boolean; optimizing: boolean; result?: AccessAnalysisFormatDetectResponse; onDetect: (sample?: string) => void; onTest: (pattern: string, sample: string) => void; onOptimize: () => void }) {
  const [sample, setSample] = useState('')
  const [pattern, setPattern] = useState('')
  return (
    <SectionCard title="日志格式" description="格式检测由 Agent 读取少量样本，自定义正则需要包含 ip/time/method/path/status/bytes 命名字段。">
      <Stack gap="sm">
        <Textarea label="样本日志（可选）" minRows={3} value={sample} onChange={(event) => setSample(event.currentTarget.value)} />
        <Textarea label="自定义正则" minRows={2} value={pattern} onChange={(event) => setPattern(event.currentTarget.value)} />
        <Group><Button variant="light" loading={detecting} onClick={() => onDetect(sample)}>检测格式</Button><Button variant="light" loading={testing} onClick={() => onTest(pattern, sample)}>测试正则</Button><Button loading={optimizing} onClick={onOptimize}>优化为 nxpanel_json</Button></Group>
        {result ? <Text size="sm">检测结果: <Code>{result.format}</Code>，失败率 {(result.failure_rate * 100).toFixed(1)}%</Text> : null}
        {result?.recommended_conf ? <Code block>{result.recommended_conf}</Code> : null}
      </Stack>
    </SectionCard>
  )
}
