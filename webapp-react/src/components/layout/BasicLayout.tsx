import { AppShell, Badge, Burger, Group, NavLink, Text, Title, UnstyledButton, rem } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import {
  IconDashboard,
  IconFolder,
  IconPalette,
  IconLogout,
  IconCalendarTime,
  IconServer,
  IconSettings,
  IconWorld,
} from '@tabler/icons-react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useSystemOverview } from '@/api/hooks'
import { useAuth } from '@/auth/AuthProvider'
import { useBranding } from '@/branding/BrandingProvider'
import { notifySuccess } from '@/utils/notify'
import { VersionUpdate } from './VersionUpdate'

const menuItems = [
  { path: '/', label: '仪表盘', icon: IconDashboard },
  { path: '/sites', label: '网站管理', icon: IconWorld },
  { path: '/files', label: '文件管理', icon: IconFolder },
  { path: '/nginx', label: 'Nginx 管理', icon: IconServer },
  { path: '/scheduled-tasks', label: '计划任务', icon: IconCalendarTime },
  { path: '/logs', label: '日志', icon: IconSettings },
  { path: '/panel-settings', label: '面板设置', icon: IconPalette },
]
export function BasicLayout() {
  const [opened, { toggle, close }] = useDisclosure()
  const location = useLocation()
  const auth = useAuth()
  const { branding } = useBranding()
  const systemOverviewQuery = useSystemOverview()
  const systemOverview = systemOverviewQuery.data
  const activeItem = menuItems.find((item) => item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path))
  const agentAvailable = systemOverview?.agent.available
  const agentStatusLabel = agentAvailable === undefined ? '未知' : agentAvailable ? '可用' : '不可用'
  const agentStatusColor = agentAvailable === undefined ? 'gray' : agentAvailable ? 'green' : 'red'

  async function handleLogout() {
    await auth.logout()
    notifySuccess({ message: '已退出登录' })
  }

  return (
    <AppShell
      layout="alt"
      padding="md"
      header={{ height: 60 }}
      navbar={{ width: 248, breakpoint: 'sm', collapsed: { mobile: !opened } }}
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="sm">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" aria-label="切换导航菜单" />
            <div>
              <Title order={4} lh={1.1}>{activeItem?.label || branding.site_name}</Title>
            </div>
          </Group>
          <Group gap="xs" wrap="nowrap">
            <VersionUpdate />
            <Badge color={agentStatusColor} variant="outline">
              Agent状态: {agentStatusLabel}
            </Badge>
            <UnstyledButton aria-label="退出登录" className="topAction" onClick={handleLogout}>
              <Group gap={6} wrap="nowrap">
                <IconLogout size={rem(16)} />
                <Text size="sm">{auth.username || '退出'}</Text>
              </Group>
            </UnstyledButton>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="md">
        <Group mb="lg" gap="sm" wrap="nowrap" align="flex-start">
          <div className="brandMark">{Array.from(branding.site_name)[0] || 'N'}</div>
          <div className="brandCopy">
            <Text fw={700} className="brandText">{branding.site_name}</Text>
            {branding.subtitle ? <Text size="xs" c="dimmed" className="brandText">{branding.subtitle}</Text> : null}
          </div>
        </Group>

        {menuItems.map((item) => {
          const active = item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path)
          const Icon = item.icon
          return (
            <NavLink
              key={item.path}
              component={Link}
              to={item.path}
              label={item.label}
              className="mainNavLink"
              active={active}
              leftSection={<Icon size={rem(18)} />}
              onClick={close}
              variant="light"
              mb={4}
            />
          )
        })}
      </AppShell.Navbar>

      <AppShell.Main>
        {/* alt 布局让侧栏顶到浏览器顶部，顶部栏仅覆盖右侧工作区。 */}
        <Outlet />
      </AppShell.Main>
    </AppShell>
  )
}
