import { ActionIcon, Alert, Badge, Button, Divider, Group, Modal, NumberInput, Radio, Stack, Switch, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEdit, IconPlus, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useState } from 'react'
import { createProxy, deleteProxy, listProxies, updateProxy } from '@/api/proxy'
import type { CreateProxyRequest, SiteDetail, SiteProxy } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { DataTable } from '@/components/tables/DataTable'
import { siteDetailKeys } from '@/hooks/useSiteDetail'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteProxyTabProps {
  site: SiteDetail
}

type ProxyFormValues = CreateProxyRequest

const defaultProxyForm: ProxyFormValues = {
  name: '',
  enabled: true,
  location_path: '/',
  upstream_url: 'http://127.0.0.1:3000',
  host_header: '$host',
  websocket_enabled: false,
  connect_timeout: 60,
  send_timeout: 60,
  read_timeout: 60,
  cache_enabled: false,
  cache_type: 'nginx',
  cache_time: 60,
}

function isProxyMarkerMissing(site: SiteDetail): boolean {
  return (site.marker_status?.missing || []).includes('MAIN-LOCATION')
}

function toProxyForm(proxy: SiteProxy): ProxyFormValues {
  return {
    name: proxy.name,
    enabled: proxy.enabled,
    location_path: proxy.location_path,
    upstream_url: proxy.upstream_url,
    host_header: proxy.host_header,
    websocket_enabled: proxy.websocket_enabled,
    connect_timeout: proxy.connect_timeout,
    send_timeout: proxy.send_timeout,
    read_timeout: proxy.read_timeout,
    cache_enabled: proxy.cache_enabled,
    cache_type: proxy.cache_type,
    cache_time: proxy.cache_time,
  }
}

export function SiteProxyTab({ site }: SiteProxyTabProps) {
  const queryClient = useQueryClient()
  const [opened, handlers] = useDisclosure(false)
  const [editingProxy, setEditingProxy] = useState<SiteProxy | null>(null)
  const proxyQueryKey = ['site-detail', site.id, 'proxy'] as const
  const proxyQuery = useQuery({ queryKey: proxyQueryKey, queryFn: () => listProxies(site.id) })
  const saveMutation = useMutation({
    mutationFn: (values: ProxyFormValues) => editingProxy
      ? updateProxy(site.id, editingProxy.id, values)
      : createProxy(site.id, values),
  })
  const toggleMutation = useMutation({ mutationFn: (proxy: SiteProxy) => updateProxy(site.id, proxy.id, toProxyForm(proxy)) })
  const deleteMutation = useMutation({ mutationFn: (proxyId: string) => deleteProxy(site.id, proxyId) })
  const markerMissing = isProxyMarkerMissing(site)
  const form = useForm<ProxyFormValues>({
    initialValues: defaultProxyForm,
    validate: {
      name: (value) => value.trim() ? null : '请输入代理名称',
      location_path: (value) => value.trim().startsWith('/') ? null : '代理路径必须以 / 开头',
      upstream_url: (value) => value.trim() ? null : '请输入目标 URL',
      host_header: (value) => value.trim() ? null : '请输入 Host 域名',
      connect_timeout: (value) => value >= 1 && value <= 3600 ? null : '连接超时范围为 1-3600 秒',
      send_timeout: (value) => value >= 1 && value <= 3600 ? null : '发送超时范围为 1-3600 秒',
      read_timeout: (value) => value >= 1 && value <= 3600 ? null : '读取超时范围为 1-3600 秒',
      cache_time: (value, values) => !values.cache_enabled || (value >= 1 && value <= 10080) ? null : '缓存时间范围为 1-10080 分钟',
    },
  })

  const columns: MRT_ColumnDef<SiteProxy>[] = [
    { accessorKey: 'name', header: '名称', size: 140 },
    { accessorKey: 'location_path', header: '代理路径', size: 120, Cell: ({ cell }) => <MonoText value={cell.getValue<string>()} maxWidth={160} /> },
    { accessorKey: 'upstream_url', header: '目标 URL', Cell: ({ cell }) => <MonoText value={cell.getValue<string>()} maxWidth={260} /> },
    { accessorKey: 'cache_enabled', header: '缓存', size: 90, Cell: ({ row }) => <Badge color={row.original.cache_enabled ? 'green' : 'gray'} variant="light">{row.original.cache_enabled ? '是' : '否'}</Badge> },
    { accessorKey: 'enabled', header: '状态', size: 90, Cell: ({ row }) => <Switch checked={row.original.enabled} disabled={toggleMutation.isPending} onChange={(event) => handleToggle(row.original, event.currentTarget.checked)} /> },
  ]

  function openCreate() {
    setEditingProxy(null)
    form.setValues(defaultProxyForm)
    form.clearErrors()
    handlers.open()
  }

  function openEdit(proxy: SiteProxy) {
    setEditingProxy(proxy)
    form.setValues(toProxyForm(proxy))
    form.clearErrors()
    handlers.open()
  }

  async function refreshAfterWrite() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: proxyQueryKey }),
      queryClient.invalidateQueries({ queryKey: siteDetailKeys.detail(site.id) }),
      queryClient.invalidateQueries({ queryKey: ['sites'] }),
    ])
  }

  async function handleSave(values: ProxyFormValues) {
    try {
      await saveMutation.mutateAsync({ ...values, name: values.name.trim(), location_path: values.location_path.trim(), upstream_url: values.upstream_url.trim(), host_header: values.host_header.trim() })
      notifySuccess({ message: editingProxy ? '代理配置已更新' : '代理已添加' })
      handlers.close()
      await refreshAfterWrite()
    } catch (error) {
      showErrorModal(error, editingProxy ? '保存反向代理失败' : '添加反向代理失败')
    }
  }

  async function handleToggle(proxy: SiteProxy, enabled: boolean) {
    try {
      await toggleMutation.mutateAsync({ ...proxy, enabled })
      notifySuccess({ message: enabled ? '已启用' : '已禁用' })
      await refreshAfterWrite()
    } catch (error) {
      showErrorModal(error, enabled ? '启用反向代理失败' : '禁用反向代理失败')
    }
  }

  function handleDelete(proxy: SiteProxy) {
    confirmDanger({
      title: '删除代理',
      message: `确认删除反向代理「${proxy.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除反向代理失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(proxy.id)
        notifySuccess({ message: '代理已删除' })
        await refreshAfterWrite()
      },
    })
  }

  return (
    <SectionCard>
      <Stack gap="md">
        {markerMissing ? (
          <Alert color="red" title="反向代理标识块缺失">
            该站点配置文件中缺少 NxPanel 反向代理标识块，反向代理表单功能将无法安全修改对应片段。请在「站点配置」中检查并修复。
          </Alert>
        ) : null}
        {proxyQuery.isError ? <ErrorAlert error={proxyQuery.error} title="加载反向代理失败" /> : null}
        <DataTable
          columns={columns}
          data={proxyQuery.data || []}
          loading={proxyQuery.isLoading || proxyQuery.isFetching}
          emptyText="暂未添加反向代理"
          plain
          toolbarActions={<Button leftSection={<IconPlus size={16} />} onClick={openCreate}>添加反向代理</Button>}
          renderRowActions={({ row }) => (
            <Group gap={4} wrap="nowrap">
              <Tooltip label="修改"><ActionIcon variant="subtle" onClick={() => openEdit(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
              <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
            </Group>
          )}
        />

        <Modal opened={opened} onClose={handlers.close} title={editingProxy ? '修改反向代理' : '添加反向代理'} size="lg" closeOnClickOutside={false} centered>
          <form onSubmit={form.onSubmit(handleSave)}>
            <Stack gap="md">
              {/* 缓存和启用开关会直接影响生成的 Nginx location，放在表单顶部便于保存前确认。 */}
              <Group grow align="flex-start">
                <Switch label="开启代理" {...form.getInputProps('enabled', { type: 'checkbox' })} />
                <Switch label="开启缓存" {...form.getInputProps('cache_enabled', { type: 'checkbox' })} />
              </Group>

              {form.values.cache_enabled ? (
                <Stack gap="sm">
                  <Radio.Group label="缓存方式" {...form.getInputProps('cache_type')}>
                    <Group mt="xs">
                      <Radio value="nginx" label="Nginx 缓存" />
                      <Tooltip multiline w={320} label="文件缓存仅将代理响应写入磁盘归档，不会从缓存中读取，每次请求仍会转发到后端。同时受 proxy_store 限制，不能缓存路径后是 / 的请求。">
                        <Radio value="file" label="文件缓存" />
                      </Tooltip>
                    </Group>
                  </Radio.Group>
                  <NumberInput label="缓存时间（分钟）" min={1} max={10080} allowDecimal={false} {...form.getInputProps('cache_time')} />
                </Stack>
              ) : null}

              <Divider label="基本配置" labelPosition="left" />
              <TextInput label="代理名称" placeholder="请输入代理名称" {...form.getInputProps('name')} />
              <TextInput label="代理路径" placeholder="/" description="以 / 开头，如 /、/api、/static。" {...form.getInputProps('location_path')} />
              <TextInput label="目标 URL" placeholder="http://127.0.0.1:3000" {...form.getInputProps('upstream_url')} />
              <TextInput label="Host 域名" placeholder="$host" description="默认 $host，可自定义如 example.com。" {...form.getInputProps('host_header')} />

              <Divider label="高级配置" labelPosition="left" />
              <Switch label="WebSocket" {...form.getInputProps('websocket_enabled', { type: 'checkbox' })} />
              <Group grow align="flex-start">
                <NumberInput label="连接超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('connect_timeout')} />
                <NumberInput label="发送超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('send_timeout')} />
                <NumberInput label="读取超时（秒）" min={1} max={3600} allowDecimal={false} {...form.getInputProps('read_timeout')} />
              </Group>
              <Group justify="flex-end"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={saveMutation.isPending}>{editingProxy ? '保存' : '添加'}</Button></Group>
            </Stack>
          </form>
        </Modal>
      </Stack>
    </SectionCard>
  )
}
