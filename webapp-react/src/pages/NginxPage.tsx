import { Select, Tabs } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { getSystemOverview } from '@/api/system'
import { NginxActions } from '@/components/nginx/NginxActions'
import { NginxConfEditorTab } from '@/components/nginx/NginxConfEditorTab'
import { NginxOverviewCard } from '@/components/nginx/NginxOverviewCard'
import { NginxParametersTab } from '@/components/nginx/NginxParametersTab'
import { NginxUpstreamsTab } from '@/components/nginx/NginxUpstreamsTab'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'

export function NginxPage() {
  const mobile = useMediaQuery('(max-width: 48rem)')
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedTab = searchParams.get('tab')
  const activeTab = ['status', 'upstreams', 'parameters', 'conf-editor'].includes(requestedTab || '') ? requestedTab! : 'status'
  const overviewQuery = useQuery({ queryKey: ['system', 'overview'], queryFn: getSystemOverview })

  function setActiveTab(value: string | null) {
    const next = new URLSearchParams(searchParams)
    if (!value || value === 'status') next.delete('tab')
    else next.set('tab', value)
    setSearchParams(next, { replace: true })
  }

  return (
    <PageShell>
      {overviewQuery.isError ? <ErrorAlert error={overviewQuery.error} title="加载 Nginx 状态失败" /> : null}
      {mobile ? <Select aria-label="Nginx 管理页面" className="nginxMobileTabSelect" value={activeTab} onChange={setActiveTab} allowDeselect={false} data={[{ value: 'status', label: '状态管理' }, { value: 'upstreams', label: '上游管理' }, { value: 'parameters', label: '常用参数' }, { value: 'conf-editor', label: 'nginx.conf 编辑' }]} /> : null}
      <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}>
        <Tabs.List className={mobile ? 'nginxDesktopTabsHidden' : undefined}>
          <Tabs.Tab value="status">状态管理</Tabs.Tab>
          <Tabs.Tab value="upstreams">上游管理</Tabs.Tab>
          <Tabs.Tab value="parameters">常用参数</Tabs.Tab>
          <Tabs.Tab value="conf-editor">nginx.conf 编辑</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="status">
          <PageShell p={0}>
            <NginxOverviewCard overview={overviewQuery.data} loading={overviewQuery.isLoading} />
            <NginxActions />
          </PageShell>
        </Tabs.Panel>
        <Tabs.Panel value="upstreams"><NginxUpstreamsTab /></Tabs.Panel>
        <Tabs.Panel value="parameters"><NginxParametersTab active={activeTab === 'parameters'} /></Tabs.Panel>
        <Tabs.Panel value="conf-editor"><NginxConfEditorTab active={activeTab === 'conf-editor'} /></Tabs.Panel>
      </Tabs>
    </PageShell>
  )
}
