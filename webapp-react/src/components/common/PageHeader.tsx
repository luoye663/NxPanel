import { Group, Stack, Text, Title } from '@mantine/core'
import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: string
  actions?: ReactNode
}

export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <Group justify="space-between" align="flex-start" gap="sm">
      <Stack gap={4}>
        <Title order={2}>{title}</Title>
        {subtitle ? <Text c="dimmed">{subtitle}</Text> : null}
      </Stack>
      {actions ? <Group gap="xs">{actions}</Group> : null}
    </Group>
  )
}
