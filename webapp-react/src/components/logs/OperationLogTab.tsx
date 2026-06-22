import { ActionIcon, Button, Group, Text, Tooltip } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEye, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useMemo, useState } from 'react'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { clearOperations, getOperations } from '@/api/operations'
import type { OperationItem } from '@/api/types'
import { MonoText } from '@/components/common/MonoText'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { StatusBadge } from '@/components/common/StatusBadge'
import { TimeCell } from '@/components/common/TimeCell'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'
import { OperationResultDrawer } from './OperationResultDrawer'

export function OperationLogTab() {
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [selectedOperationId, setSelectedOperationId] = useState<string | null>(null)
  const [manualRefreshing, setManualRefreshing] = useState(false)

  const operationsQuery = useQuery({
    queryKey: ['operations', { page: pagination.pageIndex + 1, page_size: pagination.pageSize }],
    queryFn: () => getOperations({ page: pagination.pageIndex + 1, page_size: pagination.pageSize }),
  })

  const clearMutation = useMutation({
    mutationFn: clearOperations,
    onSuccess: async () => {
      notifySuccess({ message: '操作日志已清空' })
      setPagination((current) => ({ ...current, pageIndex: 0 }))
      await queryClient.invalidateQueries({ queryKey: ['operations'] })
    },
  })

  const columns = useMemo<MRT_ColumnDef<OperationItem>[]>(() => [
    { accessorKey: 'action', header: '操作', size: 170 },
    { accessorKey: 'target_type', header: '目标类型', size: 110, Cell: ({ cell }) => <StatusBadge kind="task" value="other" label={String(cell.getValue() || '-')} /> },
    { accessorKey: 'target_id', header: '目标 ID', size: 170, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={160} /> },
    { accessorKey: 'status', header: '状态', size: 110, Cell: ({ cell }) => <StatusBadge kind="operation" value={String(cell.getValue() || '')} /> },
    { accessorKey: 'message', header: '消息', size: 260, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={320} /> },
    { accessorKey: 'created_at', header: '时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.created_at} /> },
  ], [])

  function handleClear() {
    confirmDanger({
      title: '清空操作日志',
      message: '确定要清空所有操作日志吗？此操作不可恢复。',
      confirmLabel: '清空日志',
      errorTitle: '清空操作日志失败',
      onConfirm: async () => { await clearMutation.mutateAsync() },
    })
  }

  async function handleRefresh() {
    setManualRefreshing(true)
    try {
      // 手动刷新通常很快结束，保留一个最短时长让表格进度条有明确反馈。
      await Promise.all([operationsQuery.refetch(), new Promise((resolve) => window.setTimeout(resolve, 300))])
    } finally {
      setManualRefreshing(false)
    }
  }

  return (
    <>
      {operationsQuery.isError ? <ErrorAlert error={operationsQuery.error} title="加载操作日志失败" /> : null}
      <DataTable
        columns={columns}
        data={operationsQuery.data?.items ?? []}
        rowCount={operationsQuery.data?.total ?? 0}
        loading={operationsQuery.isLoading || operationsQuery.isFetching || manualRefreshing}
        pagination={pagination}
        onPaginationChange={setPagination}
        emptyText="暂无操作日志"
        toolbarActions={(
          <Group justify="space-between" w="100%" gap="sm">
            <Text size="sm" c="dimmed">全局写操作、状态变更与失败回滚记录。</Text>
            <Group gap="xs">
              <Button variant="light" leftSection={<IconRefresh size={16} />} loading={operationsQuery.isFetching || manualRefreshing} onClick={handleRefresh}>刷新</Button>
              <Button color="red" variant="light" leftSection={<IconTrash size={16} />} loading={clearMutation.isPending} onClick={handleClear}>清空日志</Button>
            </Group>
          </Group>
        )}
        renderRowActions={({ row }) => (
          <Tooltip label="查看详情">
            <ActionIcon variant="subtle" aria-label="查看操作详情" onClick={() => setSelectedOperationId(row.original.id)}>
              <IconEye size={16} />
            </ActionIcon>
          </Tooltip>
        )}
      />
      <OperationResultDrawer
        opened={Boolean(selectedOperationId)}
        operationId={selectedOperationId}
        onClose={() => setSelectedOperationId(null)}
      />
    </>
  )
}
