import { Badge, Text } from '@mantine/core'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { useMemo } from 'react'
import type { AccessEntry } from '@/api/types'
import { DataTable } from '@/components/tables/DataTable'
import { TimeCell } from '@/components/common/TimeCell'

export function AccessEntryTable({ data, total, loading, pagination, onPaginationChange }: { data: AccessEntry[]; total: number; loading: boolean; pagination: MRT_PaginationState; onPaginationChange: (updater: MRT_PaginationState | ((old: MRT_PaginationState) => MRT_PaginationState)) => void }) {
  const columns = useMemo<MRT_ColumnDef<AccessEntry>[]>(() => [
    { accessorKey: 'ts', header: '时间', Cell: ({ row }) => <TimeCell value={row.original.ts} /> },
    { accessorKey: 'ip', header: 'IP' },
    { accessorKey: 'method', header: '方法' },
    { accessorKey: 'path', header: '路径', Cell: ({ row }) => <Text lineClamp={1}>{row.original.path}</Text> },
    { accessorKey: 'status', header: '状态', Cell: ({ row }) => <Badge color={row.original.status >= 500 ? 'red' : row.original.status >= 400 ? 'yellow' : 'green'} variant="light">{row.original.status}</Badge> },
    { accessorKey: 'bytes', header: 'bytes' },
    { accessorKey: 'user_agent', header: 'UA', Cell: ({ row }) => <Text lineClamp={1}>{row.original.user_agent}</Text> },
  ], [])
  return <DataTable columns={columns} data={data} rowCount={total} loading={loading} pagination={pagination} onPaginationChange={onPaginationChange} emptyText="暂无访问明细" plain />
}
