import { Text } from '@mantine/core'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { useMemo } from 'react'
import type { AccessPathStat } from '@/api/types'
import { DataTable } from '@/components/tables/DataTable'
import { TimeCell } from '@/components/common/TimeCell'

export function AccessPathTable({ data, total, loading, pagination, onPaginationChange }: { data: AccessPathStat[]; total: number; loading: boolean; pagination: MRT_PaginationState; onPaginationChange: (updater: MRT_PaginationState | ((old: MRT_PaginationState) => MRT_PaginationState)) => void }) {
  const columns = useMemo<MRT_ColumnDef<AccessPathStat>[]>(() => [
    { accessorKey: 'path', header: '路径', size: 280, Cell: ({ row }) => <Text lineClamp={1}>{row.original.path}</Text> },
    { accessorKey: 'requests', header: '访问' },
    { accessorKey: 'unique_ips', header: '独立 IP' },
    { accessorKey: 'status_4xx', header: '4xx' },
    { accessorKey: 'status_5xx', header: '5xx' },
    { accessorKey: 'bytes', header: '流量' },
    { accessorKey: 'last_seen_at', header: '最后访问', Cell: ({ row }) => <TimeCell value={row.original.last_seen_at} /> },
  ], [])
  return <DataTable columns={columns} data={data} rowCount={total} loading={loading} pagination={pagination} onPaginationChange={onPaginationChange} emptyText="暂无路径排行" plain />
}
