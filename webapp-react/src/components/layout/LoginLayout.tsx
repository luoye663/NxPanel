import { Box, Card, Center, Text, Title } from '@mantine/core'
import { Outlet } from 'react-router-dom'

export function LoginLayout() {
  return (
    <Center className="loginSurface" p="md">
      <Box className="loginGrid" aria-hidden="true" />
      <Card className="loginCard" shadow="sm">
        <Title order={1} size="h1" lh={1.05}>NxPanel</Title>
        <Text mt={6} mb="lg" size="sm" c="dimmed">开源 Nginx 网站管理面板</Text>
        <Outlet />
      </Card>
    </Center>
  )
}
