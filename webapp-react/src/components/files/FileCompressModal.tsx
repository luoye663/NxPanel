import { Button, Group, Modal, Radio, Stack, TextInput } from '@mantine/core'

interface FileCompressModalProps {
  opened: boolean
  selectedNames: string[]
  outputName: string
  format: 'zip' | 'tar.gz'
  submitting?: boolean
  onChange: (patch: Partial<{ outputName: string; format: 'zip' | 'tar.gz' }>) => void
  onClose: () => void
  onSubmit: () => void
}

export function FileCompressModal({ opened, selectedNames, outputName, format, submitting, onChange, onClose, onSubmit }: FileCompressModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title="压缩文件/目录" size="md" closeOnClickOutside={false}>
      <Stack gap="md">
        <TextInput label="选中项目" value={selectedNames.join(', ')} disabled />
        <Radio.Group label="压缩格式" value={format} onChange={(value) => onChange({ format: value as 'zip' | 'tar.gz' })}>
          <Group mt="xs">
            <Radio value="zip" label="ZIP" />
            <Radio value="tar.gz" label="TAR.GZ" />
          </Group>
        </Radio.Group>
        <TextInput label="输出文件名" value={outputName} onChange={(event) => onChange({ outputName: event.currentTarget.value })} placeholder="filename-20260601-1530.zip" />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>取消</Button>
          <Button loading={submitting} onClick={onSubmit}>开始压缩</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
