import { Code, Text, Tooltip } from '@mantine/core'

interface MonoTextProps {
  value?: string | null
  empty?: string
  maxWidth?: number | string
}

export function MonoText({ value, empty = '-', maxWidth = 260 }: MonoTextProps) {
  if (!value) {
    return <Text size="sm" c="dimmed">{empty}</Text>
  }

  return (
    <Tooltip label={value} multiline maw={520} openDelay={300}>
      <Code className="monoEllipsis" style={{ maxWidth }}>
        {value}
      </Code>
    </Tooltip>
  )
}
