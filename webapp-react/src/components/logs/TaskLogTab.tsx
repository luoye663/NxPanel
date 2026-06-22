import { Badge, Button, Group, Loader, ScrollArea, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { clearTaskLog, getTaskLog, getTaskLogTypes } from '@/api/logs'
import type { TaskLogEntry } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { StatusBadge } from '@/components/common/StatusBadge'
import { formatDateTime } from '@/components/common/TimeCell'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'
import { LogViewer } from './LogViewer'

const LOG_LINES = 500
const taskTypeLabels: Record<string, string> = {
  site_backup: '站点备份',
  acme_renewal: 'SSL 自动续签',
  access_analysis_scan: '访问分析扫描',
  nginx_log_rotation: 'Nginx 日志切割',
  log_rotation: '日志切割',
  ssl_renewal: 'SSL 续签',
  backup_cleanup: '备份清理',
}

function formatSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

export function TaskLogTab() {
  const queryClient = useQueryClient()
  const [selectedTask, setSelectedTask] = useState<string | null>(null)
  const typesQuery = useQuery({ queryKey: ['task-log-types'], queryFn: getTaskLogTypes })
  const tasks = typesQuery.data?.tasks ?? []
  const selectedTaskMeta = tasks.find((task) => task.name === selectedTask)

  // 任务列表加载后默认选中第一项，保持 Vue 版本进入页面即可看到日志的体验。
  useEffect(() => {
    if (!selectedTask && tasks.length > 0) setSelectedTask(tasks[0].name)
  }, [selectedTask, tasks])

  const logQuery = useQuery({
    queryKey: ['task-log', selectedTask, LOG_LINES],
    queryFn: () => getTaskLog({ task: selectedTask!, lines: LOG_LINES }),
    enabled: Boolean(selectedTask),
  })
  const clearMutation = useMutation({
    mutationFn: () => clearTaskLog({ task: selectedTask! }),
    onSuccess: async () => {
      notifySuccess({ message: '日志已清空' })
      await queryClient.invalidateQueries({ queryKey: ['task-log', selectedTask] })
      await queryClient.invalidateQueries({ queryKey: ['task-log-types'] })
    },
  })

  function handleClear() {
    if (!selectedTask) return
    confirmDanger({
      title: '清空任务日志',
      message: `确定要清空 "${selectedTaskMeta?.label ?? selectedTask}" 的日志吗？`,
      confirmLabel: '清空日志',
      errorTitle: '清空任务日志失败',
      onConfirm: async () => { await clearMutation.mutateAsync() },
    })
  }

  return (
    <div className="taskLogLayout">
      <Stack className="taskSidebar" gap={0}>
        <Group justify="space-between" p="sm" className="taskSidebarHeader">
          <Text fw={600} size="sm">任务列表</Text>
          <Button variant="subtle" loading={typesQuery.isFetching} onClick={() => typesQuery.refetch()}>刷新</Button>
        </Group>
        <ScrollArea h={600}>
          {typesQuery.isLoading ? <Group justify="center" py="xl"><Loader size="sm" /></Group> : null}
          {!typesQuery.isLoading && tasks.length === 0 ? <Text c="dimmed" ta="center" py="xl" size="sm">暂无任务日志</Text> : null}
          {tasks.map((task: TaskLogEntry) => (
            <button
              key={task.name}
              className={`taskLogItem ${selectedTask === task.name ? 'active' : ''}`}
              type="button"
              onClick={() => setSelectedTask(task.name)}
            >
              <Group justify="space-between" mb={4}>
                <StatusBadge kind="task" value={task.type} label={taskTypeLabels[task.type] ?? task.type} />
                <Badge color="gray" variant="light">{formatSize(task.size)}</Badge>
              </Group>
              <Text size="sm" fw={500}>{task.label}</Text>
              <Text size="xs" c="dimmed">{formatDateTime(task.mod_time)}</Text>
            </button>
          ))}
        </ScrollArea>
      </Stack>
      <Stack className="taskLogMain" gap="sm">
        {typesQuery.isError ? <ErrorAlert error={typesQuery.error} title="加载任务类型失败" /> : null}
        {logQuery.isError ? <ErrorAlert error={logQuery.error} title="加载任务日志失败" /> : null}
        <Group justify="space-between" gap="sm" className="logsToolbar">
          <Text fw={600}>{selectedTaskMeta?.label ?? selectedTask ?? '请选择任务'}</Text>
          <Group gap="xs">
            <Button variant="light" leftSection={<IconRefresh size={16} />} loading={logQuery.isFetching} disabled={!selectedTask} onClick={() => logQuery.refetch()}>刷新</Button>
            <Button color="red" variant="light" leftSection={<IconTrash size={16} />} loading={clearMutation.isPending} disabled={!selectedTask} onClick={handleClear}>清空日志</Button>
          </Group>
        </Group>
        <LogViewer
          lines={logQuery.data?.lines ?? []}
          truncated={logQuery.data?.truncated}
          loading={Boolean(selectedTask) && (logQuery.isLoading || logQuery.isFetching)}
          emptyText={selectedTask ? '暂无日志内容' : '请从左侧选择一个任务'}
        />
      </Stack>
    </div>
  )
}
