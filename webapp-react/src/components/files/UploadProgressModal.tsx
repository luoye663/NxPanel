import { Badge, Button, Group, Modal, Progress, Stack, Text } from '@mantine/core'
import type { UploadQueueItem } from '@/hooks/useUploadQueue'

interface UploadProgressModalProps {
  opened: boolean
  uploading: boolean
  items: UploadQueueItem[]
  onClose: () => void
  onCancel: () => void
}

const statusLabel: Record<UploadQueueItem['status'], { label: string; color: string }> = {
  uploading: { label: '上传中', color: 'blue' },
  done: { label: '完成', color: 'green' },
  error: { label: '失败', color: 'red' },
  cancelled: { label: '已取消', color: 'gray' },
}

export function UploadProgressModal({ opened, uploading, items, onClose, onCancel }: UploadProgressModalProps) {
  return (
    <Modal opened={opened} onClose={uploading ? () => undefined : onClose} title="上传文件" size="sm" closeOnClickOutside={!uploading} withCloseButton={!uploading}>
      <Stack gap="md">
        {items.map((item) => {
          const status = statusLabel[item.status]
          return (
            <Stack key={item.id} gap={5}>
              <Group justify="space-between" gap="xs" wrap="nowrap">
                <Text size="sm" className="monoEllipsis">{item.name}</Text>
                <Badge color={status.color} variant="light">{status.label}</Badge>
              </Group>
              <Progress value={item.percent} color={item.status === 'error' ? 'red' : item.status === 'cancelled' ? 'gray' : 'blue'} size="sm" />
            </Stack>
          )
        })}
        <Group justify="flex-end">
          {uploading ? <Button color="red" variant="light" onClick={onCancel}>取消上传</Button> : <Button variant="default" onClick={onClose}>关闭</Button>}
        </Group>
      </Stack>
    </Modal>
  )
}
