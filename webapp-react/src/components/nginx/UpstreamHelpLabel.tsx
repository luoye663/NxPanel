import { ActionIcon, Group, Text, Tooltip } from '@mantine/core'
import { IconInfoCircle } from '@tabler/icons-react'
import type { ReactNode } from 'react'

interface UpstreamHelpLabelProps {
  label: ReactNode
  help: ReactNode
  ariaLabel?: string
}

export function UpstreamHelpLabel({ label, help, ariaLabel }: UpstreamHelpLabelProps) {
  const accessibleLabel = ariaLabel || (typeof label === 'string' ? `${label}说明` : '配置项说明')

  return (
    <Group component="span" gap={5} wrap="nowrap">
      <Text component="span" size="sm" fw={500}>{label}</Text>
      <Tooltip label={help} multiline w={300} openDelay={200} withinPortal>
        <ActionIcon
          type="button"
          className="upstreamHelpIcon"
          variant="subtle"
          color="gray"
          size={20}
          aria-label={accessibleLabel}
          onMouseDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
        >
          <IconInfoCircle size={15} />
        </ActionIcon>
      </Tooltip>
    </Group>
  )
}
