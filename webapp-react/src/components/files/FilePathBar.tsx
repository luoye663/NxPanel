import { ActionIcon, Button, Group, ScrollArea, TextInput, Tooltip } from '@mantine/core'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { IconArrowUp, IconCheck, IconCopy, IconEdit, IconX } from '@tabler/icons-react'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface FilePathBarPart {
  label: string
  path: string
  locked: boolean
}

interface FilePathBarProps {
  currentPath: string
  parts: FilePathBarPart[]
  onNavigate: (path: string) => boolean
}

function normalizeInputPath(value: string): string {
  const sanitized = value.trim().replace(/\\+/g, '/').replace(/\/+/g, '/')
  if (!sanitized) return ''
  if (sanitized === '/') return '/'
  const prefixed = sanitized.startsWith('/') ? sanitized : `/${sanitized}`
  return prefixed.length > 1 ? prefixed.replace(/\/+$/, '') : prefixed
}

function getParentPath(path: string): string {
  if (!path || path === '/') return ''
  const parts = path.split('/').filter(Boolean)
  if (parts.length <= 1) return '/'
  return `/${parts.slice(0, -1).join('/')}`
}

export function FilePathBar({ currentPath, parts, onNavigate }: FilePathBarProps) {
  const [editing, setEditing] = useState(false)
  const [draftPath, setDraftPath] = useState(currentPath)
  const displayParts = useMemo(() => (parts.length > 0 ? parts : [{ label: '/', path: '/', locked: false }]), [parts])
  const parentPath = useMemo(() => getParentPath(currentPath), [currentPath])

  useEffect(() => {
    if (!editing) setDraftPath(currentPath)
  }, [currentPath, editing])

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented) return
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'l') {
        event.preventDefault()
        setEditing(true)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  async function copyPath() {
    try {
      await navigator.clipboard.writeText(currentPath)
      notifySuccess({ message: '路径已复制' })
    } catch {
      notifyWarning({ message: '复制失败，请手动选择路径' })
    }
  }

  function submitPath() {
    const nextPath = normalizeInputPath(draftPath)
    if (!nextPath) {
      notifyWarning({ message: '请输入有效路径' })
      return
    }
    if (onNavigate(nextPath)) setEditing(false)
  }

  function cancelEdit() {
    setDraftPath(currentPath)
    setEditing(false)
  }

  return (
    <Group justify="space-between" align="center" gap="sm" wrap="nowrap" className="filePathBar">
      {editing ? (
        <TextInput
          className="filePathInput"
          value={draftPath}
          onChange={(event) => setDraftPath(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') submitPath()
            if (event.key === 'Escape') cancelEdit()
          }}
          autoFocus
          leftSection={<IconEdit size={14} />}
          rightSectionWidth={76}
          rightSection={
            <Group gap={4} wrap="nowrap">
              <ActionIcon variant="subtle" aria-label="确认跳转" onClick={submitPath}>
                <IconCheck size={14} />
              </ActionIcon>
              <ActionIcon variant="subtle" aria-label="取消编辑" onClick={cancelEdit}>
                <IconX size={14} />
              </ActionIcon>
            </Group>
          }
        />
      ) : (
        <Group className="filePathView" gap={8} wrap="nowrap" align="center" onDoubleClick={() => setEditing(true)}>
          <Tooltip label={parentPath ? '上一级' : '已在根目录'}>
            <ActionIcon variant="subtle" aria-label="上一级" disabled={!parentPath} onClick={() => parentPath && onNavigate(parentPath)}>
              <IconArrowUp size={14} />
            </ActionIcon>
          </Tooltip>
          <ScrollArea className="filePathScroll" type="never" scrollbars="x">
            <Group className="filePathSegments" gap={0} wrap="nowrap" align="center">
              {displayParts.map((part, index) => (
                <Fragment key={part.path}>
                  {index > 0 ? <span className="filePathSeparator">/</span> : null}
                  <Tooltip label={part.path} multiline maw={520}>
                    <Button
                      size="compact-xs"
                      className="filePathToken"
                      variant={part.path === currentPath ? 'filled' : 'subtle'}
                      color={part.path === currentPath ? 'dark' : 'gray'}
                      disabled={part.locked}
                      onClick={() => onNavigate(part.path)}
                    >
                      {part.label}
                    </Button>
                  </Tooltip>
                </Fragment>
              ))}
            </Group>
          </ScrollArea>
          <ActionIcon variant="subtle" aria-label="编辑路径" onClick={() => setEditing(true)}>
            <IconEdit size={14} />
          </ActionIcon>
          <Tooltip label="复制当前路径">
            <ActionIcon variant="subtle" aria-label="复制当前路径" onClick={copyPath}>
              <IconCopy size={14} />
            </ActionIcon>
          </Tooltip>
        </Group>
      )}
    </Group>
  )
}
