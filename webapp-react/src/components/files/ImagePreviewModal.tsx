import { Image, Modal, Stack, Text } from '@mantine/core'

interface ImagePreviewModalProps {
  opened: boolean
  title: string
  path: string
  url: string
  onClose: () => void
}

export function ImagePreviewModal({ opened, title, path, url, onClose }: ImagePreviewModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title={title || '图片预览'} size="auto">
      <Stack gap="sm" className="fileImagePreview">
        <Text ff="monospace" size="xs" c="dimmed" className="monoEllipsis">{path}</Text>
        <Image src={url} alt={title} fit="contain" maw="min(82vw, 1100px)" mah="72vh" />
      </Stack>
    </Modal>
  )
}
