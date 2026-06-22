import { Button, Group, NumberInput, Select, SimpleGrid, Stack, Switch, TextInput } from '@mantine/core'
import { useEffect } from 'react'
import { useForm } from '@mantine/form'
import { SectionCard } from '@/components/common/SectionCard'
import type { AccessAnalysisSettings } from '@/api/types'

export function AccessAnalysisSettingsPanel({ settings, saving, onSave, embedded }: { settings?: AccessAnalysisSettings; saving: boolean; onSave: (settings: AccessAnalysisSettings) => void; embedded?: boolean }) {
  const form = useForm<AccessAnalysisSettings>({ initialValues: settings || defaultSettings('') })
  useEffect(() => { if (settings) form.setValues(settings) }, [settings])
  const content = (
    <form onSubmit={form.onSubmit(onSave)}>
      <Stack gap="md">
        <Group>
          <Switch label="启用定时扫描" {...form.getInputProps('enabled', { type: 'checkbox' })} />
        </Group>
        <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="md">
          <TextInput label="每天扫描时间" placeholder="03:00" {...form.getInputProps('scan_time')} />
          <NumberInput label="保留天数" min={1} max={365} {...form.getInputProps('retention_days')} />
          <Select label="日志格式" data={[{ value: 'common', label: 'common' }, { value: 'combined', label: 'combined' }, { value: 'nxpanel_json', label: 'nxpanel_json' }, { value: 'custom', label: '自定义正则' }]} {...form.getInputProps('log_format')} />
        </SimpleGrid>
        <Group gap="sm">
          <Switch label="扫描切割日志" {...form.getInputProps('include_rotated', { type: 'checkbox' })} />
          <Switch label="合并 query string" {...form.getInputProps('normalize_query', { type: 'checkbox' })} />
          <Switch label="保存访问明细样本" {...form.getInputProps('save_entries', { type: 'checkbox' })} />
        </Group>
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <NumberInput label="明细保留天数" min={1} max={30} disabled={!form.values.save_entries} {...form.getInputProps('entries_retention_days')} />
          <NumberInput label="每站点最大明细数" min={1000} max={500000} step={1000} disabled={!form.values.save_entries} {...form.getInputProps('max_entries')} />
          <NumberInput label="每日路径 Top N" min={100} max={50000} step={100} {...form.getInputProps('path_top_n')} />
          <NumberInput label="每日 IP Top N" min={100} max={50000} step={100} {...form.getInputProps('ip_top_n')} />
        </SimpleGrid>
        {form.values.log_format === 'custom' ? <TextInput label="自定义正则" placeholder="使用 (?P<ip>...) 命名捕获字段" {...form.getInputProps('custom_pattern')} /> : null}
        <Group justify="flex-end"><Button type="submit" loading={saving}>保存设置</Button></Group>
      </Stack>
    </form>
  )
  if (embedded) return content
  return (
    <SectionCard title="定时与保留" description="每个站点独立设置扫描时间、保留天数和 query string 归一化策略。">
      {content}
    </SectionCard>
  )
}

function defaultSettings(siteId: string): AccessAnalysisSettings {
  return { site_id: siteId, enabled: false, scan_time: '03:00', retention_days: 30, include_rotated: false, log_format: 'combined', custom_pattern: '', normalize_query: false, save_entries: false, entries_retention_days: 3, max_entries: 50000, path_top_n: 1000, ip_top_n: 1000, created_at: '', updated_at: '' }
}
