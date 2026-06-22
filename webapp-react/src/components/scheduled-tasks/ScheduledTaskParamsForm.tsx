import { MultiSelect, NumberInput, Select, Stack, Textarea, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { getSites } from '@/api/sites'
import type { ScheduledTaskDefinition } from '@/api/scheduledTasks'

interface ScheduledTaskParamsFormProps {
  definition?: ScheduledTaskDefinition
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
}

interface ParamSchemaField {
  type?: string
  required?: boolean
  label?: string
  default?: unknown
  min?: number
  max?: number
  options?: string[]
}

const optionLabels: Record<string, Record<string, string>> = {
  backup_type: {
    config: '配置备份',
    root: '根目录备份',
    ssl: '证书备份',
    full: '完整备份',
  },
  range: {
    today: '今天',
    yesterday: '昨天',
    '7d': '最近 7 天',
    custom: '自定义',
  },
}

function asFieldSchema(value: unknown): ParamSchemaField {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return value as ParamSchemaField
}

function asString(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function asStringArray(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function splitStringArray(value: string) {
  return value.split(/[\n,]+/).map((item) => item.trim()).filter(Boolean)
}

function fieldLabel(name: string, schema: ParamSchemaField) {
  return `${schema.label || name}${schema.required ? ' *' : ''}`
}

export function buildDefaultParams(definition?: ScheduledTaskDefinition) {
  const params: Record<string, unknown> = {}
  for (const [name, rawSchema] of Object.entries(definition?.param_schema ?? {})) {
    const schema = asFieldSchema(rawSchema)
    if (schema.default !== undefined) {
      params[name] = schema.default
      continue
    }
    if (schema.type === 'number') params[name] = 0
    if (schema.type === 'string_array') params[name] = []
    if (schema.type === 'select') params[name] = schema.options?.[0] ?? ''
    if (!schema.type || schema.type === 'string') params[name] = ''
  }
  return params
}

export function ScheduledTaskParamsForm({ definition, value, onChange }: ScheduledTaskParamsFormProps) {
  const sitesQuery = useQuery({
    queryKey: ['sites', 'scheduled-task-params'],
    queryFn: () => getSites({ page: 1, page_size: 500 }),
    enabled: Object.keys(definition?.param_schema ?? {}).some((name) => name === 'site_id' || name === 'site_ids'),
  })
  const siteOptions = (sitesQuery.data?.items ?? []).map((site) => ({ value: site.id, label: site.primary_domain || site.id }))

  function patch(name: string, next: unknown) {
    onChange({ ...value, [name]: next })
  }

  const fields = Object.entries(definition?.param_schema ?? {})

  return (
    <Stack gap="sm">
      {fields.map(([name, rawSchema]) => {
        const schema = asFieldSchema(rawSchema)
        const label = fieldLabel(name, schema)

        if ((name === 'from' || name === 'to') && value.range !== 'custom') return null

        if (name === 'site_id') {
          return (
            <Select
              key={name}
              label={label}
              placeholder="选择站点"
              data={siteOptions}
              value={asString(value[name]) || null}
              searchable
              clearable={!schema.required}
              disabled={sitesQuery.isLoading}
              onChange={(next) => patch(name, next || '')}
            />
          )
        }

        if (name === 'site_ids') {
          return (
            <MultiSelect
              key={name}
              label={label}
              placeholder="不选择则扫描全部站点"
              data={siteOptions}
              value={asStringArray(value[name])}
              searchable
              clearable
              disabled={sitesQuery.isLoading}
              onChange={(next) => patch(name, next)}
            />
          )
        }

        if (schema.type === 'select') {
          const options = schema.options ?? []
          return (
            <Select
              key={name}
              label={label}
              data={options.map((option) => ({ value: option, label: optionLabels[name]?.[option] || option }))}
              value={asString(value[name]) || null}
              allowDeselect={!schema.required}
              onChange={(next) => patch(name, next || '')}
            />
          )
        }

        if (schema.type === 'number') {
          return (
            <NumberInput
              key={name}
              label={label}
              min={schema.min}
              max={schema.max}
              value={typeof value[name] === 'number' ? value[name] : Number(value[name]) || 0}
              onChange={(next) => patch(name, Number(next) || 0)}
            />
          )
        }

        if (schema.type === 'string_array') {
          return (
            <Textarea
              key={name}
              label={label}
              description="多个值可用换行或逗号分隔"
              autosize
              minRows={2}
              value={asStringArray(value[name]).join('\n')}
              onChange={(event) => patch(name, splitStringArray(event.currentTarget.value))}
            />
          )
        }

        return (
          <TextInput
            key={name}
            label={label}
            placeholder={name === 'from' || name === 'to' ? 'YYYY-MM-DD' : undefined}
            value={asString(value[name])}
            onChange={(event) => patch(name, event.currentTarget.value)}
          />
        )
      })}
    </Stack>
  )
}
