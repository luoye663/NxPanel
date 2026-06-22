import { Button, Group, SegmentedControl, Select } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlayerPlay, IconPlayerStop, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { withGatePrefix } from '@/api/gate'
import { clearServiceLog, getServiceLog } from '@/api/logs'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'
import { LogViewer } from './LogViewer'

const DEFAULT_LOG_LINES = 200

const lineOptions = [
  { label: '200 行', value: '200' },
  { label: '500 行', value: '500' },
  { label: '1000 行', value: '1000' },
  { label: '2000 行', value: '2000' },
]

export function ServiceLogTab() {
  const queryClient = useQueryClient()
  const [service, setService] = useState('api')
  const [lines, setLines] = useState(DEFAULT_LOG_LINES)
  const [streaming, setStreaming] = useState(false)
  const [streamLines, setStreamLines] = useState<string[]>([])
  const logQuery = useQuery({
    queryKey: ['service-log', service, lines],
    queryFn: () => getServiceLog({ service, lines }),
    enabled: !streaming,
  })
  const clearMutation = useMutation({
    mutationFn: () => clearServiceLog({ service }),
    onSuccess: async () => {
      notifySuccess({ message: '日志已清空' })
      setStreamLines([])
      await queryClient.invalidateQueries({ queryKey: ['service-log', service] })
    },
  })

  useEffect(() => {
    setStreaming(false)
    setStreamLines([])
  }, [service])

  useEffect(() => {
    if (!streaming) return
    const source = new EventSource(withGatePrefix(`/system/service-logs/stream?service=${service}&from=tail`))
    source.addEventListener('line', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as { line: string }
      // 运行日志实时追踪只保留最近 1000 行，避免长时间打开页面造成内存增长。
      setStreamLines((current) => [...current, payload.line].slice(-1000))
    })
    source.addEventListener('error', () => source.close())
    return () => source.close()
  }, [service, streaming])

  function handleClear() {
    confirmDanger({
      title: '清空运行日志',
      message: `确定要清空 ${service === 'api' ? 'API' : 'Agent'} 服务日志吗？`,
      confirmLabel: '清空日志',
      errorTitle: '清空运行日志失败',
      onConfirm: async () => { await clearMutation.mutateAsync() },
    })
  }

  const visibleLines = streaming ? streamLines : (logQuery.data?.lines ?? [])

  return (
    <>
      {logQuery.isError ? <ErrorAlert error={logQuery.error} title="加载运行日志失败" /> : null}
      <Group justify="space-between" mb="sm" gap="sm" className="logsToolbar">
        <SegmentedControl value={service} onChange={setService} data={[{ label: 'API 服务', value: 'api' }, { label: 'Agent 服务', value: 'agent' }]} />
        <Group gap="xs">
          <Select w={110} data={lineOptions} value={String(lines)} allowDeselect={false} onChange={(value) => value && setLines(Number(value))} />
          <Button variant="light" leftSection={<IconRefresh size={16} />} disabled={streaming} loading={!streaming && logQuery.isFetching} onClick={() => logQuery.refetch()}>刷新</Button>
          <Button variant={streaming ? 'filled' : 'light'} leftSection={streaming ? <IconPlayerStop size={16} /> : <IconPlayerPlay size={16} />} onClick={() => setStreaming((value) => !value)}>{streaming ? '停止追踪' : '实时追踪'}</Button>
          <Button color="red" variant="light" leftSection={<IconTrash size={16} />} loading={clearMutation.isPending} onClick={handleClear}>清空日志</Button>
        </Group>
      </Group>
      <LogViewer lines={visibleLines} truncated={!streaming && logQuery.data?.truncated} loading={!streaming && (logQuery.isLoading || logQuery.isFetching)} autoScroll={streaming} />
    </>
  )
}
