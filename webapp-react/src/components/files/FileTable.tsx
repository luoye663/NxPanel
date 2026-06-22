import { ActionIcon, Badge, Checkbox, Group, Loader, Menu, ScrollArea, Table, Text, Tooltip } from '@mantine/core'
import { IconArchive, IconDotsVertical, IconDownload, IconEdit, IconEye, IconFolder, IconKey, IconTrash } from '@tabler/icons-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { FileEntry } from '@/api/types'
import { isArchiveFile, isEditableText, isImageFile } from '@/utils/fileType'

interface FileTableProps {
  entries: FileEntry[]
  loading?: boolean
  selectedSet: Set<string>
  onToggle: (name: string) => void
  onToggleAll: (checked: boolean) => void
  onOpen: (entry: FileEntry) => void
  onEdit: (entry: FileEntry) => void
  onPreview: (entry: FileEntry) => void
  onDownload: (entry: FileEntry) => void
  onExtract: (entry: FileEntry) => void
  onPermission: (entry: FileEntry) => void
  onRename: (entry: FileEntry) => void
  onDelete: (entry: FileEntry) => void
}

const ROW_HEIGHT = 48
const OVERSCAN_ROWS = 8

function formatMode(mode: string, isDir: boolean): string {
  const parsed = Number.parseInt(mode, 8)
  if (Number.isNaN(parsed)) return mode || '-'
  const chars = ['---', '--x', '-w-', '-wx', 'r--', 'r-x', 'rw-', 'rwx']
  return `${isDir ? 'd' : '-'}${chars[(parsed >> 6) & 7]}${chars[(parsed >> 3) & 7]}${chars[parsed & 7]}`
}

function formatSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 * 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`
  return `${(size / 1024 / 1024 / 1024).toFixed(1)} GB`
}

function formatTime(value: string): string {
  return value?.replace('T', ' ').substring(0, 19) || '-'
}

function fileKind(entry: FileEntry): { label: string; color: string } {
  if (entry.is_dir) return { label: '目录', color: 'blue' }
  const ext = entry.name.split('.').pop()?.toLowerCase() || 'file'
  if (['zip', 'tar', 'gz', 'tgz'].includes(ext)) return { label: '压缩包', color: 'orange' }
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico'].includes(ext)) return { label: '图片', color: 'violet' }
  if (['conf', 'json', 'yaml', 'yml', 'txt', 'log', 'html', 'css', 'js', 'ts', 'md'].includes(ext)) return { label: '文本', color: 'gray' }
  return { label: ext.toUpperCase(), color: 'gray' }
}

export function FileTable({ entries, loading, selectedSet, onToggle, onToggleAll, onOpen, onEdit, onPreview, onDownload, onExtract, onPermission, onRename, onDelete }: FileTableProps) {
  const allChecked = entries.length > 0 && selectedSet.size === entries.length
  const indeterminate = selectedSet.size > 0 && selectedSet.size < entries.length
  const viewportRef = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [viewportHeight, setViewportHeight] = useState(0)

  useEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    const viewportElement = viewport

    function updateViewport() {
      setViewportHeight(viewportElement.clientHeight)
      setScrollTop(viewportElement.scrollTop)
    }

    updateViewport()
    viewportElement.addEventListener('scroll', updateViewport, { passive: true })
    window.addEventListener('resize', updateViewport)

    let resizeObserver: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(updateViewport)
      resizeObserver.observe(viewport)
    }

    return () => {
      viewportElement.removeEventListener('scroll', updateViewport)
      window.removeEventListener('resize', updateViewport)
      resizeObserver?.disconnect()
    }
  }, [])

  const { startIndex, endIndex, topSpacerHeight, bottomSpacerHeight } = useMemo(() => {
    if (entries.length === 0) {
      return { startIndex: 0, endIndex: 0, topSpacerHeight: 0, bottomSpacerHeight: 0 }
    }

    if (viewportHeight <= 0) {
      return { startIndex: 0, endIndex: Math.min(entries.length, OVERSCAN_ROWS * 2), topSpacerHeight: 0, bottomSpacerHeight: Math.max(0, (entries.length - OVERSCAN_ROWS * 2) * ROW_HEIGHT) }
    }

    const visibleRows = Math.ceil(viewportHeight / ROW_HEIGHT)
    const firstVisible = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN_ROWS)
    const lastVisible = Math.min(entries.length, firstVisible + visibleRows + OVERSCAN_ROWS * 2)
    return {
      startIndex: firstVisible,
      endIndex: lastVisible,
      topSpacerHeight: firstVisible * ROW_HEIGHT,
      bottomSpacerHeight: Math.max(0, (entries.length - lastVisible) * ROW_HEIGHT),
    }
  }, [entries.length, scrollTop, viewportHeight])

  const visibleEntries = useMemo(() => entries.slice(startIndex, endIndex), [entries, endIndex, startIndex])

  if (loading) {
    return <Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载目录...</Text></Group>
  }

  if (entries.length === 0) {
    return <Group justify="center" py="xl"><Text c="dimmed">空目录</Text></Group>
  }

  return (
    <ScrollArea h="min(620px, 66vh)" className="fileTableScroll" viewportRef={viewportRef}>
      <Table stickyHeader highlightOnHover className="fileTable">
        <Table.Thead>
          <Table.Tr>
            <Table.Th w={44}><Checkbox aria-label="全选文件" checked={allChecked} indeterminate={indeterminate} onChange={(event) => onToggleAll(event.currentTarget.checked)} /></Table.Th>
            <Table.Th>名称</Table.Th>
            <Table.Th w={110}>类型</Table.Th>
            <Table.Th w={120}>权限</Table.Th>
            <Table.Th w={160}>所有者</Table.Th>
            <Table.Th w={110}>大小</Table.Th>
            <Table.Th w={180}>修改时间</Table.Th>
            <Table.Th w={80}>操作</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {topSpacerHeight > 0 ? (
            <Table.Tr aria-hidden="true">
              <Table.Td colSpan={8} style={{ height: topSpacerHeight, padding: 0, border: 0 }} />
            </Table.Tr>
          ) : null}
          {visibleEntries.map((entry) => {
            const kind = fileKind(entry)
            return (
              <Table.Tr key={entry.name} className={selectedSet.has(entry.name) ? 'fileRowSelected' : undefined}>
                <Table.Td><Checkbox aria-label={`选择 ${entry.name}`} checked={selectedSet.has(entry.name)} onChange={() => onToggle(entry.name)} /></Table.Td>
                <Table.Td>
                  <Group gap="xs" wrap="nowrap">
                    {entry.is_dir ? <IconFolder size={17} color="var(--mantine-color-blue-6)" /> : <Text c="dimmed" ff="monospace">#</Text>}
                    <Tooltip label={entry.name} openDelay={300}>
                      <Text component="button" type="button" className="fileNameButton monoEllipsis" onClick={() => onOpen(entry)}>{entry.name}</Text>
                    </Tooltip>
                  </Group>
                </Table.Td>
                <Table.Td><Badge color={kind.color} variant="light">{kind.label}</Badge></Table.Td>
                <Table.Td><Text ff="monospace" size="sm">{formatMode(entry.mode, entry.is_dir)}</Text></Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{entry.owner || '-'}:{entry.group || '-'}</Text></Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{entry.is_dir ? '-' : formatSize(entry.size)}</Text></Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{formatTime(entry.mod_time)}</Text></Table.Td>
                <Table.Td>
                  <Menu withinPortal position="bottom-end" shadow="md">
                    <Menu.Target>
                      <ActionIcon variant="subtle" aria-label={`${entry.name} 操作`}><IconDotsVertical size={16} /></ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      {!entry.is_dir && isEditableText(entry.name) ? <Menu.Item leftSection={<IconEdit size={15} />} onClick={() => onEdit(entry)}>编辑</Menu.Item> : null}
                      {!entry.is_dir && isImageFile(entry.name) ? <Menu.Item leftSection={<IconEye size={15} />} onClick={() => onPreview(entry)}>预览</Menu.Item> : null}
                      {!entry.is_dir ? <Menu.Item leftSection={<IconDownload size={15} />} onClick={() => onDownload(entry)}>下载</Menu.Item> : null}
                      {!entry.is_dir && isArchiveFile(entry.name) ? <Menu.Item leftSection={<IconArchive size={15} />} onClick={() => onExtract(entry)}>解压</Menu.Item> : null}
                      <Menu.Item leftSection={<IconKey size={15} />} onClick={() => onPermission(entry)}>权限</Menu.Item>
                      <Menu.Item leftSection={<IconEdit size={15} />} onClick={() => onRename(entry)}>重命名</Menu.Item>
                      <Menu.Item color="red" leftSection={<IconTrash size={15} />} onClick={() => onDelete(entry)}>删除</Menu.Item>
                    </Menu.Dropdown>
                  </Menu>
                </Table.Td>
              </Table.Tr>
            )
          })}
          {bottomSpacerHeight > 0 ? (
            <Table.Tr aria-hidden="true">
              <Table.Td colSpan={8} style={{ height: bottomSpacerHeight, padding: 0, border: 0 }} />
            </Table.Tr>
          ) : null}
        </Table.Tbody>
      </Table>
    </ScrollArea>
  )
}
