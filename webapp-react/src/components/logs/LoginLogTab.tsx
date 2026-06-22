import { Badge, Button, Group, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconRefresh, IconTrash } from '@tabler/icons-react'
import { useMemo, useState } from 'react'
import type { MRT_ColumnDef, MRT_PaginationState } from 'mantine-react-table'
import { clearLoginAudit, getLoginAudit } from '@/api/logs'
import type { LoginAuditItem } from '@/api/types'
import { MonoText } from '@/components/common/MonoText'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { StatusBadge } from '@/components/common/StatusBadge'
import { TimeCell } from '@/components/common/TimeCell'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'

export function LoginLogTab() {
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState<MRT_PaginationState>({ pageIndex: 0, pageSize: 20 })
  const [manualRefreshing, setManualRefreshing] = useState(false)

  const loginQuery = useQuery({
    queryKey: ['login-audit', { page: pagination.pageIndex + 1, page_size: pagination.pageSize }],
    queryFn: () => getLoginAudit({ page: pagination.pageIndex + 1, page_size: pagination.pageSize }),
  })

  const clearMutation = useMutation({
    mutationFn: clearLoginAudit,
    onSuccess: async () => {
      notifySuccess({ message: '登录日志已清空' })
      setPagination((current) => ({ ...current, pageIndex: 0 }))
      await queryClient.invalidateQueries({ queryKey: ['login-audit'] })
    },
  })

  const columns = useMemo<MRT_ColumnDef<LoginAuditItem>[]>(() => [
    { accessorKey: 'ip', header: '登录 IP', size: 150, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={150} /> },
    { accessorKey: 'user_agent', header: '浏览器 UA', size: 300, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={360} /> },
    { accessorKey: 'success', header: '状态', size: 100, Cell: ({ row }) => <StatusBadge kind="login" value={row.original.success} /> },
    { accessorKey: 'failure_reason', header: '失败原因', size: 180, Cell: ({ cell }) => <MonoText value={String(cell.getValue() || '')} maxWidth={180} /> },
    {
      id: 'methods',
      header: '验证方式',
      size: 150,
      Cell: ({ row }) => (
        <Group gap={4}>
          {row.original.totp_used ? <Badge color="yellow" variant="light">2FA</Badge> : null}
          {row.original.captcha_verified ? <Badge color="blue" variant="light">验证码</Badge> : null}
          {!row.original.totp_used && !row.original.captcha_verified ? <Text size="sm" c="dimmed">-</Text> : null}
        </Group>
      ),
    },
    { accessorKey: 'created_at', header: '时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.created_at} /> },
  ], [])

  function handleClear() {
    confirmDanger({
      title: '清空登录日志',
      message: '确定要清空所有登录日志吗？此操作不可恢复。',
      confirmLabel: '清空日志',
      errorTitle: '清空登录日志失败',
      onConfirm: async () => { await clearMutation.mutateAsync() },
    })
  }

  async function handleRefresh() {
    setManualRefreshing(true)
    try {
      // 手动刷新通常很快结束，保留一个最短时长让表格进度条有明确反馈。
      await Promise.all([loginQuery.refetch(), new Promise((resolve) => window.setTimeout(resolve, 300))])
    } finally {
      setManualRefreshing(false)
    }
  }

  return (
    <>
      {loginQuery.isError ? <ErrorAlert error={loginQuery.error} title="加载登录日志失败" /> : null}
      <DataTable
        columns={columns}
        data={loginQuery.data?.items ?? []}
        rowCount={loginQuery.data?.total ?? 0}
        loading={loginQuery.isLoading || loginQuery.isFetching || manualRefreshing}
        pagination={pagination}
        onPaginationChange={setPagination}
        emptyText="暂无登录日志"
        toolbarActions={(
          <Group justify="space-between" w="100%" gap="sm">
            <Text size="sm" c="dimmed">记录所有管理员登录尝试，包括成功和失败。</Text>
            <Group gap="xs">
              <Button variant="light" leftSection={<IconRefresh size={16} />} loading={loginQuery.isFetching || manualRefreshing} onClick={handleRefresh}>刷新</Button>
              <Button color="red" variant="light" leftSection={<IconTrash size={16} />} loading={clearMutation.isPending} onClick={handleClear}>清空日志</Button>
            </Group>
          </Group>
        )}
      />
    </>
  )
}
