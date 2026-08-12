import { Box, Card, Center, Text, Title } from '@mantine/core'
import { Outlet } from 'react-router-dom'
import { useBranding } from '@/branding/BrandingProvider'

export function LoginLayout() {
  const { branding } = useBranding()

  return (
    <Center className="loginSurface" p="md">
      <Box className="loginGrid" aria-hidden="true" />
      <Card className="loginCard" shadow="sm">
        <Title order={1} size="h1" lh={1.05} mb={branding.subtitle ? 0 : 'lg'} className="brandText">{branding.site_name}</Title>
        {branding.subtitle ? <Text mt={6} mb="lg" size="sm" c="dimmed">{branding.subtitle}</Text> : null}
        <Outlet />
      </Card>
    </Center>
  )
}
