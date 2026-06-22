import { Alert, Center, Loader, Stack, Text } from '@mantine/core'
import { Suspense, lazy, type ReactNode } from 'react'
import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom'
import { ProtectedRoute, useAuth } from '@/auth/AuthProvider'
import { gatePath, hasGatePath } from '@/api/gate'
import { LoginLayout } from '@/components/layout/LoginLayout'
import { BasicLayout } from '@/components/layout/BasicLayout'

const LoginPage = lazy(() => import('@/pages/LoginPage').then((module) => ({ default: module.LoginPage })))
const SetupPage = lazy(() => import('@/pages/SetupPage').then((module) => ({ default: module.SetupPage })))
const TwoFASetupPage = lazy(() => import('@/pages/TwoFASetupPage').then((module) => ({ default: module.TwoFASetupPage })))
const DashboardPage = lazy(() => import('@/pages/DashboardPage').then((module) => ({ default: module.DashboardPage })))
const LogsPage = lazy(() => import('@/pages/LogsPage').then((module) => ({ default: module.LogsPage })))
const SitesPage = lazy(() => import('@/pages/SitesPage').then((module) => ({ default: module.SitesPage })))
const NginxPage = lazy(() => import('@/pages/NginxPage').then((module) => ({ default: module.NginxPage })))
const SecuritySettingsPage = lazy(() => import('@/pages/SecuritySettingsPage').then((module) => ({ default: module.SecuritySettingsPage })))
const FileManagerPage = lazy(() => import('@/pages/FileManagerPage').then((module) => ({ default: module.FileManagerPage })))
const SiteAccessAnalysisPage = lazy(() => import('@/pages/SiteAccessAnalysisPage').then((module) => ({ default: module.SiteAccessAnalysisPage })))
const SiteBackupPage = lazy(() => import('@/pages/SiteBackupPage').then((module) => ({ default: module.SiteBackupPage })))
const ScheduledTasksPage = lazy(() => import('@/pages/ScheduledTasksPage').then((module) => ({ default: module.ScheduledTasksPage })))

export const router = createBrowserRouter([
  ...(hasGatePath ? [{
    element: <LoginLayout />,
    children: [
      {
        path: gatePath,
        element: <GateEntry setup={withPageSuspense(<SetupPage />)} login={withPageSuspense(<LoginPage />)} />,
      },
    ],
  }] : []),
  {
    element: <ProtectedRoute><BasicLayout /></ProtectedRoute>,
    children: [
      {
        path: '/',
        element: withPageSuspense(<DashboardPage />),
      },
      {
        path: '/sites',
        element: withPageSuspense(<SitesPage />),
      },
      {
        path: '/sites/analysis',
        element: withPageSuspense(<SiteAccessAnalysisPage />),
      },
      {
        path: '/sites/backups',
        element: withPageSuspense(<SiteBackupPage />),
      },
      {
        path: '/sites/:siteId/analysis',
        element: withPageSuspense(<SiteAccessAnalysisPage />),
      },
      {
        path: '/sites/:siteId/backups',
        element: withPageSuspense(<SiteBackupPage />),
      },
      {
        path: '/files',
        element: withPageSuspense(<FileManagerPage />),
      },
      {
        path: '/nginx',
        element: withPageSuspense(<NginxPage />),
      },
      {
        path: '/scheduled-tasks',
        element: withPageSuspense(<ScheduledTasksPage />),
      },
      {
        path: '/logs',
        element: withPageSuspense(<LogsPage />),
      },
      {
        path: '/security-settings',
        element: withPageSuspense(<SecuritySettingsPage />),
      },
      {
        path: '/twofa-setup',
        element: withPageSuspense(<TwoFASetupPage />),
      },
      {
        path: '/operations',
        element: <Navigate to="/logs" replace />,
      },
    ],
  },
  {
    path: '*',
    element: hasGatePath ? <Navigate to="/" replace /> : <MissingGatePath />,
  },
])

export function RouteOutlet() {
  return <Outlet />
}

function withPageSuspense(children: ReactNode) {
  return <Suspense fallback={<PageLoader />}>{children}</Suspense>
}

function GateEntry({ setup, login }: { setup: ReactNode; login: ReactNode }) {
  const auth = useAuth()
  if (!auth.initialized) return <PageLoader />
  if (auth.authenticated) return <Navigate to="/" replace />
  // 同一个隐藏入口根据初始化状态显示 Setup 或 Login，避免暴露固定 /setup 与 /login。
  if (auth.needsSetup) return setup
  return login
}

function PageLoader() {
  return (
    <Center mih={320}>
      <Stack align="center" gap="sm">
        <Loader size="sm" />
        <Text size="sm" c="dimmed">页面加载中...</Text>
      </Stack>
    </Center>
  )
}

function MissingGatePath() {
  return (
    <Center h="100vh" p="md">
      <Alert color="red" title="缺少隐藏登录路径">
        生产环境请从后端入口访问页面；Vite 开发环境请设置 VITE_NX_GATE_PATH 为后端日志中的 login_path 后重启前端开发服务器。
      </Alert>
    </Center>
  )
}
