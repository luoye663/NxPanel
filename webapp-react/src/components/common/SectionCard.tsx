import { Card, Stack, Text, Title, type CardProps } from '@mantine/core'
import type { KeyboardEventHandler, MouseEventHandler, PropsWithChildren, ReactNode } from 'react'

interface SectionCardProps extends CardProps {
  title?: string
  description?: string
  actions?: ReactNode
  onClick?: MouseEventHandler<HTMLDivElement>
  onKeyDown?: KeyboardEventHandler<HTMLDivElement>
  role?: string
  tabIndex?: number
}

export function SectionCard({ title, description, actions, children, ...props }: PropsWithChildren<SectionCardProps>) {
  return (
    <Card p="md" {...props}>
      {(title || description || actions) ? (
        <div className="sectionCardHeader">
          <Stack gap={3}>
            {title ? <Title order={4} size="h5">{title}</Title> : null}
            {description ? <Text size="sm" c="dimmed">{description}</Text> : null}
          </Stack>
          {actions}
        </div>
      ) : null}
      {children}
    </Card>
  )
}
