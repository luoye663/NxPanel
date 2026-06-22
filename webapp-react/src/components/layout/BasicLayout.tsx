import { ActionIcon, Anchor, AppShell, Badge, Burger, Group, NavLink, Popover, Stack, Text, Title, Tooltip, UnstyledButton, rem } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import {
  IconDashboard,
  IconExternalLink,
  IconFolder,
  IconLock,
  IconLogout,
  IconCalendarTime,
  IconRefresh,
  IconServer,
  IconSettings,
  IconWorld,
} from '@tabler/icons-react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useSystemOverview, useUpgradeCheckMutation, useUpgradeStatus } from '@/api/hooks'
import { useAuth } from '@/auth/AuthProvider'
import { notifyError, notifySuccess } from '@/utils/notify'

const menuItems = [
  { path: '/', label: '仪表盘', icon: IconDashboard },
  { path: '/sites', label: '网站管理', icon: IconWorld },
  { path: '/files', label: '文件管理', icon: IconFolder },
  { path: '/nginx', label: 'Nginx 管理', icon: IconServer },
  { path: '/scheduled-tasks', label: '计划任务', icon: IconCalendarTime },
  { path: '/logs', label: '日志', icon: IconSettings },
  { path: '/security-settings', label: '安全设置', icon: IconLock },
]

const defaultSubtitle = ''
const upgradeCheckInterval = 6 * 60 * 60 * 1000

function formatPublishedAt(value?: string) {
  if (!value) return '-'

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })
}

export function BasicLayout() {
  const [opened, { toggle, close }] = useDisclosure()
  const location = useLocation()
  const auth = useAuth()
  const systemOverviewQuery = useSystemOverview()
  const upgradeQuery = useUpgradeStatus(upgradeCheckInterval)
  const upgradeCheckMutation = useUpgradeCheckMutation()
  const systemOverview = systemOverviewQuery.data
  const upgradeInfo = upgradeQuery.data
  const activeItem = menuItems.find((item) => item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path))
  const agentAvailable = systemOverview?.agent.available
  const agentStatusLabel = agentAvailable === undefined ? '未知' : agentAvailable ? '可用' : '不可用'
  const agentStatusColor = agentAvailable === undefined ? 'gray' : agentAvailable ? 'green' : 'red'

  async function handleLogout() {
    await auth.logout()
    notifySuccess({ message: '已退出登录' })
  }

  function openReleaseURL() {
    if (upgradeInfo?.release_url) {
      window.open(upgradeInfo.release_url, '_blank', 'noopener,noreferrer')
    }
  }

  async function handleRefreshUpgrade() {
    try {
      await upgradeCheckMutation.mutateAsync()
      notifySuccess({ message: '已检查版本更新' })
    } catch (err) {
      notifyError({ message: err instanceof Error ? err.message : '版本检查失败' })
    }
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
              <Title order={4} lh={1.1}>{activeItem?.label || 'NxPanel'}</Title>
              <Text size="xs" c="dimmed">{defaultSubtitle}</Text>
            </div>
          </Group>
          <Group gap="xs" wrap="nowrap">
            {upgradeInfo ? (
              <Popover position="bottom-end" withArrow shadow="md" width={280}>
                <Popover.Target>
                  <Badge
                    component="button"
                    type="button"
                    className="versionBadge"
                    color={upgradeInfo.has_upgrade ? 'orange' : 'gray'}
                    variant={upgradeInfo.has_upgrade ? 'light' : 'outline'}
                  >
                    程序版本: {upgradeInfo.current_version || '版本未知'}
                  </Badge>
                </Popover.Target>
                <Popover.Dropdown>
                  <Stack gap="xs">
                    <Group justify="space-between" align="flex-start">
                      <div>
                        <Text fw={600} size="sm">版本信息</Text>
                        {upgradeInfo.has_upgrade ? (
                          <Text size="xs" c="orange.7">发现新版本</Text>
                        ) : (
                          <Text size="xs" c="dimmed">已是最新版本</Text>
                        )}
                      </div>
                      <Tooltip label="检查更新" position="left">
                        <ActionIcon
                          variant="subtle"
                          color="gray"
                          aria-label="检查更新"
                          loading={upgradeCheckMutation.isPending}
                          onClick={handleRefreshUpgrade}
                        >
                          <IconRefresh size={rem(16)} />
                        </ActionIcon>
                      </Tooltip>
                    </Group>
                    <Group justify="space-between" gap="md">
                      <Text c="dimmed" size="sm">当前版本</Text>
                      <Text size="sm" fw={600}>{upgradeInfo.current_version || '-'}</Text>
                    </Group>
                    <Group justify="space-between" gap="md">
                      <Text c="dimmed" size="sm">最新版本</Text>
                      <Text size="sm" fw={700} c={upgradeInfo.has_upgrade ? 'orange.7' : undefined}>{upgradeInfo.latest_version || '-'}</Text>
                    </Group>
                    <Group justify="space-between" gap="md">
                      <Text c="dimmed" size="sm">发布时间</Text>
                      <Text size="sm">{formatPublishedAt(upgradeInfo.published_at)}</Text>
                    </Group>
                    {upgradeInfo.error ? (
                      <Text size="xs" c="red.7">{upgradeInfo.error}</Text>
                    ) : null}
                    {upgradeInfo.release_url ? (
                      <Anchor
                        component="button"
                        type="button"
                        size="sm"
                        mt={4}
                        onClick={openReleaseURL}
                        style={{ display: 'inline-flex', alignItems: 'center', gap: 4, cursor: 'pointer' }}
                      >
                        查看发布 <IconExternalLink size={rem(14)} />
                      </Anchor>
                    ) : null}
                  </Stack>
                </Popover.Dropdown>
              </Popover>
            ) : null}
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
        <Group mb="lg" gap="sm">
          <div className="brandMark">N</div>
          <div>
            <Text fw={700}>NxPanel</Text>
            <Text size="xs" c="dimmed">Nginx 管理面板</Text>
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
