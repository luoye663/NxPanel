import { ActionIcon, Button, Group, Text, Tooltip } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { IconEye, IconRefresh } from '@tabler/icons-react'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { useMemo, useState } from 'react'
import { getOperations } from '@/api/operations'
import type { OperationItem, SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { StatusBadge } from '@/components/common/StatusBadge'
import { TimeCell } from '@/components/common/TimeCell'
import { OperationResultDrawer } from '@/components/logs/OperationResultDrawer'
import { DataTable } from '@/components/tables/DataTable'

interface SiteOperationsTabProps {
  site: SiteDetail
}

export function SiteOperationsTab({ site }: SiteOperationsTabProps) {
  const [pagination, setPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [selectedOperationId, setSelectedOperationId] = useState<string | null>(null)
  const operationsQuery = useQuery({
    queryKey: ['site-detail', site.id, 'operations', pagination.pageIndex, pagination.pageSize],
    queryFn: () => getOperations({ page: pagination.pageIndex + 1, page_size: pagination.pageSize, target_id: site.id }),
  })

  const columns = useMemo<MRT_ColumnDef<OperationItem>[]>(() => [
    { accessorKey: 'action', header: '操作', size: 170 },
    { accessorKey: 'status', header: '状态', size: 110, Cell: ({ cell }) => <StatusBadge kind="operation" value={String(cell.getValue() || '')} /> },
    { accessorKey: 'message', header: '消息', size: 280, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={360} /> },
    { accessorKey: 'created_at', header: '时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.created_at} /> },
  ], [])

  return (
    <SectionCard>
      {operationsQuery.isError ? <ErrorAlert error={operationsQuery.error} title="加载站点操作记录失败" /> : null}
      <DataTable
        columns={columns}
        data={operationsQuery.data?.items || []}
        rowCount={operationsQuery.data?.total || 0}
        loading={operationsQuery.isLoading || operationsQuery.isFetching}
        pagination={pagination}
        onPaginationChange={setPagination}
        emptyText="暂无站点操作记录"
        toolbarActions={(
          <Group justify="space-between" w="100%" gap="sm">
            <Button variant="light" leftSection={<IconRefresh size={16} />} loading={operationsQuery.isFetching} onClick={() => operationsQuery.refetch()}>刷新</Button>
            <Text size="sm" c="dimmed">仅展示当前站点相关操作。</Text>
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
      <OperationResultDrawer opened={Boolean(selectedOperationId)} operationId={selectedOperationId} onClose={() => setSelectedOperationId(null)} />
    </SectionCard>
  )
}
