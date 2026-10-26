import { ActionIcon, Alert, Button, Group, Loader, Menu, Popover, Stack, Switch, Tabs, Text } from '@mantine/core'
import { IconChevronDown, IconInfoCircle, IconRefresh, IconRestore } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { lazy, Suspense, useEffect, useId, useState } from 'react'
import { getMaintenance, getMaintenanceDefaults, saveMaintenanceDefaults, updateMaintenance } from '@/api/maintenance'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { notifySuccess } from '@/utils/notify'
import { showErrorModal } from '@/utils/errorModal'

const maxBytes = 256 * 1024
const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))
function byteLength(html: string) { return new TextEncoder().encode(html).length }

function HTMLPreview({ html, compact = false }: { html: string; compact?: boolean }) {
  const policy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'"
  return <iframe title="维护页面隔离预览" sandbox="" referrerPolicy="no-referrer" srcDoc={`<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}">${html}`} style={{ width: '100%', height: compact ? 'clamp(160px, calc(100dvh - 500px), 300px)' : 280, border: '1px solid var(--mantine-color-default-border)', borderRadius: 'var(--mantine-radius-sm)', background: 'white' }} />
}

function MaintenanceHTMLWorkspace({ label, html, onChange, compact = false }: { label: string; html: string; onChange: (value: string) => void; compact?: boolean }) {
  const labelID = useId()
  const size = byteLength(html)
  return <Stack gap="xs" role="group" aria-labelledby={labelID} className={compact ? 'maintenanceSiteWorkspace' : undefined}>
    <Text id={labelID} size="sm" fw={500}>{label}</Text>
    <Text size="xs" c="dimmed">最多 256 KB（1 KB = 1024 字节）。预览不允许脚本、同源权限或外部资源。</Text>
    <Tabs defaultValue="html" keepMounted={false}>
      <Tabs.List>
        <Tabs.Tab value="html">HTML</Tabs.Tab>
        <Tabs.Tab value="preview">预览</Tabs.Tab>
      </Tabs.List>
      <Tabs.Panel value="html" pt="sm">
        <div className="nginxEditorShell advancedConfigTemplateEditor">
          <Suspense fallback={<Text size="sm" c="dimmed" p="md">编辑器加载中...</Text>}>
            <NginxCodeEditor language="html" value={html} onChange={onChange} />
          </Suspense>
        </div>
      </Tabs.Panel>
      <Tabs.Panel value="preview" pt="sm"><HTMLPreview html={html} compact={compact} /></Tabs.Panel>
    </Tabs>
    <Text size="xs" c={size > maxBytes ? 'red' : 'dimmed'} role={size > maxBytes ? 'alert' : undefined}>
      {(size / 1024).toFixed(2)} / {maxBytes / 1024} KB{size > maxBytes ? ' · 超过 256 KB 上限' : ''}
    </Text>
  </Stack>
}

export function SiteMaintenanceTab({ site }: { site: SiteDetail }) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: ['maintenance', site.id], queryFn: () => getMaintenance(site.id), refetchOnWindowFocus: false })
  const defaults = useQuery({ queryKey: ['maintenance', 'defaults'], queryFn: getMaintenanceDefaults })
  const mutation = useMutation({ mutationFn: ({ enabled, savePage }: { enabled: boolean; savePage: boolean }) => updateMaintenance(site.id, { enabled, html_override: savePage ? (custom ? html : null) : query.data!.html_override, version: query.data!.version, expected_hash: query.data!.file_hash }) })
  const [html, setHTML] = useState('')
  const [custom, setCustom] = useState(false)
  useEffect(() => { if (query.data) { setHTML(query.data.effective_html); setCustom(!query.data.inherits) } }, [query.data])

  async function apply(enabled: boolean, savePage = true) {
    try {
      await mutation.mutateAsync({ enabled, savePage })
      notifySuccess({ message: enabled ? '网站已进入维护，业务请求返回 503，HTTPS 保持启用' : '已恢复访问，原业务配置保持不变' })
    } catch (error) { showErrorModal(error, '操作未能确认，请检查状态并对账') }
    finally { await Promise.all([query.refetch(), client.invalidateQueries({ queryKey: ['sites'] }), client.invalidateQueries({ queryKey: ['site-detail', site.id] })]) }
  }

  if (query.isLoading) return <Loader size="sm" />
  if (!query.data) return <ErrorAlert error={query.error} title="加载维护配置失败" />
  const state = query.data
  const pending = state.apply_status === 'pending' || state.drifted
  const busy = mutation.isPending || query.isFetching
  const oversize = byteLength(html) > maxBytes
  return <SectionCard><Stack gap="md">
    {pending ? <Alert color="yellow" title="存在待对账状态">上次应用未确认或实际文件与目标状态不同。请重新应用目标状态，或选择恢复访问；不会用旧快照覆盖当前证书。</Alert> : null}
    {state.read_only_reason ? <Alert color="red" title="不能安全进入维护">{state.read_only_reason}</Alert> : null}
    {query.isError ? <ErrorAlert error={query.error} title="刷新状态失败" /> : null}
    <Group justify="space-between" wrap="nowrap" align="flex-start">
      <Switch label="使用单站点维护页面覆盖全局默认" description="直接编辑 HTML 将自动启用单站点覆盖，不影响全局默认。" checked={custom} onChange={(event) => { const checked = event.currentTarget.checked; setCustom(checked); if (!checked) setHTML(defaults.data?.effective_html || state.effective_html) }} />
      <Popover width={300} position="bottom-end" withArrow shadow="md">
        <Popover.Target><ActionIcon variant="subtle" color={state.maintenance_enabled ? 'orange' : 'gray'} aria-label="网站维护模式说明"><IconInfoCircle size={18} /></ActionIcon></Popover.Target>
        <Popover.Dropdown><Text size="sm">{state.maintenance_enabled ? '当前网站维护中。' : ''}维护不会禁用监听或 HTTPS。业务请求返回 503，保留 HTTP-01 验证通路，并发送 Retry-After: 300 和 Cache-Control: no-store。维护期间请使用安全证书局部替换，其他配置修改需先恢复访问。</Text></Popover.Dropdown>
      </Popover>
    </Group>
    <MaintenanceHTMLWorkspace compact label="维护页面 HTML" html={html} onChange={(value) => { if (value !== html) { setHTML(value); setCustom(true) } }} />
    <Group justify="space-between" align="flex-start">
      <Menu position="bottom-start" withinPortal>
        <Menu.Target><Button variant="default" rightSection={<IconChevronDown size={14} />} disabled={busy}>更多</Button></Menu.Target>
        <Menu.Dropdown>
          <Menu.Item leftSection={<IconRestore size={16} />} disabled={!defaults.data} onClick={() => { setCustom(false); setHTML(defaults.data!.effective_html) }}>恢复全局默认</Menu.Item>
          <Menu.Item leftSection={<IconRefresh size={16} />} onClick={() => { void query.refetch() }}>刷新状态</Menu.Item>
        </Menu.Dropdown>
      </Menu>
      <Group gap="sm" justify="flex-end">
        {state.maintenance_enabled || pending
          ? <Button variant="default" disabled={busy} onClick={() => { void apply(false, false) }}>恢复访问</Button>
          : <Button variant="default" disabled={busy || oversize} onClick={() => { void apply(false) }}>仅保存页面设置</Button>}
        <Button color="orange" disabled={busy || oversize || site.status !== 'enabled' || Boolean(state.read_only_reason)} loading={mutation.isPending} onClick={() => { void apply(true) }}>{pending && state.desired_enabled ? '对账：重新应用维护' : state.maintenance_enabled ? '保存并应用维护页面' : '进入维护'}</Button>
      </Group>
    </Group>
  </Stack></SectionCard>
}

export function MaintenanceDefaultsPanel() {
  const client = useQueryClient()
  const query = useQuery({ queryKey: ['maintenance', 'defaults'], queryFn: getMaintenanceDefaults, refetchOnWindowFocus: false })
  const [html, setHTML] = useState('')
  const [builtin, setBuiltin] = useState(false)
  useEffect(() => { if (query.data) { setHTML(query.data.effective_html); setBuiltin(query.data.html === null) } }, [query.data])
  const mutation = useMutation({ mutationFn: () => saveMaintenanceDefaults({ html: builtin ? null : html, version: query.data!.version }) })
  if (query.isLoading) return <Loader size="sm" />
  if (!query.data) return <ErrorAlert error={query.error} title="加载全局维护页面失败" />
  return <Stack gap="md" className="advancedConfigPanel">
    <Alert color="blue" title="全局默认维护页面">保存后会更新正在维护且未设置单站点覆盖的网站。应用失败的网站保留待对账状态，可在网站维护页重新应用。</Alert>
    <MaintenanceHTMLWorkspace label="默认维护 HTML" html={html} onChange={(value) => { setHTML(value); setBuiltin(false) }} />
    <Group justify="space-between">
      <Button variant="subtle" onClick={() => { setBuiltin(true); setHTML(query.data!.builtin_html) }}>恢复内置默认</Button>
      <Button loading={mutation.isPending} disabled={byteLength(html) > maxBytes || query.isFetching} onClick={async () => {
        try { await mutation.mutateAsync(); notifySuccess({ message: '全局维护页面已保存并应用' }) }
        catch (error) { showErrorModal(error, '保存或应用失败，请检查待对账站点') }
        finally { await client.invalidateQueries({ queryKey: ['maintenance'] }) }
      }}>保存全局默认</Button>
    </Group>
  </Stack>
}
