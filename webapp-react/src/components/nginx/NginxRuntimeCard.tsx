import { Badge, Button, Divider, Group, SimpleGrid, Skeleton, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlayerPlay, IconPlayerStop, IconRefresh, IconRotateClockwise } from '@tabler/icons-react'
import { useRef, useState } from 'react'
import { controlNginx, getNginxStatus, reloadNginx, type NginxControlAction } from '@/api/nginx'
import type { SystemOverview } from '@/api/types'
import { NginxOverviewCard } from './NginxOverviewCard'
import { queryKeys } from '@/api/hooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const statusKey = ['nginx', 'runtime'] as const
const labels = { start: '启动', stop: '停止', restart: '重启', reload: '重载配置' }
type Action = NginxControlAction | 'reload'

function duration(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor(seconds % 86400 / 3600)
  const minutes = Math.floor(seconds % 3600 / 60)
  return days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分钟` : `${minutes} 分钟 ${seconds % 60} 秒`
}

export function NginxRuntimeCard({ overview, overviewLoading }: { overview?: SystemOverview; overviewLoading?: boolean }) {
  const client = useQueryClient()
  const busy = useRef(false)
  const [operationId, setOperationId] = useState('')
  const query = useQuery({ queryKey: statusKey, queryFn: getNginxStatus, refetchInterval: 5000 })
  const status = query.isError ? undefined : query.data
  const mutation = useMutation({
    mutationFn: (action: Action) => action === 'reload' ? reloadNginx({ test_before_reload: true }) : controlNginx(action),
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: statusKey }),
        client.invalidateQueries({ queryKey: queryKeys.systemOverview }),
      ])
    },
  })

  async function execute(action: Action) {
    if (busy.current) return
    busy.current = true
    setOperationId('')
    try {
      const result = await mutation.mutateAsync(action)
      setOperationId(result.operation_id)
      notifySuccess({ message: `Nginx ${labels[action]}成功` })
    } catch (error) {
      showErrorModal(error, `Nginx ${labels[action]}失败`)
    } finally {
      busy.current = false
    }
  }

  function handleAction(action: Action) {
    if (action === 'start') { void execute(action); return }
    const message = action === 'stop'
      ? '停止 Nginx 后，所有由它提供服务的站点将不可访问；如果面板通过该 Nginx 反向代理访问，当前入口也会断开。请确保可通过面板直连地址恢复启动。确认停止？'
      : action === 'restart'
        ? '重启会先检查配置，再停止并重新启动 Nginx，期间所有站点可能短暂中断，现有连接可能断开。确认重启？'
        : '先执行 nginx -t，检查通过后平滑重载配置。日常配置变更建议使用重载，而不是重启。确认继续？'
    confirmDanger({ title: `${labels[action]} Nginx`, message, confirmLabel: `确认${labels[action]}`, onConfirm: () => execute(action) })
  }

  const disabled = mutation.isPending || !status
  return (
    <SectionCard title="运行状态与控制" description="每 5 秒刷新运行指标；日常配置变更优先使用重载，无需重启进程。" actions={
      <Button variant="default" leftSection={<IconRefresh size={16} />} loading={query.isFetching} onClick={() => { void query.refetch(); void client.invalidateQueries({ queryKey: queryKeys.systemOverview }) }}>刷新状态</Button>
    }>
      <Stack gap="md">
        {query.isError ? <ErrorAlert error={query.error} title="无法读取运行状态，请刷新重试" /> : null}
        {query.isLoading ? <Skeleton height={72} /> : (
          <SimpleGrid cols={{ base: 2, sm: 3, lg: 6 }} spacing="md">
            <Stack gap={4}><Text size="xs" c="dimmed">服务状态</Text><Badge variant="light" color={!status ? 'gray' : status.running ? 'green' : 'red'}>{!status ? '未知' : status.running ? '运行中' : '已停止'}</Badge></Stack>
            <Stack gap={4}><Text size="xs" c="dimmed">{status?.memory_pss != null ? '内存占用 · PSS' : '内存合计 · RSS'}</Text><Text fw={600}>{status ? `${((status.memory_pss ?? status.memory_rss) / 1024 / 1024).toFixed(1)} MiB` : '—'}</Text>{status?.memory_pss != null ? <Text size="xs" c="dimmed">RSS 合计 {(status.memory_rss / 1024 / 1024).toFixed(1)} MiB</Text> : null}</Stack>
            <Stack gap={4}><Text size="xs" c="dimmed">主进程 PID</Text><Text>{status?.running ? status.master_pid : '—'}</Text></Stack>
            <Stack gap={4}><Text size="xs" c="dimmed">运行时长</Text><Text>{status?.running ? duration(status.uptime_seconds) : '—'}</Text></Stack>
            <Stack gap={4}><Text size="xs" c="dimmed">Worker 进程</Text><Text>{status ? status.workers : '—'}</Text></Stack>
            <Stack gap={4}><Text size="xs" c="dimmed">总进程数</Text><Text>{status ? status.processes : '—'}</Text></Stack>
          </SimpleGrid>
        )}
        <Text size="xs" c="dimmed">PSS 按比例分摊共享内存；RSS 合计可能重复计入共享内存。{status?.running && status.memory_pss == null ? '当前无法读取 PSS，已回退显示 RSS。' : ''}运行时长从主进程启动计算，重载不会重置。</Text>
        <Divider />
        <Group gap="sm" wrap="wrap" role="group" aria-label="Nginx 服务控制">
          <Button leftSection={<IconRefresh size={16} />} disabled={disabled || !status?.running} loading={mutation.isPending && mutation.variables === 'reload'} onClick={() => handleAction('reload')}>重载配置</Button>
          <Button variant="light" color="green" leftSection={<IconPlayerPlay size={16} />} disabled={disabled || status?.running} loading={mutation.isPending && mutation.variables === 'start'} onClick={() => handleAction('start')}>启动 Nginx</Button>
          <Button variant="default" leftSection={<IconRotateClockwise size={16} />} disabled={disabled || !status?.running} loading={mutation.isPending && mutation.variables === 'restart'} onClick={() => handleAction('restart')}>重启 Nginx</Button>
          <Button variant="light" color="red" leftSection={<IconPlayerStop size={16} />} disabled={disabled || !status?.running} loading={mutation.isPending && mutation.variables === 'stop'} onClick={() => handleAction('stop')}>停止 Nginx</Button>
        </Group>
        {operationId ? <Group gap="xs"><Text size="xs" c="dimmed">最近操作记录</Text><MonoText value={operationId} maxWidth="100%" /></Group> : null}
        <Divider />
        <NginxOverviewCard overview={overview} loading={overviewLoading} />
      </Stack>
    </SectionCard>
  )
}
