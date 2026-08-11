import { Alert, Button, Divider, Group, Modal, NumberInput, Select, Stack, Switch, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMediaQuery } from '@mantine/hooks'
import { IconCheck, IconDeviceFloppy } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import type { NginxUpstream, NginxUpstreamSaveRequest, NginxUpstreamServerRequest, NginxUpstreamValidateResult } from '@/api/types'
import { validateUpstream } from '@/api/upstreams'
import { showErrorModal } from '@/utils/errorModal'
import { MembersEditor, emptyUpstreamMember } from './MembersEditor'

interface UpstreamEditorModalProps {
  opened: boolean
  upstream: NginxUpstream | null
  saving: boolean
  onClose: () => void
  onSave: (values: NginxUpstreamSaveRequest) => Promise<void>
}

const algorithmOptions = [
  { value: 'round_robin', label: '轮询 (round robin)' },
  { value: 'least_conn', label: '最少连接 (least_conn)' },
  { value: 'ip_hash', label: '客户端 IP (ip_hash)' },
  { value: 'hash', label: '自定义 Hash' },
]

function initialValues(upstream: NginxUpstream | null): NginxUpstreamSaveRequest {
  return upstream ? {
    name: upstream.name,
    algorithm: upstream.algorithm,
    hash_key: upstream.hash_key,
    consistent: upstream.consistent,
    keepalive: upstream.keepalive,
    keepalive_requests: upstream.keepalive_requests,
    keepalive_timeout_seconds: upstream.keepalive_timeout_seconds,
    advanced_directives: upstream.advanced_directives,
    servers: upstream.servers.map(({ id: _id, ...server }) => server),
  } : {
    name: '', algorithm: 'round_robin', hash_key: '', consistent: false,
    keepalive: 0, keepalive_requests: 0, keepalive_timeout_seconds: 0,
    advanced_directives: '', servers: [emptyUpstreamMember()],
  }
}

function validAddress(address: string): boolean {
  const value = address.trim()
  const match = value.match(/^\[([^\]]+)]:(\d+)$/) || value.match(/^([^:[\]\s]+):(\d+)$/)
  if (!match) return false
  const port = Number(match[2])
  if (port < 1 || port > 65535 || /[;{}"'\\]/.test(value)) return false
  const host = match[1]
  if (value.startsWith('[')) return /^[0-9a-fA-F:.%]+$/.test(host) && host.includes(':')
  if (/^[0-9.]+$/.test(host)) {
    const octets = host.split('.')
    return octets.length === 4 && octets.every((octet) => /^\d{1,3}$/.test(octet) && Number(octet) <= 255)
  }
  return host.length <= 253 && host.split('.').every((label) => /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/.test(label))
}

function memberErrors(servers: NginxUpstreamServerRequest[]): string[] {
  const seen = new Set<string>()
  return servers.map((server) => {
    const address = server.address.trim().toLowerCase()
    if (!validAddress(address)) return '请输入 hostname:port、IPv4:port 或 [IPv6]:port'
    if (seen.has(address)) return '成员地址不能重复'
    seen.add(address)
    if (server.weight < 1 || server.weight > 256) return '权重范围为 1-256'
    if (server.max_fails < 0 || server.max_fails > 100) return '失败次数范围为 0-100'
    if (server.fail_timeout_seconds < 1 || server.fail_timeout_seconds > 3600) return '失败超时范围为 1-3600 秒'
    if (server.sort_order < -100000 || server.sort_order > 100000) return '排序值超出允许范围'
    if (server.backup && server.down) return '成员不能同时设为备用和停用'
    return ''
  })
}

function combinationError(values: NginxUpstreamSaveRequest): string | null {
  if (values.algorithm === 'hash' && !values.hash_key.trim()) return 'Hash 算法必须填写 Hash Key'
  if (values.algorithm !== 'hash' && (values.hash_key.trim() || values.consistent)) return 'Hash Key 和 consistent 仅适用于 Hash 算法'
  if ((values.algorithm === 'ip_hash' || values.algorithm === 'hash') && values.servers.some((server) => server.backup)) return '当前算法不支持备用成员'
  if (!values.servers.some((server) => !server.backup && !server.down)) return '至少需要一个启用的主成员'
  return null
}

function normalized(values: NginxUpstreamSaveRequest): NginxUpstreamSaveRequest {
  return {
    ...values,
    name: values.name?.trim(),
    hash_key: values.algorithm === 'hash' ? values.hash_key.trim() : '',
    consistent: values.algorithm === 'hash' && values.consistent,
    keepalive_requests: values.keepalive > 0 ? values.keepalive_requests : 0,
    keepalive_timeout_seconds: values.keepalive > 0 ? values.keepalive_timeout_seconds : 0,
    advanced_directives: values.advanced_directives.trim(),
    servers: values.servers.map((server) => ({ ...server, address: server.address.trim() })),
  }
}

export function UpstreamEditorModal({ opened, upstream, saving, onClose, onSave }: UpstreamEditorModalProps) {
  const mobile = useMediaQuery('(max-width: 48rem)')
  const [preview, setPreview] = useState<NginxUpstreamValidateResult | null>(null)
  const [validating, setValidating] = useState(false)
  const formVersion = useRef(0)
  const form = useForm<NginxUpstreamSaveRequest>({
    initialValues: initialValues(upstream),
    validate: {
      name: (value) => value && /^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(value.trim()) ? null : '名称需以字母或下划线开头，仅含字母、数字和下划线',
      keepalive: (value) => value >= 0 && value <= 10000 ? null : '范围为 0-10000',
      keepalive_requests: (value) => value >= 0 && value <= 1000000 ? null : '范围为 0-1000000',
      keepalive_timeout_seconds: (value) => value >= 0 && value <= 3600 ? null : '范围为 0-3600 秒',
    },
  })

  useEffect(() => {
    if (!opened) return
    form.setValues(initialValues(upstream))
    form.clearErrors()
    setPreview(null)
  }, [opened, upstream])

  useEffect(() => {
    formVersion.current += 1
    setPreview(null)
  }, [form.values])

  const serverErrors = memberErrors(form.values.servers)
  const comboError = combinationError(form.values)

  function canSubmit(): boolean {
    const result = form.validate()
    return !result.hasErrors && serverErrors.every((error) => !error) && !comboError
  }

  async function handleValidate() {
    if (!canSubmit()) return
    setValidating(true)
    const version = formVersion.current
    try {
      const result = await validateUpstream(normalized(form.values))
      if (version === formVersion.current) setPreview(result)
    } catch (error) {
      showErrorModal(error, '校验上游配置失败')
    } finally {
      setValidating(false)
    }
  }

  async function handleSubmit(values: NginxUpstreamSaveRequest) {
    if (serverErrors.some(Boolean) || comboError) return
    await onSave(normalized(values))
  }

  return (
    <Modal opened={opened} onClose={onClose} title={upstream ? '编辑上游组' : '新建上游组'} size="xl" fullScreen={mobile} closeOnClickOutside={false} centered>
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <div className="upstreamBasicGrid">
            <TextInput label="配置名" placeholder="backend_api" disabled={Boolean(upstream)} {...form.getInputProps('name')} />
            <Select label="负载均衡策略" data={algorithmOptions} allowDeselect={false} {...form.getInputProps('algorithm')} onChange={(value) => {
              const algorithm = (value || 'round_robin') as NginxUpstreamSaveRequest['algorithm']
              form.setFieldValue('algorithm', algorithm)
              if (algorithm !== 'hash') {
                form.setFieldValue('hash_key', '')
                form.setFieldValue('consistent', false)
              }
            }} />
          </div>
          {form.values.algorithm === 'hash' ? (
            <Group grow align="flex-start" className="upstreamResponsiveGroup">
              <TextInput label="Hash Key" placeholder="$request_uri" {...form.getInputProps('hash_key')} />
              <Switch mt={28} label="consistent" {...form.getInputProps('consistent', { type: 'checkbox' })} />
            </Group>
          ) : null}
          {comboError ? <Alert color="red">{comboError}</Alert> : null}

          <Divider label="成员配置" labelPosition="left" />
          <MembersEditor members={form.values.servers} errors={serverErrors} onChange={(servers) => { form.setFieldValue('servers', servers); setPreview(null) }} />

          <Divider label="Keepalive" labelPosition="left" />
          <div className="upstreamKeepaliveGrid">
            <NumberInput label="连接数" min={0} max={10000} allowDecimal={false} {...form.getInputProps('keepalive')} onChange={(value) => {
              const keepalive = Number(value) || 0
              form.setFieldValue('keepalive', keepalive)
              if (keepalive === 0) {
                form.setFieldValue('keepalive_requests', 0)
                form.setFieldValue('keepalive_timeout_seconds', 0)
              }
            }} />
            <NumberInput label="单连接请求数" min={0} max={1000000} allowDecimal={false} disabled={form.values.keepalive === 0} {...form.getInputProps('keepalive_requests')} />
            <NumberInput label="连接超时（秒）" min={0} max={3600} allowDecimal={false} disabled={form.values.keepalive === 0} {...form.getInputProps('keepalive_timeout_seconds')} />
          </div>

          <Divider label="高级指令" labelPosition="left" />
          <Textarea className="upstreamDirectiveEditor" autosize minRows={5} maxRows={12} placeholder={'zone backend 64k;\nqueue 100 timeout=30s;'} {...form.getInputProps('advanced_directives')} />
          {preview ? (
            <Stack gap={6}>
              <Text size="sm" fw={500}>后端渲染预览</Text>
              <Textarea className="upstreamDirectiveEditor" readOnly autosize minRows={8} maxRows={16} value={preview.rendered_block} />
            </Stack>
          ) : null}
          <Group justify="space-between" className="upstreamModalActions">
            <Button variant="light" leftSection={<IconCheck size={17} />} loading={validating} onClick={handleValidate}>校验并预览</Button>
            <Group className="upstreamModalSubmit">
              <Button variant="default" onClick={onClose}>取消</Button>
              <Button type="submit" loading={saving} leftSection={<IconDeviceFloppy size={17} />}>保存</Button>
            </Group>
          </Group>
        </Stack>
      </form>
    </Modal>
  )
}
