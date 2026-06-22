import { ActionIcon, Badge, Button, Group, Stack, Switch, Text, Tooltip } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDownload, IconEye, IconPlayerPlay, IconRefresh, IconRocket, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useMemo, useState } from 'react'
import { deleteOrder, deployOrder, downloadOrder, forceObtainOrder, listACMEOrders, renewOrder, setAutoRenew, type ACMEOrderItem } from '@/api/acme'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { TimeCell } from '@/components/common/TimeCell'
import { ACMEApplyModal } from '@/components/site-detail/ACMEApplyModal'
import { ACMEEmailModal } from '@/components/site-detail/ACMEEmailModal'
import { ACMEErrorDetailModal } from '@/components/site-detail/ACMEErrorDetailModal'
import { ACMELogModal } from '@/components/site-detail/ACMELogModal'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteACMETabProps {
  site: SiteDetail
  onChanged: () => Promise<void>
}

function statusLabel(status: string): string {
  if (status === 'success') return '成功'
  if (status === 'failed') return '失败'
  if (status === 'pre_validation_failed') return '预验证失败'
  if (status === 'processing') return '处理中'
  if (status === 'verifying') return '验证中'
  return status || '-'
}

function statusColor(status: string): string {
  if (status === 'success') return 'green'
  if (status === 'failed') return 'red'
  if (status === 'pre_validation_failed') return 'yellow'
  return 'blue'
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

export function SiteACMETab({ site, onChanged }: SiteACMETabProps) {
  const queryClient = useQueryClient()
  const [applyOpened, setApplyOpened] = useState(false)
  const [emailOpened, setEmailOpened] = useState(false)
  const [logOrderId, setLogOrderId] = useState<string | null>(null)
  const [errorOrder, setErrorOrder] = useState<ACMEOrderItem | null>(null)
  const ordersQueryKey = ['site-detail', site.id, 'acme-orders'] as const
  const ordersQuery = useQuery({ queryKey: ordersQueryKey, queryFn: () => listACMEOrders(site.id) })
  const renewMutation = useMutation({ mutationFn: renewOrder })
  const forceMutation = useMutation({ mutationFn: forceObtainOrder })
  const deleteMutation = useMutation({ mutationFn: deleteOrder })
  const deployMutation = useMutation({ mutationFn: (orderId: string) => deployOrder(orderId, { site_id: site.id, force_https: true }) })
  const autoRenewMutation = useMutation({ mutationFn: ({ orderId, enabled }: { orderId: string; enabled: boolean }) => setAutoRenew(orderId, enabled) })

  const columns = useMemo<MRT_ColumnDef<ACMEOrderItem>[]>(() => [
    { accessorKey: 'domains', header: '域名', size: 220, Cell: ({ row }) => <MonoText value={row.original.domains?.join(', ') || '-'} maxWidth={260} /> },
    { accessorKey: 'expires_at', header: '到期时间', size: 170, Cell: ({ row }) => <TimeCell value={row.original.expires_at} /> },
    { accessorKey: 'status', header: '状态', size: 120, Cell: ({ row }) => <Badge color={statusColor(row.original.status)} variant="light">{statusLabel(row.original.status)}</Badge> },
    { accessorKey: 'email', header: '邮箱', size: 180, Cell: ({ row }) => <MonoText value={row.original.email || '-'} maxWidth={220} /> },
  ], [])

  function startLogStream(orderId: string) {
    setLogOrderId(orderId)
  }

  async function refreshOrders() {
    await queryClient.invalidateQueries({ queryKey: ordersQueryKey })
  }

  async function checkOrderResult(orderId: string) {
    try {
      const orders = await listACMEOrders(site.id)
      queryClient.setQueryData(ordersQueryKey, orders)
      const order = orders.find((item) => item.id === orderId)
      if (!order) return
      if (order.status === 'success') {
        setLogOrderId(null)
        notifySuccess({ message: '证书申请成功，已自动部署' })
        await onChanged()
        return
      }
      if (order.status === 'pre_validation_failed') {
        modals.openConfirmModal({
          title: '预验证失败',
          children: '预验证未通过，继续提交可能被 Let\'s Encrypt 限制频率。确认继续提交？',
          labels: { confirm: '继续提交', cancel: '取消' },
          confirmProps: { color: 'yellow' },
          closeOnConfirm: false,
          onConfirm: async () => {
            modals.closeAll()
            try {
              const result = await forceMutation.mutateAsync(orderId)
              startLogStream(result.order_id)
            } catch (error) {
              showErrorModal(error, '继续提交失败')
            }
          },
        })
        return
      }
      if (order.status === 'failed') {
        setErrorOrder(order)
      }
    } catch (error) {
      showErrorModal(error, '刷新申请结果失败')
      await refreshOrders()
    }
  }

  async function handleRenew(order: ACMEOrderItem) {
    try {
      const result = await renewMutation.mutateAsync(order.id)
      startLogStream(result.order_id)
    } catch (error) {
      showErrorModal(error, '续签失败')
    }
  }

  function handleForce(order: ACMEOrderItem) {
    modals.openConfirmModal({
      title: '继续提交',
      children: '预验证未通过，继续提交可能被 Let\'s Encrypt 限制频率。确认继续提交？',
      labels: { confirm: '继续提交', cancel: '取消' },
      confirmProps: { color: 'yellow' },
      closeOnConfirm: false,
      onConfirm: async () => {
        modals.closeAll()
        try {
          const result = await forceMutation.mutateAsync(order.id)
          startLogStream(result.order_id)
        } catch (error) {
          showErrorModal(error, '继续提交失败')
        }
      },
    })
  }

  function handleDelete(order: ACMEOrderItem) {
    confirmDanger({
      title: '删除记录',
      message: '确认删除该申请记录？',
      confirmLabel: '确认删除',
      errorTitle: '删除申请记录失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(order.id)
        notifySuccess({ message: '已删除' })
        await refreshOrders()
      },
    })
  }

  async function handleDownload(order: ACMEOrderItem) {
    try {
      const blob = await downloadOrder(order.id)
      downloadBlob(blob, `${order.domains?.[0] || 'certificate'}_ssl.zip`)
    } catch (error) {
      showErrorModal(error, '下载证书失败')
    }
  }

  function handleDeploy(order: ACMEOrderItem) {
    confirmDanger({
      title: '部署证书',
      message: `确认将证书部署到站点 ${site.primary_domain || ''}？`,
      confirmLabel: '确认部署',
      errorTitle: '部署证书失败',
      onConfirm: async () => {
        await deployMutation.mutateAsync(order.id)
        notifySuccess({ message: '证书已部署' })
        await Promise.all([refreshOrders(), onChanged()])
      },
    })
  }

  async function handleAutoRenew(order: ACMEOrderItem, enabled: boolean) {
    try {
      await autoRenewMutation.mutateAsync({ orderId: order.id, enabled })
      await refreshOrders()
    } catch (error) {
      showErrorModal(error, '自动续期设置失败')
      await refreshOrders()
    }
  }

  return (
    <Stack gap="md">
      {ordersQuery.isError ? <ErrorAlert error={ordersQuery.error} title="加载 ACME 申请记录失败" /> : null}
      <DataTable
        columns={columns}
        data={ordersQuery.data || []}
        loading={ordersQuery.isLoading || ordersQuery.isFetching}
        emptyText="暂无申请记录"
        toolbarActions={(
          <Group gap="xs">
            <Button onClick={() => setApplyOpened(true)}>申请证书</Button>
            <Button variant="light" onClick={() => setEmailOpened(true)}>邮箱管理</Button>
            <Button variant="light" leftSection={<IconRefresh size={16} />} loading={ordersQuery.isFetching} onClick={() => ordersQuery.refetch()}>刷新</Button>
          </Group>
        )}
        renderRowActions={({ row }) => {
          const order = row.original
          return (
            <Group gap={4} wrap="nowrap">
              {order.status === 'success' ? <Tooltip label="部署"><ActionIcon variant="subtle" loading={deployMutation.isPending} onClick={() => handleDeploy(order)}><IconRocket size={16} /></ActionIcon></Tooltip> : null}
              {order.status === 'success' ? <Tooltip label="下载"><ActionIcon variant="subtle" onClick={() => handleDownload(order)}><IconDownload size={16} /></ActionIcon></Tooltip> : null}
              {order.status === 'success' ? <Tooltip label="续签"><ActionIcon variant="subtle" loading={renewMutation.isPending} onClick={() => handleRenew(order)}><IconRefresh size={16} /></ActionIcon></Tooltip> : null}
              {order.status === 'pre_validation_failed' ? <Tooltip label="继续提交"><ActionIcon color="yellow" variant="subtle" loading={forceMutation.isPending} onClick={() => handleForce(order)}><IconPlayerPlay size={16} /></ActionIcon></Tooltip> : null}
              {order.status === 'failed' || order.status === 'pre_validation_failed' ? <Tooltip label="失败详情"><ActionIcon color="red" variant="subtle" onClick={() => setErrorOrder(order)}><IconEye size={16} /></ActionIcon></Tooltip> : null}
              {order.status === 'success' ? <Switch checked={order.auto_renew} disabled={autoRenewMutation.isPending} onChange={(event) => handleAutoRenew(order, event.currentTarget.checked)} /> : null}
              <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(order)}><IconTrash size={16} /></ActionIcon></Tooltip>
            </Group>
          )
        }}
      />
      <Text size="xs" c="dimmed">文件验证使用 HTTP-01，请确保域名已解析到当前服务器且 80 端口可访问。</Text>
      <ACMEApplyModal site={site} opened={applyOpened} onClose={() => setApplyOpened(false)} onStarted={startLogStream} />
      <ACMEEmailModal opened={emailOpened} onClose={() => setEmailOpened(false)} />
      <ACMELogModal orderId={logOrderId} opened={Boolean(logOrderId)} onClose={() => setLogOrderId(null)} onDone={() => logOrderId && void checkOrderResult(logOrderId)} />
      <ACMEErrorDetailModal order={errorOrder} opened={Boolean(errorOrder)} onClose={() => setErrorOrder(null)} />
    </Stack>
  )
}
