import { Badge, Drawer, Group, Loader, ScrollArea, Stack, Table, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import type { ScheduledTaskItem } from '@/api/scheduledTasks'
import { getScheduledTaskRuns } from '@/api/scheduledTasks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { formatDateTime } from '@/components/common/TimeCell'
import { runStatusLabels, statusColor, triggerLabels } from './taskTypeRegistry'

interface ScheduledTaskRunDrawerProps {
  opened: boolean
  task: ScheduledTaskItem | null
  onClose: () => void
}

export function ScheduledTaskRunDrawer({ opened, task, onClose }: ScheduledTaskRunDrawerProps) {
  const runsQuery = useQuery({
    queryKey: ['scheduled-task-runs', task?.id],
    queryFn: () => getScheduledTaskRuns(task!.id),
    enabled: opened && Boolean(task),
  })
  const runs = runsQuery.data?.items ?? []

  return (
    <Drawer opened={opened} onClose={onClose} title={task ? `执行历史：${task.name}` : '执行历史'} size="xl" position="right">
      <Stack gap="sm">
        {runsQuery.isError ? <ErrorAlert error={runsQuery.error} title="加载执行历史失败" /> : null}
        {runsQuery.isLoading ? <Group justify="center" py="xl"><Loader size="sm" /></Group> : null}
        {!runsQuery.isLoading && runs.length === 0 ? <Text c="dimmed" ta="center" py="xl">暂无执行历史</Text> : null}
        {runs.length > 0 ? (
          <ScrollArea>
            <Table striped highlightOnHover withTableBorder>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>触发</Table.Th>
                  <Table.Th>状态</Table.Th>
                  <Table.Th>开始时间</Table.Th>
                  <Table.Th>耗时</Table.Th>
                  <Table.Th>错误摘要</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {runs.map((run) => (
                  <Table.Tr key={run.id}>
                    <Table.Td>{triggerLabels[run.trigger] ?? run.trigger}</Table.Td>
                    <Table.Td><Badge color={statusColor(run.status)} variant="light">{runStatusLabels[run.status] ?? run.status}</Badge></Table.Td>
                    <Table.Td>{formatDateTime(run.started_at)}</Table.Td>
                    <Table.Td>{run.duration_ms} ms</Table.Td>
                    <Table.Td><Text size="sm" lineClamp={2}>{run.error_message || '-'}</Text></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </ScrollArea>
        ) : null}
      </Stack>
    </Drawer>
  )
}
