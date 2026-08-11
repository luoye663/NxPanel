import { ActionIcon, Alert, Badge, Button, Divider, Group, Loader, Modal, NumberInput, Radio, SegmentedControl, Select, Stack, Switch, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure, useMediaQuery } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEdit, IconPlus, IconRefresh, IconServer, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { lazy, Suspense, useState } from 'react'
import { ApiError } from '@/api/client'
import { createProxy, deleteProxy, listProxies, syncProxy, updateProxy } from '@/api/proxy'
import type { CreateProxyRequest, NginxUpstream, SiteDetail, SiteProxy } from '@/api/types'
import { listUpstreams, upstreamKeys } from '@/api/upstreams'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { DataTable } from '@/components/tables/DataTable'
import { AuthAccountManager, AuthAccountSelector } from './AuthAccountManager'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteProxyTabProps { site: SiteDetail }
type ProxyFormValues = CreateProxyRequest & { target_mode: 'direct' | 'managed' }

const UpstreamManagerModal = lazy(() => import('@/components/nginx/UpstreamManagerModal'))

const defaultProxyForm: ProxyFormValues = {
  name: '', enabled: true, location_path: '/', target_mode: 'direct', upstream_url: 'http://127.0.0.1:3000',
  upstream_id: null, upstream_scheme: 'http', proxy_ssl_server_name: '', proxy_ssl_verify: true,
   proxy_ssl_trusted_certificate: '', proxy_ssl_verify_depth: 3, host_header: '$host', websocket_enabled: false,
  connect_timeout: 60, send_timeout: 60, read_timeout: 60, cache_enabled: false, cache_type: 'nginx',
  cache_time: 60, auth_enabled: false, auth_account_ids: [],
}

function isProxyMarkerMissing(site: SiteDetail): boolean {
  return (site.marker_status?.missing || []).includes('MAIN-LOCATION')
}

function toProxyForm(proxy: SiteProxy): ProxyFormValues {
  return {
    name: proxy.name, enabled: proxy.enabled, location_path: proxy.location_path,
    target_mode: proxy.upstream_id ? 'managed' : 'direct', upstream_url: proxy.upstream_id ? '' : proxy.upstream_url,
    upstream_id: proxy.upstream_id, upstream_scheme: proxy.upstream_scheme || 'http',
    proxy_ssl_server_name: proxy.proxy_ssl_server_name || '', proxy_ssl_verify: proxy.proxy_ssl_verify,
     proxy_ssl_trusted_certificate: proxy.proxy_ssl_trusted_certificate || '', proxy_ssl_verify_depth: proxy.proxy_ssl_verify_depth || 3,
    host_header: proxy.host_header, websocket_enabled: proxy.websocket_enabled,
    connect_timeout: proxy.connect_timeout, send_timeout: proxy.send_timeout, read_timeout: proxy.read_timeout,
    cache_enabled: proxy.cache_enabled, cache_type: proxy.cache_type, cache_time: proxy.cache_time,
    auth_enabled: proxy.auth_enabled, auth_account_ids: proxy.auth_account_ids || [],
  }
}

function toProxyRequest(values: ProxyFormValues): CreateProxyRequest {
  const common = {
    name: values.name.trim(), enabled: values.enabled, location_path: values.location_path.trim(),
    host_header: values.host_header.trim(), websocket_enabled: values.websocket_enabled,
    connect_timeout: values.connect_timeout, send_timeout: values.send_timeout, read_timeout: values.read_timeout,
    cache_enabled: values.cache_enabled, cache_type: values.cache_type, cache_time: values.cache_time,
    auth_enabled: values.auth_enabled, auth_account_ids: values.auth_enabled ? values.auth_account_ids : [],
  }
  if (values.target_mode === 'direct') {
    return {
      ...common, upstream_url: values.upstream_url.trim(), upstream_id: null, upstream_scheme: 'http',
      proxy_ssl_server_name: '', proxy_ssl_verify: undefined, proxy_ssl_trusted_certificate: '', proxy_ssl_verify_depth: 0,
    }
  }
  const https = values.upstream_scheme === 'https'
  const verify = https && values.proxy_ssl_verify !== false
  return {
    ...common, upstream_url: '', upstream_id: values.upstream_id, upstream_scheme: values.upstream_scheme,
    proxy_ssl_server_name: https ? values.proxy_ssl_server_name.trim() : '', proxy_ssl_verify: https ? verify : undefined,
    proxy_ssl_trusted_certificate: verify ? values.proxy_ssl_trusted_certificate.trim() : '',
    proxy_ssl_verify_depth: verify ? values.proxy_ssl_verify_depth : 0,
  }
}

function availableMembers(upstream: NginxUpstream): number {
  return upstream.servers.filter((server) => !server.down).length
}

function validTLSServerName(value: string): boolean {
  const name = value.trim()
  if (!name || /[\0\n\r;{}"'$\s]/.test(name)) return false
  if (name.includes(':')) return /^[0-9a-fA-F:.%]+$/.test(name) && name.split(':').length >= 3
  if (/^[0-9.]+$/.test(name)) {
    const octets = name.split('.')
    return octets.length === 4 && octets.every((octet) => /^\d{1,3}$/.test(octet) && Number(octet) <= 255)
  }
  return name.length <= 253 && name.split('.').every((label) => /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/.test(label))
}

function validCAPath(value: string): boolean {
  return value.startsWith('/') && !/[\0\n\r\t ;{}"'#\\]/.test(value)
}

export function SiteProxyTab({ site }: SiteProxyTabProps) {
  const mobile = useMediaQuery('(max-width: 48rem)')
  const queryClient = useQueryClient()
  const [opened, handlers] = useDisclosure(false)
  const [accountManagerOpened, accountManagerHandlers] = useDisclosure(false)
  const [upstreamManagerOpened, upstreamManagerHandlers] = useDisclosure(false)
  const [editingProxy, setEditingProxy] = useState<SiteProxy | null>(null)
  const proxyQueryKey = ['site-detail', site.id, 'proxy'] as const
  const proxyQuery = useQuery({ queryKey: proxyQueryKey, queryFn: () => listProxies(site.id) })
  const upstreamQuery = useQuery({ queryKey: upstreamKeys.all, queryFn: listUpstreams })
  const saveMutation = useMutation({ mutationFn: (values: ProxyFormValues) => editingProxy ? updateProxy(site.id, editingProxy.id, toProxyRequest(values)) : createProxy(site.id, toProxyRequest(values)) })
  const toggleMutation = useMutation({ mutationFn: ({ proxy, enabled }: { proxy: SiteProxy; enabled: boolean }) => updateProxy(site.id, proxy.id, toProxyRequest({ ...toProxyForm(proxy), enabled })) })
  const deleteMutation = useMutation({ mutationFn: (proxyId: string) => deleteProxy(site.id, proxyId) })
  const syncMutation = useMutation({ mutationFn: () => syncProxy(site.id) })
  const markerMissing = isProxyMarkerMissing(site)
  const form = useForm<ProxyFormValues>({
    initialValues: defaultProxyForm,
    validate: {
      name: (value) => value.trim() ? null : '请输入代理名称',
      location_path: (value) => value.trim().startsWith('/') ? null : '代理路径必须以 / 开头',
      upstream_url: (value, values) => values.target_mode === 'direct' && !value.trim() ? '请输入目标 URL' : null,
      upstream_id: (value, values) => values.target_mode === 'managed' && !value ? '请选择上游组' : null,
      proxy_ssl_server_name: (value, values) => values.target_mode !== 'managed' || values.upstream_scheme !== 'https' || validTLSServerName(value) ? null : '请输入不含端口的 hostname 或 IP',
      proxy_ssl_trusted_certificate: (value, values) => {
        if (values.target_mode !== 'managed' || values.upstream_scheme !== 'https' || !values.proxy_ssl_verify) return null
        if (!value.trim()) return '开启证书验证时必须填写 CA 证书路径'
        return validCAPath(value) ? null : 'CA 路径必须是无空格和特殊字符的绝对路径'
      },
      proxy_ssl_verify_depth: (value, values) => values.target_mode !== 'managed' || values.upstream_scheme !== 'https' || !values.proxy_ssl_verify || (value >= 1 && value <= 100) ? null : '验证深度范围为 1-100',
      host_header: (value) => value.trim() ? null : '请输入 Host 域名',
      connect_timeout: (value, values) => !values.websocket_enabled || (value >= 1 && value <= 3600) ? null : '连接超时范围为 1-3600 秒',
      send_timeout: (value, values) => !values.websocket_enabled || (value >= 1 && value <= 3600) ? null : '发送超时范围为 1-3600 秒',
      read_timeout: (value, values) => !values.websocket_enabled || (value >= 1 && value <= 3600) ? null : '读取超时范围为 1-3600 秒',
      cache_time: (value, values) => !values.cache_enabled || (value >= 1 && value <= 10080) ? null : '缓存时间范围为 1-10080 分钟',
      auth_account_ids: (value, values) => !values.auth_enabled || (value && value.length > 0) ? null : '请选择至少一个账户',
    },
  })

  const upstreamById = new Map((upstreamQuery.data || []).map((item) => [item.id, item]))
  const columns: MRT_ColumnDef<SiteProxy>[] = [
    { accessorKey: 'name', header: '名称', size: 140 },
    { accessorKey: 'location_path', header: '代理路径', size: 120, Cell: ({ cell }) => <MonoText value={cell.getValue<string>()} maxWidth={160} /> },
    { id: 'target', header: '目标', Cell: ({ row }) => {
      const upstream = row.original.upstream_id ? upstreamById.get(row.original.upstream_id) : null
      const value = upstream ? `${row.original.upstream_scheme}://${upstream.name}` : row.original.upstream_url
      return <MonoText value={value} maxWidth={280} />
    } },
    { accessorKey: 'cache_enabled', header: '缓存', size: 90, Cell: ({ row }) => <Badge color={row.original.cache_enabled ? 'green' : 'gray'} variant="light">{row.original.cache_enabled ? '是' : '否'}</Badge> },
    { accessorKey: 'auth_enabled', header: '访问限制', size: 100, Cell: ({ row }) => <Badge color={row.original.auth_enabled ? 'blue' : 'gray'} variant="light">{row.original.auth_enabled ? `${row.original.auth_account_ids?.length || 0} 个账户` : '关闭'}</Badge> },
    { accessorKey: 'enabled', header: '状态', size: 90, Cell: ({ row }) => <Switch checked={row.original.enabled} disabled={toggleMutation.isPending} onChange={(event) => handleToggle(row.original, event.currentTarget.checked)} /> },
  ]

  function openCreate() { setEditingProxy(null); form.setValues(defaultProxyForm); form.clearErrors(); handlers.open() }
  function openEdit(proxy: SiteProxy) { setEditingProxy(proxy); form.setValues(toProxyForm(proxy)); form.clearErrors(); handlers.open() }

  async function refreshAfterWrite() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: proxyQueryKey }),
      queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
      queryClient.invalidateQueries({ queryKey: ['sites'] }),
      queryClient.invalidateQueries({ queryKey: upstreamKeys.all }),
    ])
  }

  async function handleSave(values: ProxyFormValues) {
    try {
      await saveMutation.mutateAsync(values)
      notifySuccess({ message: editingProxy ? '代理配置已更新' : '代理已添加' })
      handlers.close()
      await refreshAfterWrite()
    } catch (error) {
      const desiredSaved = error instanceof ApiError && error.details?.desired_saved === true
      if (desiredSaved) {
        handlers.close()
        await refreshAfterWrite()
      }
      showErrorModal(error, desiredSaved ? '代理已保存，但同步 Nginx 失败' : editingProxy ? '保存反向代理失败' : '添加反向代理失败')
    }
  }

  async function handleToggle(proxy: SiteProxy, enabled: boolean) {
    try {
      await toggleMutation.mutateAsync({ proxy, enabled })
      notifySuccess({ message: enabled ? '已启用' : '已禁用' })
      await refreshAfterWrite()
    } catch (error) {
      await refreshAfterWrite()
      const desiredSaved = error instanceof ApiError && error.details?.desired_saved === true
      showErrorModal(error, desiredSaved ? `代理已${enabled ? '启用' : '禁用'}，但同步 Nginx 失败` : enabled ? '启用反向代理失败' : '禁用反向代理失败')
    }
  }

  async function handleSync() {
    try {
      await syncMutation.mutateAsync()
      notifySuccess({ message: '反向代理配置已重新同步' })
      await refreshAfterWrite()
    } catch (error) {
      showErrorModal(error, '同步反向代理配置失败')
      await refreshAfterWrite()
    }
  }

  function handleDelete(proxy: SiteProxy) {
    confirmDanger({ title: '删除代理', message: `确认删除反向代理「${proxy.name}」？`, confirmLabel: '确认删除', errorTitle: '删除反向代理失败', onConfirm: async () => {
      try {
        await deleteMutation.mutateAsync(proxy.id)
        notifySuccess({ message: '代理已删除' })
      } catch (error) {
        const desiredSaved = error instanceof ApiError && error.details?.desired_saved === true
        showErrorModal(error, desiredSaved ? '代理已删除，但同步 Nginx 失败' : '删除反向代理失败')
      } finally {
        await refreshAfterWrite()
      }
    } })
  }

  return (
    <SectionCard>
      <Stack gap="md">
        {markerMissing ? <Alert color="red" title="反向代理标识块缺失">该站点配置文件中缺少 NxPanel 反向代理标识块，反向代理表单功能将无法安全修改对应片段。请在「站点配置」中检查并修复。</Alert> : null}
        {proxyQuery.isError ? <ErrorAlert error={proxyQuery.error} title="加载反向代理失败" /> : null}
        {upstreamQuery.isError ? <ErrorAlert error={upstreamQuery.error} title="加载上游组失败" /> : null}
        <DataTable
          columns={columns} data={proxyQuery.data || []} loading={proxyQuery.isLoading || proxyQuery.isFetching} emptyText="暂未添加反向代理" plain
          toolbarActions={<Group gap="xs" className="proxyToolbar"><Tooltip label="从已保存状态重新生成配置"><ActionIcon aria-label="重新同步反向代理配置" variant="default" size="lg" loading={syncMutation.isPending} onClick={handleSync}><IconRefresh size={17} /></ActionIcon></Tooltip> <Button leftSection={<IconPlus size={16} />} onClick={openCreate}>添加反向代理</Button> <Button variant="default" leftSection={<IconServer size={16} />} onClick={upstreamManagerHandlers.open}>上游组</Button></Group>}
          renderRowActions={({ row }) => <Group gap={4} wrap="nowrap"><Tooltip label="修改"><ActionIcon aria-label={`修改反向代理 ${row.original.name}`} variant="subtle" onClick={() => openEdit(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip><Tooltip label="删除"><ActionIcon aria-label={`删除反向代理 ${row.original.name}`} color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip></Group>}
        />

        <Modal opened={opened} onClose={handlers.close} title={editingProxy ? '修改反向代理' : '添加反向代理'} size="xl" fullScreen={mobile} closeOnClickOutside={false} centered>
          <form onSubmit={form.onSubmit(handleSave)}>
            <Stack gap="md">
              <Group grow align="flex-start" className="proxyResponsiveGroup"><Switch label="开启代理" {...form.getInputProps('enabled', { type: 'checkbox' })} /><Switch label="开启缓存" {...form.getInputProps('cache_enabled', { type: 'checkbox' })} /></Group>
              {form.values.cache_enabled ? <Stack gap="sm"><Radio.Group label="缓存方式" {...form.getInputProps('cache_type')}><Group mt="xs"><Radio value="nginx" label="Nginx 缓存" /><Tooltip multiline w={320} label="文件缓存仅将代理响应写入磁盘归档，不会从缓存中读取，每次请求仍会转发到后端。"><Radio value="file" label="文件缓存" /></Tooltip></Group></Radio.Group><NumberInput label="缓存时间（分钟）" min={1} max={10080} allowDecimal={false} {...form.getInputProps('cache_time')} /></Stack> : null}

              <Divider label="基本配置" labelPosition="left" />
              <TextInput label="代理名称" {...form.getInputProps('name')} />
              <TextInput label="代理路径" placeholder="/" description="以 / 开头，如 /、/api、/static。" {...form.getInputProps('location_path')} />
               <SegmentedControl fullWidth value={form.values.target_mode} onChange={(value) => { form.setFieldValue('target_mode', value as ProxyFormValues['target_mode']); if (value === 'managed' && !form.values.upstream_id) { form.setFieldValue('proxy_ssl_verify', true); if (form.values.proxy_ssl_verify_depth === 0) form.setFieldValue('proxy_ssl_verify_depth', 3) } }} data={[{ value: 'direct', label: '直接地址' }, { value: 'managed', label: '上游组' }]} />
              {form.values.target_mode === 'direct' ? <TextInput label="目标 URL" placeholder="http://127.0.0.1:3000" {...form.getInputProps('upstream_url')} /> : (
                <Stack gap="sm">
                  <Group align="flex-end" wrap="nowrap" className="managedUpstreamPicker">
                    <Select searchable label="上游组" placeholder="选择上游组" data={(upstreamQuery.data || []).map((item) => ({ value: item.id, label: `${item.name} · ${item.algorithm} · ${availableMembers(item)} 个可用成员` }))} {...form.getInputProps('upstream_id')} style={{ flex: 1 }} />
                      <Tooltip label="管理上游组"><ActionIcon type="button" variant="default" size="lg" aria-label="管理上游组" onClick={upstreamManagerHandlers.open}><IconServer size={17} /></ActionIcon></Tooltip>
                  </Group>
                  <SegmentedControl value={form.values.upstream_scheme} onChange={(value) => form.setFieldValue('upstream_scheme', value as 'http' | 'https')} data={[{ value: 'http', label: 'HTTP' }, { value: 'https', label: 'HTTPS' }]} />
                   {form.values.upstream_scheme === 'https' ? <Stack gap="sm"><TextInput label="SNI Server Name" placeholder="backend.example.com" {...form.getInputProps('proxy_ssl_server_name')} /><Switch label="验证上游证书" checked={form.values.proxy_ssl_verify !== false} onChange={(event) => { const checked = event.currentTarget.checked; form.setFieldValue('proxy_ssl_verify', checked); if (!checked) { form.setFieldValue('proxy_ssl_trusted_certificate', ''); form.setFieldValue('proxy_ssl_verify_depth', 0) } else if (form.values.proxy_ssl_verify_depth === 0) form.setFieldValue('proxy_ssl_verify_depth', 3) }} />{form.values.proxy_ssl_verify !== false ? <Group grow align="flex-start" className="proxyResponsiveGroup"><TextInput label="CA 证书绝对路径" placeholder="/etc/ssl/certs/ca-certificates.crt" {...form.getInputProps('proxy_ssl_trusted_certificate')} /><NumberInput label="验证深度" min={1} max={100} allowDecimal={false} {...form.getInputProps('proxy_ssl_verify_depth')} /></Group> : null}</Stack> : null}
                </Stack>
              )}
              <TextInput label="Host 域名" placeholder="$host" {...form.getInputProps('host_header')} />

              <Divider label="高级配置" labelPosition="left" />
              <Switch label="WebSocket" {...form.getInputProps('websocket_enabled', { type: 'checkbox' })} />
              {form.values.websocket_enabled ? <Group grow align="flex-start" className="proxyResponsiveGroup"><NumberInput label="连接超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('connect_timeout')} /><NumberInput label="发送超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('send_timeout')} /><NumberInput label="读取超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('read_timeout')} /></Group> : null}
              <Switch label="访问限制" {...form.getInputProps('auth_enabled', { type: 'checkbox' })} />
              {form.values.auth_enabled ? <Stack gap="xs"><Group justify="space-between"><Text size="sm" fw={500}>账户</Text><Button size="xs" variant="subtle" onClick={accountManagerHandlers.open}>账户管理</Button></Group><AuthAccountSelector siteId={site.id} value={form.values.auth_account_ids || []} onChange={(value) => form.setFieldValue('auth_account_ids', value)} />{form.errors.auth_account_ids ? <Text size="xs" c="red">{form.errors.auth_account_ids}</Text> : null}</Stack> : null}
              <Group justify="flex-end" className="proxyModalActions"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={saveMutation.isPending}>{editingProxy ? '保存' : '添加'}</Button></Group>
            </Stack>
          </form>
        </Modal>
        <AuthAccountManager siteId={site.id} opened={accountManagerOpened} onClose={accountManagerHandlers.close} />
        {upstreamManagerOpened ? (
          <Suspense fallback={<Modal opened onClose={upstreamManagerHandlers.close} title="上游组管理" size="xl" fullScreen={mobile} centered><Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载上游管理...</Text></Group></Modal>}>
            <UpstreamManagerModal opened={upstreamManagerOpened} onClose={upstreamManagerHandlers.close} />
          </Suspense>
        ) : null}
      </Stack>
    </SectionCard>
  )
}
