import { Text } from '@mantine/core'
import {
  MantineReactTable,
  type MRT_ColumnDef,
  type MRT_PaginationState,
  type MRT_RowData,
  type MRT_TableOptions,
} from 'mantine-react-table'
import type { ReactNode } from 'react'

const zhHansLocalization = {
  actions: '操作',
  noRecordsToDisplay: '暂无记录',
  rowsPerPage: '每页行数',
  of: '/',
  goToNextPage: '下一页',
  goToPreviousPage: '上一页',
  goToFirstPage: '第一页',
  goToLastPage: '最后一页',
}

interface DataTableProps<TData extends MRT_RowData> {
  columns: MRT_ColumnDef<TData>[]
  data: TData[]
  rowCount?: number
  loading?: boolean
  initialLoading?: boolean
  pagination?: MRT_PaginationState
  onPaginationChange?: MRT_TableOptions<TData>['onPaginationChange']
  renderRowActions?: MRT_TableOptions<TData>['renderRowActions']
  mantineTableBodyRowProps?: MRT_TableOptions<TData>['mantineTableBodyRowProps']
  toolbarActions?: ReactNode
  emptyText?: string
  plain?: boolean
}

export function DataTable<TData extends MRT_RowData>({
  columns,
  data,
  rowCount = data.length,
  loading = false,
  initialLoading,
  pagination,
  onPaginationChange,
  renderRowActions,
  mantineTableBodyRowProps,
  toolbarActions,
  emptyText = '暂无数据',
  plain = false,
}: DataTableProps<TData>) {
  // MRT 传入 pagination 字段后会按受控分页读取 pageSize；无分页表格不能传 undefined。
  const showInitialLoading = initialLoading ?? (loading && data.length === 0)
  const tableState = pagination
    ? { isLoading: showInitialLoading, pagination, showProgressBars: loading }
    : { isLoading: showInitialLoading, showProgressBars: loading }

  return (
    <MantineReactTable
      columns={columns}
      data={data}
      displayColumnDefOptions={{ 'mrt-row-actions': { header: '操作', size: 96 } }}
      enableColumnActions={false}
      enableColumnFilters={false}
      enableColumnPinning={Boolean(renderRowActions)}
      enableDensityToggle={false}
      enableFullScreenToggle={false}
      enableGlobalFilter={false}
      enableRowActions={Boolean(renderRowActions)}
      enableTopToolbar={Boolean(toolbarActions)}
      localization={zhHansLocalization}
      manualPagination={Boolean(pagination)}
      initialState={renderRowActions ? { columnPinning: { right: ['mrt-row-actions'] } } : undefined}
      mantinePaperProps={plain ? { withBorder: false, shadow: 'none', radius: 0, style: { background: 'transparent' } } : { withBorder: true, shadow: 'none', radius: 'md' }}
      mantinePaginationProps={{ rowsPerPageOptions: ['10', '20', '30', '50'], showRowsPerPage: true }}
      mantineTableBodyCellProps={{ style: { fontSize: 'var(--mantine-font-size-sm)', verticalAlign: 'middle' } }}
      mantineTableBodyRowProps={mantineTableBodyRowProps}
      mantineTableContainerProps={{ className: plain ? 'dataTableScroll plainDataTableScroll' : 'dataTableScroll' }}
      mantineTableHeadCellProps={{ style: { fontSize: 'var(--mantine-font-size-xs)' } }}
      positionActionsColumn="last"
      renderEmptyRowsFallback={() => <Text ta="center" c="dimmed" py="xl">{emptyText}</Text>}
      renderRowActions={renderRowActions}
      renderTopToolbarCustomActions={() => toolbarActions}
      rowCount={rowCount}
      state={tableState}
      onPaginationChange={onPaginationChange}
    />
  )
}
