import { ActionIcon, Badge, Button, Checkbox, Group, Modal, Select, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArchive, IconEye, IconFolder, IconRefresh, IconSearch, IconSettings, IconTrash, IconUpload, IconWorldCheck, IconWorldOff } from '@tabler/icons-react'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { disableSite, deleteSite, enableSite, getSites, importScan } from '@/api/sites'
import type { SiteListItem } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { StatusBadge } from '@/components/common/StatusBadge'
import { TimeCell } from '@/components/common/TimeCell'
import { SiteDetailModal } from '@/components/site-detail/SiteDetailModal'
import { AdvancedConfigModal } from '@/components/sites/AdvancedConfigModal'
import { SiteCreateModal } from '@/components/sites/SiteCreateModal'
import { SiteImportModal } from '@/components/sites/SiteImportModal'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess, notifyWarning } from '@/utils/notify'

const statusOptions = [
  { value: 'enabled', label: '已启用' },
  { value: 'disabled', label: '已禁用' },
  { value: 'failed', label: '失败' },
  { value: 'drifted', label: '配置漂移' },
]

export function SitesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [filters, setFilters] = useState({ keyword: '', status: '' })
  const [keywordInput, setKeywordInput] = useState('')
  const [createOpened, createHandlers] = useDisclosure(false)
  const [deleteOpened, deleteHandlers] = useDisclosure(false)
  const [importOpened, importHandlers] = useDisclosure(false)
  const [advancedConfigOpened, advancedConfigHandlers] = useDisclosure(false)
  const [detailOpened, detailHandlers] = useDisclosure(false)
  const [selectedSite, setSelectedSite] = useState<SiteListItem | null>(null)
  const [detailInitialTab, setDetailInitialTab] = useState('basic')
  const [importItems, setImportItems] = useState<Awaited<ReturnType<typeof importScan>>['items']>([])
  const [manualRefreshing, setManualRefreshing] = useState(false)
  const [manualSearching, setManualSearching] = useState(false)
  const deleteForm = useForm({
    initialValues: { delete_root: false, delete_logs: false, delete_ssl_files: false },
  })

  const sitesQuery = useQuery({
    queryKey: ['sites', { page: pagination.pageIndex + 1, page_size: pagination.pageSize, keyword: filters.keyword || undefined, status: filters.status || undefined }],
    queryFn: () => getSites({ page: pagination.pageIndex + 1, page_size: pagination.pageSize, keyword: filters.keyword || undefined, status: filters.status || undefined }),
    placeholderData: keepPreviousData,
  })
  const enableMutation = useMutation({ mutationFn: enableSite })
  const disableMutation = useMutation({ mutationFn: disableSite })
  const deleteMutation = useMutation({ mutationFn: ({ siteId, values }: { siteId: string; values: typeof deleteForm.values }) => deleteSite(siteId, { delete_root: values.delete_root, delete_logs: values.delete_logs, delete_ssl_files: values.delete_ssl_files }) })
  const importScanMutation = useMutation({ mutationFn: importScan })

  const columns = useMemo<MRT_ColumnDef<SiteListItem>[]>(() => [
    { accessorKey: 'primary_domain', header: '域名', size: 190, Cell: ({ row }) => <Text fw={600} c="blue.7">{row.original.primary_domain}</Text> },
    { accessorKey: 'status', header: '状态', size: 120, Cell: ({ row }) => <StatusBadge kind="site" value={row.original.status} /> },
    { accessorKey: 'ssl_enabled', header: 'SSL', size: 80, Cell: ({ row }) => row.original.ssl_enabled ? <Badge color="green" variant="light">已启用</Badge> : <Text c="dimmed">-</Text> },
    { accessorKey: 'proxy_enabled', header: '反代', size: 80, Cell: ({ row }) => row.original.proxy_enabled ? <Badge color="green" variant="light">已启用</Badge> : <Text c="dimmed">-</Text> },
    { accessorKey: 'root_path', header: '根目录', size: 260, Cell: ({ row }) => <PathCell value={row.original.root_path} maxWidth={260} onClick={() => openDetail(row.original, 'files')} /> },
    { accessorKey: 'updated_at', header: '更新时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.updated_at} /> },
  ], [])

  function refreshSites() {
    return queryClient.invalidateQueries({ queryKey: ['sites'] })
  }

  function openDetail(site: SiteListItem, tab = 'basic') {
    setSelectedSite(site)
    setDetailInitialTab(tab)
    detailHandlers.open()
  }

  async function handleSearch() {
    const keyword = keywordInput.trim()
    setManualSearching(true)
    try {
      if (keyword === filters.keyword && pagination.pageIndex === 0) {
        await Promise.all([sitesQuery.refetch(), new Promise((resolve) => window.setTimeout(resolve, 300))])
        return
      }
      setPagination((current) => ({ ...current, pageIndex: 0 }))
      setFilters((current) => ({ ...current, keyword }))
      await new Promise((resolve) => window.setTimeout(resolve, 300))
    } finally {
      setManualSearching(false)
    }
  }

  async function handleRefresh() {
    setManualRefreshing(true)
    try {
      await Promise.all([sitesQuery.refetch(), new Promise((resolve) => window.setTimeout(resolve, 300))])
    } finally {
      setManualRefreshing(false)
    }
  }

  function handleToggle(site: SiteListItem) {
    if (site.status === 'enabled') {
      confirmDanger({
        title: '禁用网站',
        message: `禁用网站「${site.primary_domain}」后，该站点配置将不再被 Nginx 加载。确认禁用？`,
        confirmLabel: '确认禁用',
        errorTitle: '禁用网站失败',
        onConfirm: async () => {
          await disableMutation.mutateAsync(site.id)
          notifySuccess({ message: '网站已禁用' })
          await refreshSites()
        },
      })
      return
    }

    confirmDanger({
      title: '启用网站',
      message: `确认启用网站「${site.primary_domain}」？`,
      confirmLabel: '确认启用',
      errorTitle: '启用网站失败',
      onConfirm: async () => {
        await enableMutation.mutateAsync(site.id)
        notifySuccess({ message: '网站已启用' })
        await refreshSites()
      },
    })
  }

  function openDelete(site: SiteListItem) {
    setSelectedSite(site)
    deleteForm.setValues({ delete_root: false, delete_logs: false, delete_ssl_files: false })
    deleteForm.clearErrors()
    deleteHandlers.open()
  }

  async function handleDelete(values: typeof deleteForm.values) {
    if (!selectedSite) return
    deleteHandlers.close()
    try {
      await deleteMutation.mutateAsync({ siteId: selectedSite.id, values })
      notifySuccess({ message: '网站已删除' })
      await refreshSites()
    } catch (error) {
      showErrorModal(error, '删除网站失败')
    }
  }

  async function handleImportScan() {
    importHandlers.open()
    setImportItems([])
    try {
      const result = await importScanMutation.mutateAsync()
      setImportItems(result.items || [])
      if (!result.items?.length) notifyWarning({ message: '未发现可导入的旧站点' })
    } catch (error) {
      showErrorModal(error, '扫描旧站点失败')
    }
  }

  async function handleCreated(siteId: string) {
    createHandlers.close()
    await refreshSites()
    setSelectedSite({ id: siteId, primary_domain: '', domains: [], bindings: [], status: '', root_path: '', ssl_enabled: false, proxy_enabled: false, access_log_path: '', error_log_path: '', updated_at: '' })
    setDetailInitialTab('basic')
    detailHandlers.open()
  }

  return (
    <PageShell>
      {sitesQuery.isError ? <ErrorAlert error={sitesQuery.error} title="加载网站列表失败" /> : null}
      <SectionCard
        title="网站列表"
        description="点击整行查看基础详情，根目录列可进入文件入口。"
        actions={(
          <Group justify="flex-end" gap="xs" wrap="nowrap" className="sitesToolbarActions">
            <Button variant="outline" leftSection={<IconArchive size={16} />} onClick={() => navigate('/sites/backups')}>备份管理</Button>
            <Button variant="outline" color="gray" leftSection={<IconSettings size={16} />} onClick={advancedConfigHandlers.open}>高级配置</Button>
            <Button variant="outline" leftSection={<IconUpload size={16} />} loading={importScanMutation.isPending} onClick={handleImportScan}>导入旧站点</Button>
          </Group>
        )}
      >
        <DataTable
          columns={columns}
          data={sitesQuery.data?.items ?? []}
          rowCount={sitesQuery.data?.total ?? 0}
          loading={sitesQuery.isLoading || sitesQuery.isFetching || manualRefreshing || manualSearching}
          initialLoading={sitesQuery.isLoading && !manualSearching}
          pagination={pagination}
          onPaginationChange={setPagination}
          emptyText="暂无网站"
          toolbarActions={(
            <Group justify="space-between" w="100%" gap="sm" className="sitesToolbarActions">
              <Group gap="xs" className="sitesToolbarActions">
                <TextInput w={190} placeholder="搜索域名" leftSection={<IconSearch size={16} />} value={keywordInput} onChange={(event) => setKeywordInput(event.currentTarget.value)} onKeyDown={(event) => { if (event.key === 'Enter') void handleSearch() }} />
                <Select w={130} placeholder="全部状态" clearable data={statusOptions} value={filters.status || null} onChange={(value) => { setPagination((current) => ({ ...current, pageIndex: 0 })); setFilters((current) => ({ ...current, status: value || '' })) }} />
                <Button variant="light" leftSection={<IconSearch size={16} />} loading={manualSearching} onClick={handleSearch}>搜索</Button>
                <Button variant="light" color="gray" leftSection={<IconRefresh size={16} />} loading={sitesQuery.isFetching || manualRefreshing} onClick={handleRefresh}>刷新</Button>
              </Group>
              <Button onClick={createHandlers.open}>新建网站</Button>
            </Group>
          )}
          plain
          mantineTableBodyRowProps={({ row }) => ({ onClick: () => openDetail(row.original), style: { cursor: 'pointer' } })}
          renderRowActions={({ row }) => (
            <Group gap={4} wrap="nowrap" onClick={(event) => event.stopPropagation()}>
              <Tooltip label={row.original.status === 'enabled' ? '禁用网站' : '启用网站'}>
                <ActionIcon variant="subtle" color={row.original.status === 'enabled' ? 'yellow' : 'green'} aria-label={row.original.status === 'enabled' ? '禁用网站' : '启用网站'} loading={enableMutation.isPending || disableMutation.isPending} onClick={() => handleToggle(row.original)}>
                  {row.original.status === 'enabled' ? <IconWorldOff size={16} /> : <IconWorldCheck size={16} />}
                </ActionIcon>
              </Tooltip>
              <Tooltip label="基础详情"><ActionIcon variant="subtle" aria-label="查看详情" onClick={() => openDetail(row.original)}><IconEye size={16} /></ActionIcon></Tooltip>
              <Tooltip label="文件入口"><ActionIcon variant="subtle" aria-label="打开文件入口" onClick={() => openDetail(row.original, 'files')}><IconFolder size={16} /></ActionIcon></Tooltip>
              <Tooltip label="删除网站"><ActionIcon variant="subtle" color="red" aria-label="删除网站" onClick={() => openDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
            </Group>
          )}
        />
      </SectionCard>

      <SiteCreateModal opened={createOpened} onClose={createHandlers.close} onCreated={handleCreated} />
      <AdvancedConfigModal opened={advancedConfigOpened} onClose={advancedConfigHandlers.close} />
      <SiteDetailModal opened={detailOpened} siteId={selectedSite?.id ?? null} initialTab={detailInitialTab} onClose={detailHandlers.close} />
      <SiteImportModal opened={importOpened} scanning={importScanMutation.isPending} items={importItems} onClose={importHandlers.close} />
      <Modal opened={deleteOpened} onClose={deleteHandlers.close} title="删除网站" size="md" closeOnClickOutside={false}>
        <form onSubmit={deleteForm.onSubmit(handleDelete)}>
          <Stack gap="md">
            <Text size="sm">确认删除网站「<Text span fw={700}>{selectedSite?.primary_domain}</Text>」？此操作不可恢复。</Text>
            <Stack gap="xs">
              <Checkbox label="同时删除网站根目录" {...deleteForm.getInputProps('delete_root', { type: 'checkbox' })} />
              <Checkbox label="同时删除日志文件" {...deleteForm.getInputProps('delete_logs', { type: 'checkbox' })} />
              <Checkbox label="同时删除 SSL 证书文件" {...deleteForm.getInputProps('delete_ssl_files', { type: 'checkbox' })} />
            </Stack>
            <Group justify="flex-end"><Button variant="default" onClick={deleteHandlers.close}>取消</Button><Button type="submit" color="red" loading={deleteMutation.isPending}>确认删除</Button></Group>
          </Stack>
        </form>
      </Modal>
    </PageShell>
  )
}
