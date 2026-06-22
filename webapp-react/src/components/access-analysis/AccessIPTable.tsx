import { Text } from '@mantine/core'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { useMemo } from 'react'
import type { AccessIPStat } from '@/api/types'
import { DataTable } from '@/components/tables/DataTable'
import { TimeCell } from '@/components/common/TimeCell'

export function AccessIPTable({ data, total, loading, pagination, onPaginationChange }: { data: AccessIPStat[]; total: number; loading: boolean; pagination: MRT_PaginationState; onPaginationChange: (updater: MRT_PaginationState | ((old: MRT_PaginationState) => MRT_PaginationState)) => void }) {
  const columns = useMemo<MRT_ColumnDef<AccessIPStat>[]>(() => [
    { accessorKey: 'ip', header: 'IP' },
    { accessorKey: 'requests', header: '访问' },
    { accessorKey: 'unique_paths', header: '路径数' },
    { accessorKey: 'error_requests', header: '错误' },
    { accessorKey: 'sample_user_agent', header: 'UA 摘要', Cell: ({ row }) => <Text lineClamp={1}>{row.original.sample_user_agent || '-'}</Text> },
    { accessorKey: 'last_seen_at', header: '最后访问', Cell: ({ row }) => <TimeCell value={row.original.last_seen_at} /> },
  ], [])
  return <DataTable columns={columns} data={data} rowCount={total} loading={loading} pagination={pagination} onPaginationChange={onPaginationChange} emptyText="暂无 IP 排行" plain />
}
