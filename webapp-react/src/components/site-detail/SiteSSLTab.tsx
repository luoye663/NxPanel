import { Alert, Badge, Group, Loader, Stack, Tabs, Text } from '@mantine/core'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getSSL } from '@/api/ssl'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { SiteACMETab } from '@/components/site-detail/SiteACMETab'
import { SiteCertificateStoreTab } from '@/components/site-detail/SiteCertificateStoreTab'
import { SiteExistingCertificatePanel } from '@/components/site-detail/SiteExistingCertificatePanel'
import { SiteManualCertificatePanel } from '@/components/site-detail/SiteManualCertificatePanel'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { useState } from 'react'

interface SiteSSLTabProps {
  site: SiteDetail
}

function isSSLMarkerMissing(site: SiteDetail): boolean {
  return (site.marker_status?.missing || []).includes('SSL')
}

export function SiteSSLTab({ site }: SiteSSLTabProps) {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<string | null>('current')
  const sslQueryKey = ['site-detail', site.id, 'ssl'] as const
  const sslQuery = useQuery({ queryKey: sslQueryKey, queryFn: () => getSSL(site.id) })
  const ssl = sslQuery.data

  async function refreshSSL() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: sslQueryKey }),
      queryClient.invalidateQueries({ queryKey: ['site-detail', site.id, 'ssl-content'] }),
      queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
      queryClient.invalidateQueries({ queryKey: ['sites'] }),
    ])
  }

  return (
    <SectionCard>
      <Stack gap="md">
        {isSSLMarkerMissing(site) ? (
          <Alert color="red" title="SSL 标识块缺失">
            该站点配置文件中缺少 NxPanel SSL 标识块，SSL 表单功能将无法安全修改对应片段。请在「站点配置」中检查并修复。
          </Alert>
        ) : null}
        {sslQuery.isError ? <ErrorAlert error={sslQuery.error} title="加载 SSL 状态失败" /> : null}
        {sslQuery.isLoading ? <Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载 SSL 状态...</Text></Group> : null}
        {ssl ? (
          <Tabs value={activeTab} onChange={setActiveTab}>
            <Tabs.List>
              <Tabs.Tab value="current">当前证书 <Badge ml={6} color={ssl.enabled ? 'green' : 'gray'} variant="light">{ssl.enabled ? '已启用' : '未启用'}</Badge></Tabs.Tab>
              <Tabs.Tab value="existing">使用已有证书</Tabs.Tab>
              <Tabs.Tab value="store">证书夹</Tabs.Tab>
              <Tabs.Tab value="acme">Let's Encrypt</Tabs.Tab>
            </Tabs.List>
            <Tabs.Panel value="current"><SiteManualCertificatePanel site={site} ssl={ssl} active={activeTab === 'current'} onChanged={refreshSSL} /></Tabs.Panel>
            <Tabs.Panel value="existing"><SiteExistingCertificatePanel site={site} onChanged={refreshSSL} /></Tabs.Panel>
            <Tabs.Panel value="store"><SiteCertificateStoreTab site={site} onChanged={refreshSSL} /></Tabs.Panel>
            <Tabs.Panel value="acme"><SiteACMETab site={site} onChanged={refreshSSL} /></Tabs.Panel>
          </Tabs>
        ) : null}
      </Stack>
    </SectionCard>
  )
}
