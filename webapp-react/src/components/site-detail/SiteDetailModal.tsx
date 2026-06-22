import { Alert, Button, Group, Loader, Modal, Stack } from '@mantine/core'
import { useDisclosure, useMediaQuery } from '@mantine/hooks'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { enableSite } from '@/api/sites'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SiteAccessLimitTab } from '@/components/site-detail/SiteAccessLimitTab'
import { SiteBasicTab, SiteDocumentTab } from '@/components/site-detail/SiteBasicTab'
import { SiteConfigTab } from '@/components/site-detail/SiteConfigTab'
import { SiteFilesTab } from '@/components/site-detail/SiteFilesTab'
import { SiteLogsTab } from '@/components/site-detail/SiteLogsTab'
import { SiteOperationsTab } from '@/components/site-detail/SiteOperationsTab'
import { fallbackSiteDetailTab, getSiteDetailTabMeta, SiteDetailShell, type SiteDetailTab } from '@/components/site-detail/SiteDetailShell'
import { SiteProxyTab } from '@/components/site-detail/SiteProxyTab'
import { SiteRewriteTab } from '@/components/site-detail/SiteRewriteTab'
import { SiteSSLTab } from '@/components/site-detail/SiteSSLTab'
import { siteDetailKeys, useSiteDetail } from '@/hooks/useSiteDetail'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteDetailModalProps {
  siteId: string | null
  initialTab?: string
  opened: boolean
  onClose: () => void
}

function toSiteDetailTab(value?: string): SiteDetailTab {
  const allowed: SiteDetailTab[] = ['basic', 'document', 'proxy', 'access-limit', 'hotlink', 'ssl', 'rewrite', 'config', 'files', 'logs', 'operations']
  return allowed.includes(value as SiteDetailTab) ? value as SiteDetailTab : 'basic'
}

export function SiteDetailModal({ siteId, initialTab, opened, onClose }: SiteDetailModalProps) {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<SiteDetailTab>(toSiteDetailTab(initialTab))
  const [enableOpened, enableHandlers] = useDisclosure(false)
  const mobile = useMediaQuery('(max-width: 48rem)')
  const detailQuery = useSiteDetail(siteId, opened)
  const site = detailQuery.data
  const activeMeta = getSiteDetailTabMeta(activeTab)
  const enableMutation = useMutation({ mutationFn: () => enableSite(siteId!) })

  useEffect(() => {
    if (opened) setActiveTab(toSiteDetailTab(initialTab))
  }, [opened, initialTab, siteId])

  useEffect(() => {
    if (site) setActiveTab((current) => fallbackSiteDetailTab(site, current))
  }, [site])

  function closeAndReset() {
    setActiveTab('basic')
    onClose()
  }

  async function handleEnable() {
    if (!siteId) return
    enableHandlers.close()
    try {
      await enableMutation.mutateAsync()
      notifySuccess({ message: '网站已启用' })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(siteId) }),
        queryClient.invalidateQueries({ queryKey: ['sites'] }),
      ])
    } catch (error) {
      showErrorModal(error, '启用网站失败')
    }
  }

  function renderActiveTab() {
    if (!site) return null
    if (activeTab === 'basic') return <SiteBasicTab site={site} />
    if (activeTab === 'document') return <SiteDocumentTab site={site} />
    if (activeTab === 'config') return <SiteConfigTab site={site} />
    if (activeTab === 'proxy') return <SiteProxyTab site={site} />
    if (activeTab === 'rewrite') return <SiteRewriteTab site={site} />
    if (activeTab === 'access-limit') return <SiteAccessLimitTab site={site} initialTab="auth" />
    if (activeTab === 'hotlink') return <SiteAccessLimitTab site={site} initialTab="hotlink" singleTab />
    if (activeTab === 'ssl') return <SiteSSLTab site={site} />
    if (activeTab === 'files') return <SiteFilesTab site={site} />
    if (activeTab === 'logs') return <SiteLogsTab site={site} />
    return <SiteOperationsTab site={site} />
  }

  return (
    <Modal
      opened={opened}
      onClose={closeAndReset}
      title={`网站详情-${activeMeta.label}`}
      size="min(1080px, 86vw)"
      fullScreen={mobile}
      closeOnClickOutside={false}
      className="siteDetailModal"
      classNames={{ content: 'siteDetailModalContent', body: 'siteDetailModalBody' }}
    >
      <Stack gap="md" className="siteDetailModalStack">
        {detailQuery.isLoading ? <Group justify="center" py="xl"><Loader /></Group> : null}
        {detailQuery.isError ? <ErrorAlert error={detailQuery.error} title="加载网站详情失败" /> : null}
        {site?.is_imported && (site.import_warnings?.length || 0) > 0 ? (
          <Alert color="yellow" title="导入站点白名单提醒">
            <Stack gap={4}>
              {site.import_warnings?.map((warning) => <div key={warning}>{warning}</div>)}
            </Stack>
          </Alert>
        ) : null}
        {site ? (
          <SiteDetailShell
            site={site}
            activeTab={activeTab}
            onTabChange={(tab) => setActiveTab(fallbackSiteDetailTab(site, tab))}
            actions={site.status !== 'enabled' ? <Button color="green" loading={enableMutation.isPending} onClick={enableHandlers.open}>启用</Button> : null}
          >
            {renderActiveTab()}
          </SiteDetailShell>
        ) : null}
        <Modal opened={enableOpened} onClose={enableHandlers.close} title="启用网站" size="sm" centered>
          <Stack gap="md">
            <div>确认启用网站「{site?.primary_domain}」？</div>
            <Group justify="flex-end"><Button variant="default" onClick={enableHandlers.close}>取消</Button><Button color="green" loading={enableMutation.isPending} onClick={handleEnable}>确认启用</Button></Group>
          </Stack>
        </Modal>
      </Stack>
    </Modal>
  )
}
