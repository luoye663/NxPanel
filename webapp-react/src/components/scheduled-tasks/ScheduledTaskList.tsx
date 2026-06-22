import { ActionIcon, Badge, Group, Switch, Table, Text, Tooltip } from '@mantine/core'
import { IconHistory, IconPencil, IconPlayerPlay, IconTrash } from '@tabler/icons-react'
import type { ScheduledTaskDefinition, ScheduledTaskItem } from '@/api/scheduledTasks'
import { formatDateTime } from '@/components/common/TimeCell'
import { formatSchedule, runStatusLabels, statusColor, taskStatusLabels } from './taskTypeRegistry'

interface ScheduledTaskListProps {
  tasks: ScheduledTaskItem[]
  definitions: ScheduledTaskDefinition[]
  togglingId?: string
  onEdit: (task: ScheduledTaskItem) => void
  onToggle: (task: ScheduledTaskItem, enabled: boolean) => void
  onRun: (task: ScheduledTaskItem) => void
  onHistory: (task: ScheduledTaskItem) => void
  onDelete: (task: ScheduledTaskItem) => void
}

export function ScheduledTaskList({ tasks, definitions, togglingId, onEdit, onToggle, onRun, onHistory, onDelete }: ScheduledTaskListProps) {
  function typeLabel(type: string) {
    return definitions.find((item) => item.type === type)?.label ?? type
  }

  if (tasks.length === 0) {
    return <Text c="dimmed" ta="center" py="xl">暂无计划任务，点击右上角新增任务。</Text>
  }

  return (
    <Table striped highlightOnHover withTableBorder>
      <Table.Thead>
        <Table.Tr>
          <Table.Th>任务名称</Table.Th>
          <Table.Th>类型</Table.Th>
          <Table.Th>启用</Table.Th>
          <Table.Th>运行态</Table.Th>
          <Table.Th>周期</Table.Th>
          <Table.Th>下次执行</Table.Th>
          <Table.Th>上次结果</Table.Th>
          <Table.Th>操作</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {tasks.map((task) => (
          <Table.Tr key={task.id}>
            <Table.Td>
              <Group gap="xs">
                <Text fw={600}>{task.name}</Text>
                {task.system ? <Badge color="gray" variant="light">系统</Badge> : null}
              </Group>
            </Table.Td>
            <Table.Td>{typeLabel(task.type)}</Table.Td>
            <Table.Td>
              <Switch checked={task.enabled} disabled={togglingId === task.id} onChange={(event) => onToggle(task, event.currentTarget.checked)} />
            </Table.Td>
            <Table.Td><Badge color={statusColor(task.status)} variant="light">{taskStatusLabels[task.status] ?? task.status}</Badge></Table.Td>
            <Table.Td><Text size="sm">{formatSchedule(task.schedule.kind, task.schedule.expr, task.schedule.timezone)}</Text></Table.Td>
            <Table.Td>{formatDateTime(task.next_run_at)}</Table.Td>
            <Table.Td>
              <StackedResult status={task.last_status} error={task.last_error} />
            </Table.Td>
            <Table.Td>
              <Group gap={4} wrap="nowrap">
                <Tooltip label="立即执行"><ActionIcon variant="subtle" onClick={() => onRun(task)}><IconPlayerPlay size={16} /></ActionIcon></Tooltip>
                <Tooltip label="执行历史"><ActionIcon variant="subtle" onClick={() => onHistory(task)}><IconHistory size={16} /></ActionIcon></Tooltip>
                <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => onEdit(task)}><IconPencil size={16} /></ActionIcon></Tooltip>
                <Tooltip label={task.system ? '系统任务不能删除' : '删除'}><ActionIcon variant="subtle" color="red" disabled={task.system} onClick={() => onDelete(task)}><IconTrash size={16} /></ActionIcon></Tooltip>
              </Group>
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}

function StackedResult({ status, error }: { status?: string | null; error: string }) {
  if (!status) return <Text c="dimmed" size="sm">-</Text>
  return (
    <div>
      <Badge color={statusColor(status)} variant="light">{runStatusLabels[status] ?? status}</Badge>
      {error ? <Text size="xs" c="red" lineClamp={1}>{error}</Text> : null}
    </div>
  )
}
