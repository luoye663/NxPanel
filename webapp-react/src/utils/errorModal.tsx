import { Alert, Button, Code, Group, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useState } from 'react'
import { ApiError } from '@/api/client'
import { getWhitelistGuidance, sanitizeDetails } from '@/components/common/ErrorAlert'

const wrap = { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' } as const

function ErrorModalContent({ error, onClose }: { error: unknown; onClose: () => void }) {
  const [copyStatus, setCopyStatus] = useState('')
  const apiError = error instanceof ApiError ? error : null
  const message = apiError?.message || (error instanceof Error ? error.message : '未知错误')
  const details = sanitizeDetails(apiError?.details ?? {}) as Record<string, unknown>
  const { stderr, operation_id: operationId, ...otherDetails } = details
  const guidance = getWhitelistGuidance(apiError, message)
  const identifiers = [
    typeof operationId === 'string' ? `操作 ID：${operationId}` : '',
    apiError?.request_id ? `请求 ID：${apiError.request_id}` : '',
  ].filter(Boolean)
  const stderrText = typeof stderr === 'string' ? stderr : ''
  const jsonDetails = {
    ...otherDetails,
    ...(stderr !== undefined && typeof stderr !== 'string' ? { stderr } : {}),
    ...(operationId !== undefined && typeof operationId !== 'string' ? { operation_id: operationId } : {}),
  }
  const json = Object.keys(jsonDetails).length ? JSON.stringify(jsonDetails, null, 2) : ''

  async function copy() {
    try {
      await navigator.clipboard.writeText([message, ...guidance, ...identifiers, stderrText, json].filter(Boolean).join('\n\n'))
      setCopyStatus('已复制')
    } catch {
      setCopyStatus('无法访问剪贴板，请手动选择并复制。')
    }
  }

  return (
    <Stack gap="md">
      <div role="region" aria-label="错误详情" tabIndex={0} style={{ maxHeight: 'min(55dvh, 32rem)', overflowY: 'auto' }}>
        <Alert color="red">
          <Stack gap="xs">
            {[message, ...guidance].map((text, index) => <Text key={index} size="sm" style={wrap}>{text}</Text>)}
            {identifiers.map((text) => <Text key={text} size="xs" c="dimmed" style={wrap}>{text}</Text>)}
            {stderrText ? <Code block style={wrap}>{stderrText}</Code> : null}
            {json ? <Code block style={wrap}>{json}</Code> : null}
          </Stack>
        </Alert>
      </div>
      <Group justify="space-between">
        <Text role="status" size="xs" c="dimmed">{copyStatus}</Text>
        <Group gap="xs">
          <Button variant="default" onClick={() => void copy()}>复制详情</Button>
          <Button onClick={onClose}>关闭</Button>
        </Group>
      </Group>
    </Stack>
  )
}

export function showErrorModal(error: unknown, title = '操作失败') {
  const modalId = modals.open({
    title: <span style={{ overflowWrap: 'anywhere' }}>{title}</span>,
    size: 'lg',
    centered: true,
    children: <ErrorModalContent error={error} onClose={() => modals.close(modalId)} />,
  })
}
