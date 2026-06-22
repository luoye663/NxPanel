import { Button, Checkbox, Group, Modal, Stack, TextInput } from '@mantine/core'
import type { FileEntry } from '@/api/types'

interface FilePermissionModalProps {
  opened: boolean
  entry: FileEntry | null
  path: string
  mode: string
  owner: string
  group: string
  recursive: boolean
  submitting?: boolean
  onChange: (patch: Partial<{ mode: string; owner: string; group: string; recursive: boolean }>) => void
  onClose: () => void
  onSubmit: () => void
}

function formatMode(mode: string, isDir: boolean): string {
  const parsed = Number.parseInt(mode, 8)
  if (Number.isNaN(parsed)) return mode || '-'
  const chars = ['---', '--x', '-w-', '-wx', 'r--', 'r-x', 'rw-', 'rwx']
  return `${isDir ? 'd' : '-'}${chars[(parsed >> 6) & 7]}${chars[(parsed >> 3) & 7]}${chars[parsed & 7]}`
}

export function FilePermissionModal({ opened, entry, path, mode, owner, group, recursive, submitting, onChange, onClose, onSubmit }: FilePermissionModalProps) {
  return (
    <Modal opened={opened} onClose={onClose} title="修改权限 / 所有者" size="md" closeOnClickOutside={false}>
      <Stack gap="md">
        <TextInput label="文件路径" value={path} disabled />
        <TextInput label="权限" value={mode} onChange={(event) => onChange({ mode: event.currentTarget.value })} placeholder="如 755 / 644" rightSectionWidth={92} rightSection={<code>{formatMode(mode, Boolean(entry?.is_dir))}</code>} />
        <TextInput label="所有者" value={owner} onChange={(event) => onChange({ owner: event.currentTarget.value })} placeholder="用户名 或 UID" />
        <TextInput label="用户组" value={group} onChange={(event) => onChange({ group: event.currentTarget.value })} placeholder="组名 或 GID" />
        <Checkbox label="应用到子目录" checked={recursive} onChange={(event) => onChange({ recursive: event.currentTarget.checked })} />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>取消</Button>
          <Button loading={submitting} onClick={onSubmit}>确认修改</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
