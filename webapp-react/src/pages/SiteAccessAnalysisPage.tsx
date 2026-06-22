import { Box, Button, Group, Loader, Modal, Select, Tabs, Text, TextInput } from '@mantine/core'
import { DateInput, DateTimePicker } from '@mantine/dates'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import type { MRT_PaginationState } from 'mantine-react-table'
import {
  detectAccessLogFormat,
  getAccessAnalysisAnomalies,
  getAccessAnalysisEntries,
  getAccessAnalysisIPs,
  getAccessAnalysisPaths,
  getAccessAnalysisSettings,
  getAccessAnalysisSummary,
  optimizeAccessLogFormat,
  saveAccessAnalysisSettings,
  scanAccessAnalysis,
  testAccessLogFormat,
} from '@/api/accessAnalysis'
import { getSites } from '@/api/sites'
import type { AccessAnalysisScanResponse, AccessAnalysisSettings } from '@/api/types'
import { AccessAnalysisExportMenu } from '@/components/access-analysis/AccessAnalysisExportMenu'
import { AccessAnalysisFormatPanel } from '@/components/access-analysis/AccessAnalysisFormatPanel'
import { AccessAnalysisOverview } from '@/components/access-analysis/AccessAnalysisOverview'
import { AccessAnalysisScanPanel } from '@/components/access-analysis/AccessAnalysisScanPanel'
import { AccessAnalysisSettingsPanel } from '@/components/access-analysis/AccessAnalysisSettingsPanel'
import { AccessAnalysisTrendChart } from '@/components/access-analysis/AccessAnalysisTrendChart'
import { AccessAnomalyPanel } from '@/components/access-analysis/AccessAnomalyPanel'
import { AccessEntryTable } from '@/components/access-analysis/AccessEntryTable'
import { AccessIPTable } from '@/components/access-analysis/AccessIPTable'
import { AccessPathTable } from '@/components/access-analysis/AccessPathTable'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

function startOfDay(date: Date) {
  const next = new Date(date)
  next.setHours(0, 0, 0, 0)
  return next
}

function endOfDay(date: Date) {
  const next = new Date(date)
  next.setHours(23, 59, 59, 999)
  return next
}

function startOfToday() {
  return startOfDay(new Date())
}

function endOfToday() {
  return endOfDay(new Date())
}

export function SiteAccessAnalysisPage() {
  const { siteId = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<string | null>('paths')
  const [controlTab, setControlTab] = useState<string | null>('scan')
  const [controlOpened, setControlOpened] = useState(false)
  const [from, setFrom] = useState<Date | null>(() => startOfToday())
  const [to, setTo] = useState<Date | null>(() => endOfToday())
  const [pathFilter, setPathFilter] = useState('')
  const [ipFilter, setIPFilter] = useState('')
  const [lastScan, setLastScan] = useState<AccessAnalysisScanResponse | undefined>()
  const [scanRange, setScanRange] = useState('today')
  const [scanFrom, setScanFrom] = useState<Date | null>(null)
  const [scanTo, setScanTo] = useState<Date | null>(null)
  const [pathsPagination, setPathsPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [ipsPagination, setIPsPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [entriesPagination, setEntriesPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const sitesQuery = useQuery({ queryKey: ['sites', 'access-analysis-selector'], queryFn: () => getSites({ page: 1, page_size: 500 }) })
  const siteOptions = useMemo(() => (sitesQuery.data?.items || []).map((site) => ({ value: site.id, label: site.primary_domain || site.id })), [sitesQuery.data])
  const selectedSiteId = siteId || siteOptions[0]?.value || ''
  const rangeQuery = useMemo(() => ({ from: from?.toISOString(), to: to?.toISOString() }), [from, to])
  const queryBase = { ...rangeQuery, path: pathFilter || undefined, ip: ipFilter || undefined }

  const summaryQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'summary', rangeQuery], queryFn: () => getAccessAnalysisSummary(selectedSiteId, rangeQuery), enabled: Boolean(selectedSiteId) })
  const settingsQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'settings'], queryFn: () => getAccessAnalysisSettings(selectedSiteId), enabled: Boolean(selectedSiteId) })
  const pathsQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'paths', queryBase, pathsPagination], queryFn: () => getAccessAnalysisPaths(selectedSiteId, { ...queryBase, page: pathsPagination.pageIndex + 1, page_size: pathsPagination.pageSize }), enabled: Boolean(selectedSiteId) })
  const ipsQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'ips', queryBase, ipsPagination], queryFn: () => getAccessAnalysisIPs(selectedSiteId, { ...queryBase, page: ipsPagination.pageIndex + 1, page_size: ipsPagination.pageSize }), enabled: Boolean(selectedSiteId) })
  const entriesQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'entries', queryBase, entriesPagination], queryFn: () => getAccessAnalysisEntries(selectedSiteId, { ...queryBase, page: entriesPagination.pageIndex + 1, page_size: entriesPagination.pageSize }), enabled: Boolean(selectedSiteId) && activeTab === 'entries' })
  const anomaliesQuery = useQuery({ queryKey: ['access-analysis', selectedSiteId, 'anomalies', rangeQuery], queryFn: () => getAccessAnalysisAnomalies(selectedSiteId, rangeQuery), enabled: Boolean(selectedSiteId) })
  const scanMutation = useMutation({ mutationFn: ({ range, from, to }: { range: string; from?: string; to?: string }) => scanAccessAnalysis(selectedSiteId, { range, from, to }) })
  const saveSettingsMutation = useMutation({ mutationFn: (settings: AccessAnalysisSettings) => saveAccessAnalysisSettings(selectedSiteId, settings) })
  const detectMutation = useMutation({ mutationFn: (sample?: string) => detectAccessLogFormat(selectedSiteId, sample) })
  const testMutation = useMutation({ mutationFn: ({ pattern, sample }: { pattern: string; sample: string }) => testAccessLogFormat(selectedSiteId, pattern, sample) })
  const optimizeMutation = useMutation({ mutationFn: () => optimizeAccessLogFormat(selectedSiteId) })

  useEffect(() => {
    setLastScan(undefined)
  }, [selectedSiteId])

  async function refreshAll() {
    if (!selectedSiteId) return
    await queryClient.invalidateQueries({ queryKey: ['access-analysis', selectedSiteId] })
  }

  async function handleScan(range: string, scanFrom?: string, scanTo?: string) {
    try {
      const result = await scanMutation.mutateAsync({ range, from: scanFrom, to: scanTo })
      setLastScan(result)
      notifySuccess({ message: '访问日志扫描完成' })
      await refreshAll()
    } catch (error) { showErrorModal(error, '访问日志扫描失败') }
  }

  async function handleSave(settings: AccessAnalysisSettings) {
    try { await saveSettingsMutation.mutateAsync(settings); notifySuccess({ message: '访问分析设置已保存' }); await refreshAll() } catch (error) { showErrorModal(error, '保存访问分析设置失败') }
  }

  async function handleOptimize() {
    try { await optimizeMutation.mutateAsync(); notifySuccess({ message: '已切换为 nxpanel_json 分析格式' }); await refreshAll() } catch (error) { showErrorModal(error, '优化日志格式失败') }
  }

  return (
    <PageShell>
      <Group justify="space-between" align="end">
        <Group align="end">
          <Select label="网站列表" placeholder="选择要分析的网站" data={siteOptions} value={selectedSiteId || null} searchable allowDeselect={false} w={{ base: '100%', sm: 360 }} disabled={sitesQuery.isLoading} onChange={(value) => value && navigate(`/sites/${value}/analysis`)} />
          <Select label="扫描范围" value={scanRange} allowDeselect={false} w={140} data={[{ value: 'today', label: '今天' }, { value: 'yesterday', label: '昨天' }, { value: '7d', label: '最近 7 天' }, { value: 'custom', label: '自定义' }]} onChange={(value) => setScanRange(value || 'today')} />
          {scanRange === 'custom' ? <DateInput label="开始日期" value={scanFrom} onChange={setScanFrom} /> : null}
          {scanRange === 'custom' ? <DateInput label="结束日期" value={scanTo} onChange={setScanTo} /> : null}
          <Button loading={scanMutation.isPending} disabled={!selectedSiteId} onClick={() => handleScan(scanRange, scanFrom?.toISOString(), scanTo?.toISOString())}>开始扫描</Button>
          <Button variant="outline" disabled={!selectedSiteId} onClick={() => setControlOpened(true)}>分析控制</Button>
        </Group>
        <Group gap="xs">
          <Button variant="light" disabled={!selectedSiteId} onClick={refreshAll}>刷新</Button>
        </Group>
      </Group>
      {lastScan ? <Text size="sm" c="dimmed">最近扫描: {lastScan.scanned_lines} 行，跳过 {lastScan.skipped_lines} 行，耗时 {lastScan.duration_ms}ms{lastScan.truncated ? '，已触发限制截断' : ''}</Text> : null}
      {sitesQuery.isError ? <ErrorAlert error={sitesQuery.error} title="加载网站列表失败" /> : null}
      {summaryQuery.isError ? <ErrorAlert error={summaryQuery.error} title="加载访问分析概览失败" /> : null}
      {summaryQuery.isLoading ? <Group justify="center"><Loader /></Group> : null}
      <AccessAnalysisOverview summary={summaryQuery.data?.summary} />
      <SectionCard title="筛选条件" description="排行、趋势、明细和导出共用当前时间筛选；日期默认覆盖今天 00:00:00 至 23:59:59。" p="md">
        <Group align="end"><DateTimePicker label="开始日期" value={from} onChange={setFrom} valueFormat="YYYY-MM-DD HH:mm:ss" clearable /><DateTimePicker label="结束日期" value={to} onChange={setTo} valueFormat="YYYY-MM-DD HH:mm:ss" clearable /><TextInput label="路径包含" value={pathFilter} onChange={(event) => setPathFilter(event.currentTarget.value)} /><TextInput label="IP" value={ipFilter} onChange={(event) => setIPFilter(event.currentTarget.value)} /><AccessAnalysisExportMenu siteId={selectedSiteId} query={queryBase} /></Group>
      </SectionCard>
      <SectionCard title="小时趋势" description="按小时展示访问量、独立 IP 和错误量。"><AccessAnalysisTrendChart points={summaryQuery.data?.trend || []} /></SectionCard>
      <Tabs value={activeTab} onChange={setActiveTab}>
        <Tabs.List><Tabs.Tab value="paths">路径排行</Tabs.Tab><Tabs.Tab value="ips">IP 排行</Tabs.Tab><Tabs.Tab value="entries">访问明细</Tabs.Tab><Tabs.Tab value="anomalies">异常洞察</Tabs.Tab></Tabs.List>
        <Tabs.Panel value="paths"><AccessPathTable data={pathsQuery.data?.items || []} total={pathsQuery.data?.total || 0} loading={pathsQuery.isLoading} pagination={pathsPagination} onPaginationChange={setPathsPagination} /></Tabs.Panel>
        <Tabs.Panel value="ips"><AccessIPTable data={ipsQuery.data?.items || []} total={ipsQuery.data?.total || 0} loading={ipsQuery.isLoading} pagination={ipsPagination} onPaginationChange={setIPsPagination} /></Tabs.Panel>
        <Tabs.Panel value="entries"><AccessEntryTable data={entriesQuery.data?.items || []} total={entriesQuery.data?.total || 0} loading={entriesQuery.isLoading} pagination={entriesPagination} onPaginationChange={setEntriesPagination} /></Tabs.Panel>
        <Tabs.Panel value="anomalies"><AccessAnomalyPanel anomalies={anomaliesQuery.data || []} /></Tabs.Panel>
      </Tabs>
      <Modal opened={controlOpened} onClose={() => setControlOpened(false)} title="分析控制" size="lg" centered>
        <Tabs value={controlTab} onChange={setControlTab}>
          <Tabs.List><Tabs.Tab value="scan">扫描控制</Tabs.Tab><Tabs.Tab value="settings">定时与保留</Tabs.Tab><Tabs.Tab value="format">日志格式检测</Tabs.Tab></Tabs.List>
          <Box mih={360}>
            <Tabs.Panel value="scan"><AccessAnalysisScanPanel lastResult={lastScan} embedded /></Tabs.Panel>
            <Tabs.Panel value="settings"><AccessAnalysisSettingsPanel settings={settingsQuery.data} saving={saveSettingsMutation.isPending} onSave={handleSave} embedded /></Tabs.Panel>
            <Tabs.Panel value="format"><AccessAnalysisFormatPanel detecting={detectMutation.isPending} testing={testMutation.isPending} optimizing={optimizeMutation.isPending} result={detectMutation.data || testMutation.data} onDetect={(sample) => detectMutation.mutate(sample)} onTest={(pattern, sample) => testMutation.mutate({ pattern, sample })} onOptimize={handleOptimize} /></Tabs.Panel>
          </Box>
        </Tabs>
      </Modal>
    </PageShell>
  )
}
