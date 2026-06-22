import { Tabs } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { getSystemOverview } from '@/api/system'
import { NginxActions } from '@/components/nginx/NginxActions'
import { NginxConfEditorTab } from '@/components/nginx/NginxConfEditorTab'
import { NginxOverviewCard } from '@/components/nginx/NginxOverviewCard'
import { NginxParametersTab } from '@/components/nginx/NginxParametersTab'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'

export function NginxPage() {
  const [activeTab, setActiveTab] = useState<string | null>('status')
  const overviewQuery = useQuery({ queryKey: ['system', 'overview'], queryFn: getSystemOverview })

  return (
    <PageShell>
      {overviewQuery.isError ? <ErrorAlert error={overviewQuery.error} title="加载 Nginx 状态失败" /> : null}
      <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}>
        <Tabs.List>
          <Tabs.Tab value="status">状态管理</Tabs.Tab>
          <Tabs.Tab value="parameters">常用参数</Tabs.Tab>
          <Tabs.Tab value="conf-editor">nginx.conf 编辑</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="status">
          <PageShell p={0}>
            <NginxOverviewCard overview={overviewQuery.data} loading={overviewQuery.isLoading} />
            <NginxActions />
          </PageShell>
        </Tabs.Panel>
        <Tabs.Panel value="parameters"><NginxParametersTab active={activeTab === 'parameters'} /></Tabs.Panel>
        <Tabs.Panel value="conf-editor"><NginxConfEditorTab active={activeTab === 'conf-editor'} /></Tabs.Panel>
      </Tabs>
    </PageShell>
  )
}
