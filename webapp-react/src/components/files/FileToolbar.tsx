import { Button, Group, Text } from '@mantine/core'
import { IconArchive, IconClipboard, IconCopy, IconDownload, IconFilePlus, IconFolderPlus, IconKey, IconRefresh, IconScissors, IconTrash, IconUpload, IconX } from '@tabler/icons-react'
import type { FileClipboardState } from '@/hooks/useFileClipboard'

interface FileToolbarProps {
  selectedCount: number
  clipboard: FileClipboardState | null
  loading?: boolean
  onNewFile: () => void
  onNewDir: () => void
  onUpload: () => void
  onDownload: () => void
  onCopy: () => void
  onCut: () => void
  onPaste: () => void
  onPermission: () => void
  onCompress: () => void
  onClearClipboard: () => void
  onDeleteSelected: () => void
  onRefresh: () => void
}

export function FileToolbar({ selectedCount, clipboard, loading, onNewFile, onNewDir, onUpload, onDownload, onCopy, onCut, onPaste, onPermission, onCompress, onClearClipboard, onDeleteSelected, onRefresh }: FileToolbarProps) {
  return (
    <Group justify="space-between" align="center" gap="sm" className="fileToolbar">
      <Group gap="xs">
        <Button leftSection={<IconFilePlus size={16} />} onClick={onNewFile}>新建文件</Button>
        <Button variant="light" leftSection={<IconFolderPlus size={16} />} onClick={onNewDir}>新建目录</Button>
        <Button variant="light" leftSection={<IconUpload size={16} />} onClick={onUpload}>上传</Button>
        <Button variant="light" leftSection={<IconRefresh size={16} />} loading={loading} onClick={onRefresh}>刷新</Button>
      </Group>
      <Group gap="xs">
        {selectedCount > 0 ? <Text size="sm" c="dimmed">已选择 {selectedCount} 项</Text> : null}
        {selectedCount > 0 ? <Button variant="light" leftSection={<IconDownload size={16} />} onClick={onDownload}>下载</Button> : null}
        {selectedCount > 0 ? <Button variant="light" leftSection={<IconCopy size={16} />} onClick={onCopy}>复制</Button> : null}
        {selectedCount > 0 ? <Button variant="light" leftSection={<IconScissors size={16} />} onClick={onCut}>剪切</Button> : null}
        {selectedCount > 0 ? <Button variant="light" leftSection={<IconKey size={16} />} onClick={onPermission}>权限</Button> : null}
        {selectedCount > 0 ? <Button variant="light" leftSection={<IconArchive size={16} />} onClick={onCompress}>压缩</Button> : null}
        {selectedCount > 0 ? <Button color="red" variant="light" leftSection={<IconTrash size={16} />} onClick={onDeleteSelected}>删除</Button> : null}
        {clipboard ? <Button variant="outline" leftSection={<IconClipboard size={16} />} onClick={onPaste}>粘贴到当前目录 ({clipboard.paths.length})</Button> : null}
        {clipboard ? <Button variant="subtle" leftSection={<IconX size={16} />} onClick={onClearClipboard}>清空剪贴板</Button> : null}
      </Group>
    </Group>
  )
}
