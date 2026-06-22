import { Select, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { getSites } from '@/api/sites'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { SiteBackupTab } from '@/components/site-detail/SiteBackupTab'

export function SiteBackupPage() {
  const navigate = useNavigate()
  const { siteId } = useParams()
  const [selectedSiteId, setSelectedSiteId] = useState(siteId || '')
  const sitesQuery = useQuery({ queryKey: ['sites', 'backup-selector'], queryFn: () => getSites({ page: 1, page_size: 200 }) })

  const siteOptions = useMemo(() => (sitesQuery.data?.items || []).map((site) => ({ value: site.id, label: site.primary_domain || site.id })), [sitesQuery.data])

  useEffect(() => {
    if (!selectedSiteId && siteOptions.length > 0) setSelectedSiteId(siteOptions[0].value)
  }, [siteOptions, selectedSiteId])

  function handleSelect(value: string | null) {
    const next = value || ''
    setSelectedSiteId(next)
    navigate(next ? `/sites/${next}/backups` : '/sites/backups', { replace: true })
  }

  return (
    <PageShell>
      {sitesQuery.isError ? <ErrorAlert error={sitesQuery.error} title="加载网站列表失败" /> : null}
      <SectionCard title="备份管理" description="选择网站后管理手动备份、定时备份、下载、恢复和删除。">
        <Select label="选择网站" placeholder="请选择网站" data={siteOptions} value={selectedSiteId || null} onChange={handleSelect} searchable clearable={false} />
      </SectionCard>
      {selectedSiteId ? <SiteBackupTab site={{ id: selectedSiteId }} /> : <Text c="dimmed">暂无可配置的网站。</Text>}
    </PageShell>
  )
}
