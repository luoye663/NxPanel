import { Button, Group, Modal, Stack, TextInput } from '@mantine/core'

interface FileExtractModalProps {
  opened: boolean
  archivePath: string
  destDir: string
  submitting?: boolean
  onDestDirChange: (value: string) => void
  onClose: () => void
  onSubmit: () => void
}

export function FileExtractModal({ opened, archivePath, destDir, submitting, onDestDirChange, onClose, onSubmit }: FileExtractModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title="解压文件" size="md" closeOnClickOutside={false}>
      <Stack gap="md">
        <TextInput label="压缩包" value={archivePath} disabled />
        <TextInput label="解压到" value={destDir} onChange={(event) => onDestDirChange(event.currentTarget.value)} placeholder="目标目录" />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>取消</Button>
          <Button loading={submitting} onClick={onSubmit}>开始解压</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
