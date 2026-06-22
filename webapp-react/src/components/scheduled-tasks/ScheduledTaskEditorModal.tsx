import { Button, Collapse, Group, Modal, NumberInput, Select, Stack, Switch, Text, TextInput } from '@mantine/core'
import { useEffect, useState } from 'react'
import type { CreateScheduledTaskPayload, ScheduledTaskDefinition, ScheduledTaskItem, ScheduledTaskSchedule, UpdateScheduledTaskPayload } from '@/api/scheduledTasks'
import { ScheduleFields } from './ScheduleFields'
import { buildDefaultParams, ScheduledTaskParamsForm } from './ScheduledTaskParamsForm'

interface ScheduledTaskEditorModalProps {
  opened: boolean
  definitions: ScheduledTaskDefinition[]
  task: ScheduledTaskItem | null
  saving: boolean
  onClose: () => void
  onSubmit: (payload: CreateScheduledTaskPayload | UpdateScheduledTaskPayload) => void
}

function getLocalTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
}

function withLocalTimezone(schedule: ScheduledTaskSchedule) {
  if (schedule.timezone && schedule.timezone !== 'UTC') return schedule
  return { ...schedule, timezone: getLocalTimezone() }
}

const defaultSchedule: ScheduledTaskSchedule = { kind: 'daily', expr: '02:00', timezone: getLocalTimezone() }

export function ScheduledTaskEditorModal({ opened, definitions, task, saving, onClose, onSubmit }: ScheduledTaskEditorModalProps) {
  const [taskType, setTaskType] = useState('')
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [schedule, setSchedule] = useState<ScheduledTaskSchedule>(defaultSchedule)
  const [params, setParams] = useState<Record<string, unknown>>({})
  const [timeoutSeconds, setTimeoutSeconds] = useState(600)
  const [maxRetries, setMaxRetries] = useState(0)
  const [retryDelaySeconds, setRetryDelaySeconds] = useState(60)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const selectedDefinition = definitions.find((item) => item.type === taskType)

  useEffect(() => {
    if (!opened) return
    if (task) {
      setTaskType(task.type)
      setName(task.name)
      setEnabled(task.enabled)
      setSchedule(task.schedule)
      setParams(task.params ?? {})
      setTimeoutSeconds(task.timeout_seconds || 600)
      setMaxRetries(task.max_retries || 0)
      setRetryDelaySeconds(task.retry_delay_seconds || 60)
      setShowAdvanced(false)
      return
    }
    const first = definitions[0]
    setTaskType(first?.type ?? '')
    setName(first?.label ?? '')
    setEnabled(true)
    setSchedule(withLocalTimezone(first?.default_schedule ?? defaultSchedule))
    setParams(buildDefaultParams(first))
    setTimeoutSeconds(600)
    setMaxRetries(0)
    setRetryDelaySeconds(60)
    setShowAdvanced(false)
  }, [definitions, opened, task])

  function handleTaskTypeChange(value: string | null) {
    const nextTaskType = value || ''
    const nextDefinition = definitions.find((item) => item.type === nextTaskType)
    setTaskType(nextTaskType)
    setName(nextDefinition?.label ?? '')
    setSchedule(withLocalTimezone(nextDefinition?.default_schedule ?? defaultSchedule))
    setParams(buildDefaultParams(nextDefinition))
  }

  function handleSubmit() {
    const payload: UpdateScheduledTaskPayload = {
      name,
      enabled,
      schedule,
      params,
      concurrency_policy: task?.concurrency_policy || 'skip',
      missed_policy: task?.missed_policy || 'run_once',
      timeout_seconds: timeoutSeconds,
      max_retries: maxRetries,
      retry_delay_seconds: retryDelaySeconds,
      version: task?.version,
    }
    onSubmit(task ? payload : { ...payload, type: taskType })
  }

  return (
    <Modal opened={opened} onClose={onClose} title={task ? '编辑计划任务' : '新增计划任务'} size="lg" centered>
      <Stack gap="sm">
        <Select
          label="任务类型"
          data={definitions.map((item) => ({ value: item.type, label: item.label || item.type }))}
          value={taskType}
          onChange={handleTaskTypeChange}
          disabled={Boolean(task)}
          placeholder="暂无可用任务类型"
          allowDeselect={false}
        />
        {selectedDefinition?.description ? <Text size="sm" c="dimmed">{selectedDefinition.description}</Text> : null}
        <TextInput label="任务名称" value={name} onChange={(event) => setName(event.currentTarget.value)} />
        <Switch label="启用任务" checked={enabled} onChange={(event) => setEnabled(event.currentTarget.checked)} />
        <ScheduleFields value={schedule} onChange={setSchedule} />
        <ScheduledTaskParamsForm definition={selectedDefinition} value={params} onChange={setParams} />
        <div>
          <Button variant="subtle" px={0} onClick={() => setShowAdvanced((value) => !value)}>
            {showAdvanced ? '收起高级执行设置' : '高级执行设置'}
          </Button>
          <Collapse in={showAdvanced}>
            <Group grow mt="xs">
              <NumberInput label="超时秒数" min={60} value={timeoutSeconds} onChange={(value) => setTimeoutSeconds(Number(value) || 600)} />
              <NumberInput label="最大重试" min={0} max={10} value={maxRetries} onChange={(value) => setMaxRetries(Number(value) || 0)} />
              <NumberInput label="重试延迟秒" min={1} value={retryDelaySeconds} onChange={(value) => setRetryDelaySeconds(Number(value) || 60)} />
            </Group>
          </Collapse>
        </div>
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>取消</Button>
          <Button loading={saving} disabled={!taskType || !name} onClick={handleSubmit}>保存</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
