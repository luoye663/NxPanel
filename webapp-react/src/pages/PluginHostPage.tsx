import { Alert, Breadcrumbs, Loader, Stack, Text } from '@mantine/core'
import { Link, useParams } from 'react-router-dom'
import { usePluginContributions } from '@/api/pluginHooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { PluginSandbox } from '@/components/plugins/PluginSandbox'
import { WAFManagementPage } from '@/pages/WAFManagementPage'

export function PluginHostPage() {
  const { pluginId = '', '*': routePath = '' } = useParams()
  const contributionQuery = usePluginContributions()
  const contributions = contributionQuery.data || []
  const contribution = contributions.find((item) => item.plugin_id === pluginId && item.point === 'global_page' && (!item.route || routePath.startsWith(item.route.replace(/^\//, ''))))

  if (contributionQuery.isLoading) return <Stack align="center" py="xl"><Loader /><Text c="dimmed">正在加载插件入口...</Text></Stack>
  if (contributionQuery.isError) return <ErrorAlert error={contributionQuery.error} title="加载插件入口失败" />
  if (!contribution) return <Alert color="yellow" title="插件页面不可用">该插件未启用、未声明全局页面，或当前路由无权访问。</Alert>
  if (contribution.renderer === 'native_waf') return <WAFManagementPage pluginId={pluginId} />
  return (
    <PageShell>
      <Breadcrumbs><Text component={Link} to="/plugins">插件中心</Text><Text>{contribution.label}</Text></Breadcrumbs>
      <PluginSandbox pluginId={pluginId} entry={contribution.ui_entry || 'main.js'} allowedRPCMethods={contribution.rpc_methods} title={contribution.label} context={{ route: routePath }} />
    </PageShell>
  )
}
