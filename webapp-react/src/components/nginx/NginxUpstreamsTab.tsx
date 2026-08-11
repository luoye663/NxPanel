import { ActionIcon, Badge, Button, Group, Select, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEdit, IconPlus, IconRefresh, IconTrash, IconUpload } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useState } from 'react'
import { ApiError } from '@/api/client'
import type { NginxUpstream, NginxUpstreamSaveRequest } from '@/api/types'
import { createUpstream, deleteUpstream, getUpstreamStatus, listUpstreams, syncUpstreams, updateUpstream, upstreamKeys } from '@/api/upstreams'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'
import { UpstreamEditorModal } from './UpstreamEditorModal'

const algorithmLabels: Record<NginxUpstream['algorithm'], string> = {
  round_robin: '轮询',
  least_conn: '最少连接',
  ip_hash: 'IP Hash',
  hash: 'Hash',
}

export function NginxUpstreamsTab() {
  const queryClient = useQueryClient()
  const [opened, modal] = useDisclosure(false)
  const [editing, setEditing] = useState<NginxUpstream | null>(null)
  const [search, setSearch] = useState('')
  const [algorithm, setAlgorithm] = useState<string | null>(null)
  const listQuery = useQuery({ queryKey: upstreamKeys.all, queryFn: listUpstreams })
  const statusQuery = useQuery({ queryKey: upstreamKeys.status, queryFn: getUpstreamStatus })
  const saveMutation = useMutation({
    mutationFn: (values: NginxUpstreamSaveRequest) => editing ? updateUpstream(editing.id, values) : createUpstream(values),
  })
  const deleteMutation = useMutation({ mutationFn: deleteUpstream })
  const syncMutation = useMutation({ mutationFn: syncUpstreams })

  const rows = (listQuery.data || []).filter((item) => {
    const keyword = search.trim().toLowerCase()
    const matchesSearch = !keyword || item.name.toLowerCase().includes(keyword) || item.servers.some((server) => server.address.toLowerCase().includes(keyword))
    return matchesSearch && (!algorithm || item.algorithm === algorithm)
  })

  const columns: MRT_ColumnDef<NginxUpstream>[] = [
    { accessorKey: 'name', header: '配置名', size: 150, Cell: ({ cell }) => <MonoText value={cell.getValue<string>()} maxWidth={180} /> },
    { accessorKey: 'algorithm', header: '策略', size: 110, Cell: ({ row }) => algorithmLabels[row.original.algorithm] },
    { id: 'servers', header: '成员摘要', size: 260, Cell: ({ row }) => <Text size="sm" lineClamp={2}>{row.original.servers.map((server) => server.address).join('、')}</Text> },
    { accessorKey: 'keepalive', header: 'Keepalive', size: 100 },
    { accessorKey: 'reference_count', header: '引用数', size: 80 },
    { accessorKey: 'updated_at', header: '更新时间', size: 170, Cell: ({ cell }) => new Date(cell.getValue<string>()).toLocaleString() },
  ]

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: upstreamKeys.all }),
      queryClient.invalidateQueries({ queryKey: upstreamKeys.status }),
    ])
  }

  async function handleSave(values: NginxUpstreamSaveRequest) {
    try {
      await saveMutation.mutateAsync(values)
      notifySuccess({ message: editing ? '上游组已更新并同步' : '上游组已创建并同步' })
      modal.close()
      await refresh()
    } catch (error) {
      const desiredSaved = error instanceof ApiError && error.details?.desired_saved === true
      if (desiredSaved) {
        modal.close()
        await refresh()
      }
      showErrorModal(error, desiredSaved ? '配置已保存，但同步 Nginx 失败' : '保存上游组失败')
    }
  }

  async function handleSync() {
    try {
      await syncMutation.mutateAsync()
      notifySuccess({ message: '上游配置已重新同步' })
      await refresh()
    } catch (error) {
      showErrorModal(error, '同步上游配置失败')
      await refresh()
    }
  }

  function handleDelete(item: NginxUpstream) {
    if (item.reference_count > 0) return
    confirmDanger({
      title: '删除上游组',
      message: `确认删除上游组「${item.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除上游组失败',
      onConfirm: async () => {
        try {
          await deleteMutation.mutateAsync(item.id)
          notifySuccess({ message: '上游组已删除' })
        } catch (error) {
          const desiredSaved = error instanceof ApiError && error.details?.desired_saved === true
          showErrorModal(error, desiredSaved ? '上游组已删除，但同步 Nginx 失败' : '删除上游组失败')
        } finally {
          await refresh()
        }
      },
    })
  }

  return (
    <SectionCard
      title="上游管理"
      actions={(
        <Group gap="xs" className="upstreamHeaderActions">
          <Badge color={statusQuery.isLoading ? 'gray' : statusQuery.data?.synced ? 'green' : 'yellow'} variant="light">
            {statusQuery.isLoading ? '状态加载中' : statusQuery.data?.synced ? '已同步' : '待同步'}
          </Badge>
          {!statusQuery.isLoading && !statusQuery.data?.synced ? <Button size="xs" variant="light" leftSection={<IconUpload size={15} />} loading={syncMutation.isPending} onClick={handleSync}>重新同步</Button> : null}
        </Group>
      )}
    >
      <Stack gap="md">
        {listQuery.isError ? <ErrorAlert error={listQuery.error} title="加载上游组失败" /> : null}
        {statusQuery.isError ? <ErrorAlert error={statusQuery.error} title="加载同步状态失败" /> : null}
        <DataTable
          columns={columns}
          data={rows}
          loading={listQuery.isLoading || listQuery.isFetching || statusQuery.isFetching}
          emptyText="暂无上游组"
          plain
          toolbarActions={(
            <Group gap="xs" className="upstreamToolbar">
              <TextInput value={search} onChange={(event) => setSearch(event.currentTarget.value)} placeholder="搜索名称或成员地址" w={250} />
              <Select value={algorithm} onChange={setAlgorithm} clearable placeholder="全部策略" w={165} data={Object.entries(algorithmLabels).map(([value, label]) => ({ value, label }))} />
              <Tooltip label="刷新"><ActionIcon aria-label="刷新上游组" variant="default" size="lg" onClick={() => refresh()}><IconRefresh size={17} /></ActionIcon></Tooltip>
              <Button leftSection={<IconPlus size={16} />} onClick={() => { setEditing(null); modal.open() }}>新建</Button>
            </Group>
          )}
          renderRowActions={({ row }) => (
            <Group gap={4} wrap="nowrap">
              <Tooltip label="编辑"><ActionIcon aria-label={`编辑上游组 ${row.original.name}`} variant="subtle" onClick={() => { setEditing(row.original); modal.open() }}><IconEdit size={16} /></ActionIcon></Tooltip>
              <Tooltip label={row.original.reference_count > 0 ? `仍被 ${row.original.reference_count} 个代理引用，无法删除` : '删除'}>
                <span><ActionIcon aria-label={`删除上游组 ${row.original.name}`} color="red" variant="subtle" disabled={row.original.reference_count > 0} loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></span>
              </Tooltip>
            </Group>
          )}
        />
      </Stack>
      <UpstreamEditorModal opened={opened} upstream={editing} saving={saveMutation.isPending} onClose={modal.close} onSave={handleSave} />
    </SectionCard>
  )
}
