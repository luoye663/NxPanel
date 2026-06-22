import { ActionIcon, Group, Popover, Select, Stack, Text, TextInput } from '@mantine/core'
import { IconInfoCircle } from '@tabler/icons-react'
import type { ScheduledTaskSchedule } from '@/api/scheduledTasks'
import { scheduleKindOptions } from './taskTypeRegistry'

interface ScheduleFieldsProps {
  value: ScheduledTaskSchedule
  onChange: (value: ScheduledTaskSchedule) => void
}

const placeholders: Record<string, string> = {
  daily: '02:00',
  weekly: '1 02:00',
  monthly: '1 02:00',
  interval: '1h',
  cron: '0 2 * * *',
}

const timezoneOptions = [
  { value: 'Asia/Shanghai', label: 'Asia/Shanghai (北京时间)' },
  { value: 'Asia/Hong_Kong', label: 'Asia/Hong_Kong' },
  { value: 'Asia/Tokyo', label: 'Asia/Tokyo' },
  { value: 'Asia/Singapore', label: 'Asia/Singapore' },
  { value: 'UTC', label: 'UTC' },
  { value: 'Europe/London', label: 'Europe/London' },
  { value: 'Europe/Berlin', label: 'Europe/Berlin' },
  { value: 'America/New_York', label: 'America/New_York' },
  { value: 'America/Los_Angeles', label: 'America/Los_Angeles' },
]

function withCurrentTimezoneOptions(timezone: string) {
  if (!timezone || timezoneOptions.some((item) => item.value === timezone)) return timezoneOptions
  return [{ value: timezone, label: `${timezone} (当前)` }, ...timezoneOptions]
}

export function ScheduleFields({ value, onChange }: ScheduleFieldsProps) {
  function patch(next: Partial<ScheduledTaskSchedule>) {
    onChange({ ...value, ...next })
  }

  return (
    <Group grow align="flex-start">
      <Select
        label="周期类型"
        data={scheduleKindOptions}
        value={value.kind}
        onChange={(kind) => patch({ kind: kind || 'daily', expr: placeholders[kind || 'daily'] })}
        allowDeselect={false}
      />
      <TextInput
        label={(
          <Group gap={6} wrap="nowrap">
            <Text span size="sm" fw={500}>周期表达式</Text>
            <Popover width={260} position="top" withArrow shadow="md">
              <Popover.Target>
                <ActionIcon variant="subtle" color="gray" aria-label="周期表达式说明">
                  <IconInfoCircle size={15} />
                </ActionIcon>
              </Popover.Target>
              <Popover.Dropdown>
                <Stack gap={4}>
                  <Text size="xs">每日：<Text span ff="monospace">HH:mm</Text></Text>
                  <Text size="xs">每周：<Text span ff="monospace">weekday HH:mm</Text></Text>
                  <Text size="xs">每月：<Text span ff="monospace">day HH:mm</Text></Text>
                  <Text size="xs">间隔：<Text span ff="monospace">1h</Text> / <Text span ff="monospace">30m</Text></Text>
                  <Text size="xs">cron：5 段表达式</Text>
                </Stack>
              </Popover.Dropdown>
            </Popover>
          </Group>
        )}
        placeholder={placeholders[value.kind] ?? '0 2 * * *'}
        value={value.expr}
        onChange={(event) => patch({ expr: event.currentTarget.value })}
      />
      <Select
        label="时区"
        placeholder="选择时区"
        data={withCurrentTimezoneOptions(value.timezone)}
        searchable
        allowDeselect={false}
        value={value.timezone}
        onChange={(timezone) => patch({ timezone: timezone || 'Asia/Shanghai' })}
      />
    </Group>
  )
}
