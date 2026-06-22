import { Badge, Button, Checkbox, Divider, Group, NumberInput, Select, SimpleGrid, Slider, Stack, Switch, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy, IconRefresh, IconRestore, IconInfoCircle } from '@tabler/icons-react'
import { useEffect, useMemo } from 'react'
import { saveNginxParameters } from '@/api/nginx'
import { queryKeys, useNginxParameters } from '@/api/hooks'
import type { NginxParameterValue } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'

const switchParams = new Set(['gzip', 'gzip_vary', 'sendfile', 'tcp_nopush', 'tcp_nodelay', 'server_tokens', 'multi_accept', 'reset_timedout_connection', 'lingering_close', 'ssl_prefer_server_ciphers', 'ssl_stapling', 'log_not_found', 'log_subrequest', 'server_name_in_redirect', 'ignore_invalid_headers', 'underscores_in_headers'])
const numberParams = new Set(['worker_connections', 'keepalive_timeout', 'gzip_min_length', 'gzip_comp_level', 'server_names_hash_bucket_size', 'send_timeout', 'proxy_connect_timeout', 'proxy_read_timeout', 'proxy_send_timeout', 'worker_rlimit_nofile', 'lingering_timeout', 'ssl_session_timeout'])
const checkboxParams = new Set(['ssl_protocols'])
const selectParams = new Set(['use'])

function normalizeValue(param: NginxParameterValue): string {
  return param.key === 'use' && param.value === '' ? '__auto__' : param.value
}

function numericLimit(key: string) {
  if (key === 'worker_connections') return { max: 65535, step: 256 }
  if (key === 'worker_rlimit_nofile') return { max: 1048576, step: 1024 }
  if (key === 'server_names_hash_bucket_size') return { max: 1024, step: 32 }
  if (key === 'gzip_min_length') return { max: 10240, step: 1 }
  if (key === 'ssl_session_timeout') return { max: 86400, step: 1 }
  return { max: 600, step: 1 }
}

export function NginxParametersTab({ active }: { active: boolean }) {
  const queryClient = useQueryClient()
  const paramsQuery = useNginxParameters(active)
  const saveMutation = useMutation({ mutationFn: saveNginxParameters })
  const form = useForm<{ parameters: Record<string, string> }>({ initialValues: { parameters: {} } })

  useEffect(() => {
    if (!paramsQuery.data) return
    form.setValues({ parameters: Object.fromEntries(paramsQuery.data.parameters.map((param) => [param.key, normalizeValue(param)])) })
    form.resetDirty()
    // 参数来自服务端解析结果，切换 tab 后重置 dirty 状态，避免误提示未保存。
  }, [paramsQuery.data])

  const groupedParams = useMemo(() => {
    const groups = new Map<string, NginxParameterValue[]>()
    for (const param of paramsQuery.data?.parameters ?? []) {
      const group = param.group || '其他'
      groups.set(group, [...(groups.get(group) ?? []), param])
    }
    return Array.from(groups.entries()).map(([name, params]) => ({ name, params }))
  }, [paramsQuery.data?.parameters])

  function setValue(key: string, value: string) {
    form.setFieldValue(`parameters.${key}`, value)
  }

  function toSavePayload() {
    const payload: Record<string, string> = {}
    for (const param of paramsQuery.data?.parameters ?? []) {
      const value = form.values.parameters[param.key] ?? ''
      if (value === '__auto__') payload[param.key] = ''
      else if (value === '' && param.clearable) payload[param.key] = '__clear__'
      else payload[param.key] = value
    }
    return payload
  }

  function handleSave() {
    confirmDanger({
      title: '保存常用参数',
      message: '修改 Nginx 常用参数将直接修改 nginx.conf，并自动执行 nginx -t 验证与 reload。确认保存？',
      confirmLabel: '确认保存',
      errorTitle: '保存常用参数失败',
      onConfirm: async () => {
        await saveMutation.mutateAsync({ parameters: toSavePayload() })
        notifySuccess({ message: '常用参数保存成功，Nginx 已 reload' })
        await queryClient.invalidateQueries({ queryKey: queryKeys.nginxParameters })
        await queryClient.invalidateQueries({ queryKey: queryKeys.systemOverview })
      },
    })
  }

  function renderControl(param: NginxParameterValue) {
    const value = form.values.parameters[param.key] ?? normalizeValue(param)
    if (switchParams.has(param.key)) {
      return <Switch checked={value === 'on'} onChange={(event) => setValue(param.key, event.currentTarget.checked ? 'on' : 'off')} onLabel="ON" offLabel="OFF" />
    }
    if (checkboxParams.has(param.key)) {
      return <Checkbox.Group value={value ? value.split(/\s+/) : []} onChange={(values) => setValue(param.key, values.join(' '))}>{(param.options ?? []).map((option) => <Checkbox key={option} value={option} label={option} />)}</Checkbox.Group>
    }
    if (selectParams.has(param.key)) {
      return <Select w={280} value={value} onChange={(next) => setValue(param.key, next || '__auto__')} data={[{ value: '__auto__', label: 'auto（自动选择最佳模型）' }, ...(param.options ?? []).map((option) => ({ value: option, label: option }))]} />
    }
    if (param.key === 'gzip_comp_level') {
      return <Slider w={280} min={1} max={9} step={1} value={Number.parseInt(value || '0', 10) || 0} onChange={(next) => setValue(param.key, String(next))} />
    }
    if (numberParams.has(param.key)) {
      const limits = numericLimit(param.key)
      return <NumberInput w={180} min={1} max={limits.max} step={limits.step} value={Number.parseInt(value || '0', 10) || 0} onChange={(next) => setValue(param.key, String(next || 0))} />
    }
    return <TextInput w={280} value={value} placeholder={param.clearable ? '留空使用 nginx 默认值' : ''} onChange={(event) => setValue(param.key, event.currentTarget.value)} />
  }

  return (
    <Stack gap="sm">
      {paramsQuery.data?.conf_path ? <Group gap="xs"><Text size="sm" c="dimmed">主配置：</Text><MonoText value={paramsQuery.data.conf_path} maxWidth="100%" /></Group> : null}
      {paramsQuery.isError ? <ErrorAlert error={paramsQuery.error} title="加载常用参数失败" /> : null}
      {groupedParams.map((group, index) => (
        <SectionCard key={group.name} title={group.name}>
          {index > 0 ? <Divider mb="sm" /> : null}
          <SimpleGrid className="nginxParamList" cols={{ base: 1, lg: 2 }} spacing="sm">
            {group.params.map((param) => {
              const value = form.values.parameters[param.key] ?? normalizeValue(param)
              const modified = value !== param.default_value && value !== '__auto__'
              return (
                <div key={param.key} className="nginxParamRow">
                  <div className="nginxParamMeta">
                    <Group gap={5} wrap="nowrap">
                      <Text fw={600} size="sm" truncate>{param.key}</Text>
                      <Tooltip label={param.tooltip || param.description} multiline maw={360} openDelay={200}><IconInfoCircle size={15} color="var(--mantine-color-gray-6)" /></Tooltip>
                      {param.unit ? <Badge size="xs" color="blue" variant="light">{param.unit}</Badge> : null}
                      {modified ? <Badge size="xs" color="yellow" variant="light">已修改</Badge> : null}
                    </Group>
                    <Text size="xs" c="dimmed" lineClamp={2}>{param.description}</Text>
                  </div>
                  <div className="nginxParamControl">
                    {renderControl(param)}
                    {modified ? <Button size="compact-xs" variant="subtle" leftSection={<IconRestore size={14} />} onClick={() => setValue(param.key, param.key === 'use' ? '__auto__' : param.default_value)}>恢复默认</Button> : null}
                  </div>
                </div>
              )
            })}
          </SimpleGrid>
        </SectionCard>
      ))}
      <Group justify="flex-end">
        <Button variant="light" leftSection={<IconRefresh size={16} />} loading={paramsQuery.isFetching} onClick={() => paramsQuery.refetch()}>刷新参数</Button>
        <Button leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending} disabled={!paramsQuery.data} onClick={handleSave}>保存参数并 Reload</Button>
      </Group>
    </Stack>
  )
}
