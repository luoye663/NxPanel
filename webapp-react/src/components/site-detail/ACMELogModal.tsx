import { Badge, Group, Modal, Stack, Text } from '@mantine/core'
import { useState } from 'react'
import { getOrderLogSSEUrl } from '@/api/acme'
import { LogViewer } from '@/components/logs/LogViewer'
import { useEventSource } from '@/hooks/useEventSource'
import { notifyError } from '@/utils/notify'

interface ACMELogModalProps {
  orderId: string | null
  opened: boolean
  onClose: () => void
  onDone: () => void
}

export function ACMELogModal({ orderId, opened, onClose, onDone }: ACMELogModalProps) {
  const [lines, setLines] = useState<string[]>([])
  const url = opened && orderId ? getOrderLogSSEUrl(orderId) : null
  const stream = useEventSource(url, {
    onMessage: (event) => setLines((current) => [...current, event.data]),
    onDone,
    onError: () => notifyError({ message: '日志连接中断' }),
  })

  function handleClose() {
    stream.close()
    onClose()
  }

  return (
    <Modal opened={opened} onClose={handleClose} title="申请证书" size="lg" closeOnClickOutside={false} centered>
      <Stack gap="sm">
        <Group gap="xs"><Text size="sm" c="dimmed">日志连接</Text><Badge variant="light">{stream.status}</Badge></Group>
        {/* 日志 modal 关闭或 orderId 变化时 useEventSource 会关闭旧连接，避免重复消费 SSE。 */}
        <LogViewer lines={lines} emptyText="等待日志..." />
      </Stack>
    </Modal>
  )
}
