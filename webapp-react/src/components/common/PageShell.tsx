import { Stack, type StackProps } from '@mantine/core'
import type { PropsWithChildren } from 'react'

export function PageShell({ children, ...props }: PropsWithChildren<StackProps>) {
  return (
    <Stack gap="sm" className="pageShell" {...props}>
      {children}
    </Stack>
  )
}
