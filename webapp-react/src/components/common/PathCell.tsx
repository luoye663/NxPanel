import { ActionIcon, Code, Group, Tooltip } from '@mantine/core'
import { IconCopy } from '@tabler/icons-react'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface PathCellProps {
  value?: string | null
  maxWidth?: number | string
  onClick?: () => void
  justify?: 'flex-start' | 'flex-end'
  wrap?: boolean
}

export function PathCell({ value, maxWidth = 300, onClick, justify = 'flex-start', wrap = false }: PathCellProps) {
  async function handleCopy() {
    if (!value) return
    try {
      await navigator.clipboard.writeText(value)
      notifySuccess({ message: '路径已复制' })
    } catch {
      notifyWarning({ message: '复制失败，请手动选择路径' })
    }
  }

  if (!value) return <Code>-</Code>

  return (
    <Group gap={4} wrap="nowrap" justify={justify} align="center" style={{ minWidth: 0 }}>
      <Tooltip label={value} multiline maw={520} openDelay={300}>
        <Code
          className={wrap ? undefined : 'monoEllipsis'}
          component={onClick ? 'button' : 'span'}
          onClick={onClick}
          style={{ maxWidth, cursor: onClick ? 'pointer' : 'default', border: 0, minWidth: 0, ...(wrap ? { whiteSpace: 'normal', overflowWrap: 'anywhere' } : {}) }}
        >
          {value}
        </Code>
      </Tooltip>
      <Tooltip label="复制路径">
        <ActionIcon variant="subtle" aria-label="复制路径" onClick={handleCopy}>
          <IconCopy size={14} />
        </ActionIcon>
      </Tooltip>
    </Group>
  )
}
