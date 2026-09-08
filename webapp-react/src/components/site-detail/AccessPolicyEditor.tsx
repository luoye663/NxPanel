import { ActionIcon, Alert, Badge, Button, Checkbox, Divider, Group, Modal, MultiSelect, NumberInput, Select, SimpleGrid, Stack, Switch, Table, TagsInput, Text, TextInput, Textarea, Tooltip } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconEdit, IconGripVertical, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { accessPolicyKeys, getAccessPolicy, previewAccessPolicy, saveAccessPolicy, syncAccessPolicy, type AccessAction, type AccessCondition, type AccessPolicy, type AccessPolicyRule, type AccessPreviewRequest, type AccessPreviewResult } from '@/api/accessPolicy'
import { geoAccessKeys, getGeoIPStatus } from '@/api/geoAccess'
import { ApiError } from '@/api/client'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { AuthAccountManager, AuthAccountSelector } from './AuthAccountManager'
import { notifySuccess } from '@/utils/notify'

interface Props {
  siteId: string
  initialPolicy: AccessPolicy
  focusSource?: string
  onCancelMigration: () => void
  onDirtyChange?: (dirty: boolean) => void
}

const conditionLabels: Record<AccessCondition['kind'], string> = { ip: 'IP / CIDR', country: '国家 / 地区', path: '请求路径', extension: '文件后缀', referer: 'Referer' }
const conditionHelp: Record<AccessCondition['kind'], string> = {
  ip: '例如 203.0.113.8、2001:db8::/32；按回车添加，可粘贴多行。',
  country: '可搜索中文名称或国家代码并多选；未知地区（ZZ）表示无法识别地域的 IP。需要先安装 GeoIP 数据库。',
  path: '以 / 开头，不含查询参数，例如 /admin 或 /api。',
  extension: '例如 jpg、png、zip，不含开头的点。',
  referer: '填写域名，例如 example.com、*.example.com；none 表示空 Referer，blocked 表示无有效协议的 Referer。',
}

function countryName(code: string) {
  if (code === 'TW') return '中国台湾 (TW)'
  if (code === 'ZZ') return '未知地区 (ZZ)'
  try { return `${new Intl.DisplayNames(['zh-CN'], { type: 'region' }).of(code) || code} (${code})` } catch { return code }
}

function actionLabel(action: AccessAction) {
  if (action.type === 'allow') return '放行并结束检查'
  if (action.type === 'auth') return '密码验证通过后放行'
  return action.status_code === 444 ? '断开连接（444）' : `拒绝（${action.status_code || 403}）`
}

function conditionLabel(condition: AccessCondition) {
  if (condition.kind === 'path' && !['exact', 'prefix'].includes(condition.operator || '')) return `旧路径表达式（需转换）：${condition.values.join('、')}`
  const operator = condition.kind === 'path' ? (condition.operator === 'exact' ? '等于' : '前缀为') : '属于'
  return `${conditionLabels[condition.kind]} ${condition.negate ? '不' : ''}${operator} ${condition.values.map((value) => condition.kind === 'country' ? countryName(value) : value).join('、')}`
}

function emptyRule(): AccessPolicyRule {
  return { id: crypto.randomUUID(), name: '', enabled: true, match: 'all', conditions: [{ kind: 'ip', operator: 'in', values: [], negate: false }], action: { type: 'allow' } }
}

function actionError(action: AccessAction): string | null {
  if (action.type === 'auth' && (!action.account_ids?.length || action.account_ids.length > 1024)) return '密码验证需要选择 1–1024 个账户。'
  const status = action.status_code || 403
  if (action.type === 'deny' && (!Number.isInteger(status) || status < 400 || status > 599)) return '拒绝状态码必须是 400–599 的整数。'
  if (action.type === 'deny' && new TextEncoder().encode(action.response_body || '').length > 65536) return '响应正文不能超过 64 KiB（UTF-8）。'
  return null
}

function ActionFields({ action, onChange, siteId }: { action: AccessAction; onChange: (action: AccessAction) => void; siteId: string }) {
  return <Stack gap="sm">
    <Select label="执行动作" value={action.type} allowDeselect={false} data={[{ value: 'allow', label: '放行并结束检查' }, { value: 'deny', label: '拒绝 / 自定义响应' }, { value: 'auth', label: '密码验证通过后放行' }]} onChange={(value) => onChange(value === 'deny' ? { type: 'deny', status_code: 403, response_type: 'text', response_body: '' } : value === 'auth' ? { type: 'auth', account_ids: [] } : { type: 'allow' })} />
    {action.type === 'deny' ? <>
      <NumberInput label="HTTP 状态码" description="400–599；444 表示断开连接，不返回响应正文。" min={400} max={599} allowDecimal={false} value={action.status_code ?? 403} onChange={(value) => onChange({ ...action, status_code: Number(value) })} />
      {action.status_code !== 444 ? <>
        <Select label="响应格式" value={action.response_type || 'text'} allowDeselect={false} data={[{ value: 'text', label: 'TXT 纯文本' }, { value: 'html', label: 'HTML' }]} onChange={(value) => onChange({ ...action, response_type: value as 'text' | 'html' })} />
        <Textarea label="响应正文" description="留空时返回默认错误响应，最多 64 KiB（UTF-8）。" minRows={3} maxLength={65536} value={action.response_body || ''} onChange={(event) => onChange({ ...action, response_body: event.currentTarget.value })} />
      </> : null}
    </> : null}
    {action.type === 'auth' ? <>
      <Text>验证失败返回 401；验证通过后结束本套检查。</Text>
      <AuthAccountSelector siteId={siteId} value={action.account_ids || []} onChange={(account_ids) => onChange({ ...action, account_ids })} />
    </> : null}
  </Stack>
}

export function AccessPolicyEditor({ siteId, initialPolicy, focusSource, onCancelMigration, onDirtyChange }: Props) {
  const client = useQueryClient()
  const mobile = useMediaQuery('(max-width: 48rem)')
  const [baseline, setBaseline] = useState(initialPolicy)
  const [draft, setDraft] = useState<AccessPolicy>({ ...initialPolicy, mode: 'unified' })
  const [editing, setEditing] = useState<AccessPolicyRule | null>(null)
  const geoStatus = useQuery({ queryKey: geoAccessKeys.status, queryFn: getGeoIPStatus, enabled: editing?.conditions.some((condition) => condition.kind === 'country') ?? false })
  const countries = [...new Set([
    ...(geoStatus.data?.countries || []),
    'ZZ',
    ...(editing?.conditions.filter((condition) => condition.kind === 'country').flatMap((condition) => condition.values) || []),
  ])].sort().map((code) => ({ value: code, label: countryName(code) }))
  const [ruleError, setRuleError] = useState<string | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [accountsOpened, setAccountsOpened] = useState(false)
  const [draggedId, setDraggedId] = useState<string | null>(null)
  const [previewOpened, setPreviewOpened] = useState(false)
  const [previewRequest, setPreviewRequest] = useState<AccessPreviewRequest>({ ip: '', path: '/', referer: '' })
  const [preview, setPreview] = useState<AccessPreviewResult | null>(null)
  const dirty = JSON.stringify(draft) !== JSON.stringify({ ...baseline, mode: 'unified' }) || baseline.version === 0
  const save = useMutation({ mutationFn: () => saveAccessPolicy(siteId, draft) })
  const sync = useMutation({ mutationFn: () => syncAccessPolicy(siteId) })
  const previewMutation = useMutation({ mutationFn: () => previewAccessPolicy(siteId, draft, previewRequest) })
  const pendingProxy = baseline.pending_source === 'proxy' && baseline.apply_status !== 'applied'
  const working = save.isPending || sync.isPending || previewMutation.isPending
  const busy = working || pendingProxy
  const linkedProxy = editing?.source_type === 'proxy'

  useEffect(() => { onDirtyChange?.(dirty || editing !== null); return () => onDirtyChange?.(false) }, [dirty, editing, onDirtyChange])
  useEffect(() => {
    if (!dirty && !editing) return
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [dirty, editing])

  function changePolicy(next: AccessPolicy) { setDraft(next); setPreview(null); previewMutation.reset(); setError(null) }
  function changeRules(rules: AccessPolicyRule[]) { changePolicy({ ...draft, rules }) }
  function moveRule(id: string, to: number) {
    const from = draft.rules.findIndex((rule) => rule.id === id)
    if (from < 0 || to < 0 || to >= draft.rules.length || from === to) return
    const rules = [...draft.rules]
    const [rule] = rules.splice(from, 1)
    rules.splice(to, 0, rule)
    changeRules(rules)
  }

  function finishEditing() {
    if (!editing) return
    const message = !editing.name.trim() ? '请填写规则名称。' : new TextEncoder().encode(editing.name.trim()).length > 256 ? '规则名称不能超过 256 字节（UTF-8）。' : editing.conditions.some((condition) => !condition.values.length) ? '请为每个条件添加至少一个值。' : actionError(editing.action)
    if (message) { setRuleError(message); return }
    const rule = { ...editing, name: editing.name.trim() }
    changeRules(draft.rules.some((item) => item.id === rule.id) ? draft.rules.map((item) => item.id === rule.id ? rule : item) : [...draft.rules, rule])
    setEditing(null)
  }

  async function acceptPolicy(policy: AccessPolicy) {
    setDraft(policy); setBaseline(policy); setPreview(null)
    client.setQueryData(accessPolicyKeys.site(siteId), policy)
    await client.invalidateQueries({ queryKey: ['site-detail', siteId] })
  }

  async function apply() {
    const message = actionError(draft.default_action)
    if (message) { setError(new Error(message)); return }
    setError(null)
    try {
      const policy = await save.mutateAsync()
      await acceptPolicy(policy)
      if (policy.apply_status === 'applied') notifySuccess({ message: '访问策略已保存并应用' })
    } catch (failure) {
      setError(failure)
      if (failure instanceof ApiError && failure.details?.desired_saved === true && typeof failure.details.saved_version === 'number') {
        try {
          const stored = await getAccessPolicy(siteId)
          // Only our exact persisted version can replace this draft. Another editor
          // may have saved again while the failed request was returning.
          if (stored.version === failure.details.saved_version) {
            setBaseline(stored); setDraft({ ...stored, mode: 'unified' })
            client.setQueryData(accessPolicyKeys.site(siteId), stored)
          }
        } catch { /* Keep the original draft if status recovery is unavailable. */ }
      } else {
        // A stale editor must retain its version until the user explicitly reloads.
        await client.invalidateQueries({ queryKey: accessPolicyKeys.site(siteId) })
      }
    }
  }

  async function resync() {
    setError(null)
    try { const policy = await sync.mutateAsync(); await acceptPolicy(policy); if (policy.apply_status === 'applied') notifySuccess({ message: '访问策略已重新同步' }) } catch (failure) {
      setError(failure)
      // A proxy retry may persist a newer desired version before its transaction fails.
      try {
        const stored = await getAccessPolicy(siteId)
        setBaseline(stored); setDraft({ ...stored, mode: 'unified' })
        client.setQueryData(accessPolicyKeys.site(siteId), stored)
      } catch { /* The existing retry remains available when status cannot be read. */ }
    }
  }

  async function reloadSaved() {
    if (dirty && !window.confirm('放弃当前草稿并重新载入已保存的策略？')) return
    const policy = await client.fetchQuery({ queryKey: accessPolicyKeys.site(siteId), queryFn: () => getAccessPolicy(siteId), staleTime: 0 }).catch(setError)
    if (policy) { setBaseline(policy); changePolicy({ ...policy, mode: 'unified' }) }
  }

  return <SectionCard title="访问策略" description="从上到下判断，首条命中的规则决定结果。拖动或使用上下移动按钮调整顺序，保存后统一生效。">
    <Stack gap="md">
      {baseline.mode === 'legacy' && baseline.version === 0 ? <Alert color="yellow" title="正在准备切换">
        现有策略仍在运行。请检查导入规则和行为差异，点击“保存并应用”后切换为统一策略。
      </Alert> : null}
      {baseline.mode === 'legacy' && draft.warnings?.length ? <Alert color="yellow" title="需要检查的行为差异"><Stack gap={4}>{draft.warnings.map((warning, index) => <Text key={index}>{warning}</Text>)}</Stack></Alert> : null}
      {baseline.version > 0 && baseline.apply_status !== 'applied' ? <Alert color="yellow" title="策略已保存，等待应用成功">{baseline.last_error || '请重新同步，确认线上配置与已保存策略一致。'}</Alert> : null}
      {pendingProxy ? <Alert color="yellow" title="先完成关联反代同步">反代配置与访问策略有未完成的关联修改。请点击“重新同步”一起应用，成功后再编辑规则。</Alert> : null}
      {error ? <ErrorAlert error={error} title="未完成应用，草稿已保留" /> : null}
      <Group justify="space-between">
        <Group><Button leftSection={<IconPlus size={16} />} disabled={busy || draft.rules.length >= 256} onClick={() => { setRuleError(null); setEditing(emptyRule()) }}>新增规则</Button><Button variant="default" disabled={busy} onClick={() => setAccountsOpened(true)}>账户管理</Button></Group>
        <Badge variant="light" color={dirty ? 'yellow' : 'green'}>{dirty ? '草稿未应用' : '已保存'}</Badge>
      </Group>
      {focusSource ? <Alert color="blue">{focusSource === 'hotlink' ? '防盗链规则已纳入以下执行顺序。' : '反代密码验证已纳入以下执行顺序。'}相关导入规则带有“来源”标记；也可以添加新的组合条件。</Alert> : null}
      <Table.ScrollContainer minWidth={680}>
        <Table verticalSpacing="sm" withRowBorders styles={{ td: { verticalAlign: 'middle' } }}>
          <Table.Thead><Table.Tr><Table.Th w={120}>顺序</Table.Th><Table.Th>规则与条件</Table.Th><Table.Th w={160}>动作</Table.Th><Table.Th w={120}>状态</Table.Th><Table.Th w={88}>操作</Table.Th></Table.Tr></Table.Thead>
          <Table.Tbody>{draft.rules.map((rule, index) => <Table.Tr key={rule.id} onDragOver={(event) => { if (draggedId && !busy) event.preventDefault() }} onDrop={(event) => { event.preventDefault(); if (draggedId && !busy) moveRule(draggedId, index); setDraggedId(null) }}>
            <Table.Td><Group gap={4} wrap="nowrap">
              <Tooltip label="拖动调整顺序"><ActionIcon variant="subtle" aria-label={`拖动 ${rule.name} 调整顺序`} disabled={busy} draggable={!busy} onDragStart={(event) => { setDraggedId(rule.id); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', rule.id) }} onDragEnd={() => setDraggedId(null)}><IconGripVertical size={16} /></ActionIcon></Tooltip>
              <Text w={20}>{index + 1}</Text><Stack gap={2}>
                <ActionIcon variant="subtle" aria-label={`上移 ${rule.name}`} disabled={busy || index === 0} onClick={() => moveRule(rule.id, index - 1)}><IconArrowUp size={15} /></ActionIcon>
                <ActionIcon variant="subtle" aria-label={`下移 ${rule.name}`} disabled={busy || index === draft.rules.length - 1} onClick={() => moveRule(rule.id, index + 1)}><IconArrowDown size={15} /></ActionIcon>
              </Stack>
            </Group></Table.Td>
            <Table.Td><Stack gap={4}><Group gap={6}><Text fw={600}>{rule.name}</Text>{rule.source_type && (focusSource === rule.source_type || focusSource === rule.source_id) ? <Badge variant="outline">来源：{rule.source_type === 'hotlink' ? '防盗链' : '反向代理'}</Badge> : null}{rule.source_disabled ? <Badge variant="light" color="yellow">关联反代已停用</Badge> : null}</Group><Text size="xs" c="dimmed">{!rule.conditions.length ? (rule.match === 'all' ? '始终匹配' : '无可匹配的条件') : rule.match === 'all' ? '全部条件满足' : '任一条件满足'}</Text>{rule.conditions.map((condition, ci) => <Text key={ci} size="xs" style={{ overflowWrap: 'anywhere' }}>{conditionLabel(condition)}</Text>)}</Stack></Table.Td>
            <Table.Td><Text>{actionLabel(rule.action)}</Text></Table.Td>
            <Table.Td><Switch styles={{ body: { alignItems: 'center' }, track: { flexShrink: 0 }, label: { whiteSpace: 'nowrap' } }} aria-label={`启用 ${rule.name}`} checked={rule.enabled} label={rule.enabled ? '启用' : '禁用'} disabled={busy} onChange={(event) => { const enabled = event.currentTarget.checked; changeRules(draft.rules.map((item) => item.id === rule.id ? { ...item, enabled } : item)) }} /></Table.Td>
            <Table.Td><Group gap={4} wrap="nowrap"><Tooltip label="编辑"><ActionIcon aria-label={`编辑 ${rule.name}`} variant="subtle" disabled={busy} onClick={() => { setRuleError(null); setEditing(structuredClone(rule)) }}><IconEdit size={16} /></ActionIcon></Tooltip><Tooltip label="从草稿中移除"><ActionIcon aria-label={`移除 ${rule.name}`} variant="subtle" color="red" disabled={busy} onClick={() => changeRules(draft.rules.filter((item) => item.id !== rule.id))}><IconTrash size={16} /></ActionIcon></Tooltip></Group></Table.Td>
          </Table.Tr>)}</Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {!draft.rules.length ? <Text ta="center" c="dimmed" py="lg">暂无规则。请求将执行下方的默认动作。</Text> : null}
      <Divider label="全部未命中时" labelPosition="left" />
      <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><ActionFields action={draft.default_action} onChange={(default_action) => changePolicy({ ...draft, default_action })} siteId={siteId} /></fieldset>
      <Text size="xs" c="dimmed">“放行”结束本套访问检查。WAF、上游应用认证和手写配置中的限制仍独立生效。</Text>
      <Group justify="space-between">
        <Group><Button variant="default" disabled={busy} onClick={() => setPreviewOpened((value) => !value)}>{previewOpened ? '收起预览' : '测试请求'}</Button><Button variant="subtle" disabled={working} onClick={reloadSaved}>重新载入</Button></Group>
        <Group>{baseline.mode === 'legacy' && baseline.version === 0 ? <Button variant="default" disabled={busy} onClick={onCancelMigration}>返回现有设置</Button> : <Button variant="default" loading={sync.isPending} disabled={dirty || save.isPending || previewMutation.isPending} onClick={resync}>重新同步</Button>}<Button loading={save.isPending} disabled={!dirty || pendingProxy || sync.isPending || previewMutation.isPending} onClick={apply}>保存并应用</Button></Group>
      </Group>
      {previewOpened ? <>
        <Divider label="测试当前草稿" labelPosition="left" />
        <Text c="dimmed">填写示例请求，查看每条规则的判断和最终动作。密码规则只显示“需要验证”，不接收密码。</Text>
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput label="访问者 IP" description="地域根据此 IP 和当前 GeoIP 数据库判断。" placeholder="203.0.113.8" required disabled={previewMutation.isPending} value={previewRequest.ip} onChange={(event) => { setPreviewRequest({ ...previewRequest, ip: event.currentTarget.value }); setPreview(null) }} />
          <TextInput label="请求路径" placeholder="/admin" required disabled={previewMutation.isPending} value={previewRequest.path} onChange={(event) => { setPreviewRequest({ ...previewRequest, path: event.currentTarget.value }); setPreview(null) }} />
          <TextInput label="Referer（可选）" placeholder="https://example.com/page" disabled={previewMutation.isPending} value={previewRequest.referer || ''} onChange={(event) => { setPreviewRequest({ ...previewRequest, referer: event.currentTarget.value }); setPreview(null) }} />
        </SimpleGrid>
        <Group><Button variant="light" loading={previewMutation.isPending} disabled={!previewRequest.ip.trim() || !previewRequest.path.startsWith('/')} onClick={async () => { setPreview(null); try { setPreview(await previewMutation.mutateAsync()) } catch { /* Mutation state displays the error. */ } }}>运行预览</Button></Group>
        {previewMutation.error ? <ErrorAlert error={previewMutation.error} title="预览失败" /> : null}
        {preview ? <Stack gap="xs" aria-live="polite"><Alert color="blue" title={`最终动作：${actionLabel(preview.action)}`}>{preview.matched_rule_id === 'default' ? '来源：全部未命中时的默认动作' : `来源：${draft.rules.find((rule) => rule.id === preview.matched_rule_id)?.name || preview.matched_rule_id}`}{preview.requires_authentication ? '；需要密码验证' : ''}</Alert>{preview.rules.map((result, index) => <Group key={result.id} justify="space-between"><Text>{index + 1}. {draft.rules.find((rule) => rule.id === result.id)?.name}</Text><Text size="xs">{!result.evaluated ? '未执行（已结束或已禁用）' : result.matched ? '命中' : '未命中'}{result.evaluated && result.conditions.length ? ` · 条件：${result.conditions.map((matched) => matched ? '满足' : '不满足').join(' / ')}` : ''}</Text></Group>)}</Stack> : null}
      </> : null}
    </Stack>
    <Modal opened={editing !== null} onClose={() => setEditing(null)} title={draft.rules.some((rule) => rule.id === editing?.id) ? '编辑访问规则' : '新增访问规则'} size="lg" fullScreen={mobile} closeOnClickOutside={false}>
      {editing ? <Stack gap="md">
        <TextInput label="规则名称" required maxLength={100} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.currentTarget.value })} />
        {linkedProxy ? <Alert color="blue" title="已关联反代"><Stack gap="xs"><Text>首个路径条件和反代启停状态跟随关联反代。可添加其他条件；解除关联后可独立修改路径与匹配方式，规则按自身启用状态执行。</Text><Group><Button variant="light" onClick={() => setEditing({ ...editing, source_type: undefined, source_id: undefined, source_disabled: undefined })}>解除反代关联</Button></Group></Stack></Alert> : null}
        <Checkbox label="始终匹配（处理此前未命中的所有请求）" disabled={linkedProxy} checked={!editing.conditions.length && editing.match === 'all'} onChange={(event) => setEditing({ ...editing, match: 'all', conditions: event.currentTarget.checked ? [] : [{ kind: 'ip', operator: 'in', values: [], negate: false }] })} />
        {editing.conditions.length || editing.match === 'any' ? <Select label="匹配方式" disabled={linkedProxy} value={editing.match} allowDeselect={false} data={[{ value: 'all', label: '全部条件满足（并且）' }, { value: 'any', label: '任一条件满足（或者）' }]} onChange={(value) => setEditing({ ...editing, match: value as 'all' | 'any' })} /> : <Alert color="yellow">此规则会处理所有到达它的请求，后面的规则不会再执行。</Alert>}
        {editing.conditions.map((condition, index) => <fieldset disabled={linkedProxy && index === 0} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }} key={index}><Stack gap="xs">
          <Divider label={`条件 ${index + 1}`} labelPosition="left" />
          <Group align="flex-end"><Select style={{ flex: 1 }} label="条件类型" value={condition.kind} allowDeselect={false} data={Object.entries(conditionLabels).map(([value, label]) => ({ value, label }))} onChange={(value) => setEditing({ ...editing, conditions: editing.conditions.map((item, ci) => ci === index ? { kind: value as AccessCondition['kind'], operator: value === 'path' ? 'prefix' : 'in', values: [], negate: false } : item) })} /><ActionIcon variant="subtle" color="red" aria-label={`移除条件 ${index + 1}`} disabled={editing.conditions.length === 1} onClick={() => setEditing({ ...editing, conditions: editing.conditions.filter((_, ci) => ci !== index) })}><IconTrash size={16} /></ActionIcon></Group>
          {condition.kind === 'path' ? <Select label="路径匹配" error={!['exact', 'prefix'].includes(condition.operator || '') ? '旧路径表达式需要转换：请选择匹配方式并检查匹配值。' : undefined} value={condition.operator || null} allowDeselect={false} data={[{ value: 'prefix', label: '前缀匹配' }, { value: 'exact', label: '完全相等' }]} onChange={(value) => setEditing({ ...editing, conditions: editing.conditions.map((item, ci) => ci === index ? { ...item, operator: value as 'prefix' | 'exact' } : item) })} /> : null}
          {condition.kind === 'country' ? <>
            <MultiSelect label="匹配值" required description={conditionHelp.country} placeholder={geoStatus.isFetching ? '正在加载国家 / 地区…' : '搜索并选择国家 / 地区'} data={countries} searchable clearable nothingFoundMessage="没有匹配的国家 / 地区" maxDropdownHeight={300} value={condition.values} onChange={(values) => setEditing({ ...editing, conditions: editing.conditions.map((item, ci) => ci === index ? { ...item, values } : item) })} />
            {geoStatus.error ? <Stack gap="xs"><ErrorAlert error={geoStatus.error} title="国家 / 地区列表加载失败" /><Button variant="subtle" size="xs" loading={geoStatus.isFetching} onClick={() => void geoStatus.refetch()}>重新加载列表</Button></Stack> : null}
            {geoStatus.data && !geoStatus.data.installed ? <Text size="xs" c="dimmed">请先在地域策略设置中安装 GeoIP 数据库，再应用地域条件。</Text> : null}
          </> : <TagsInput label="匹配值" required description={conditionHelp[condition.kind]} placeholder="输入后按回车添加" value={condition.values} splitChars={[',', '\n']} acceptValueOnBlur onChange={(values) => setEditing({ ...editing, conditions: editing.conditions.map((item, ci) => ci === index ? { ...item, values: values.map((value) => condition.kind === 'extension' ? value.replace(/^\./, '').toLowerCase() : value) } : item) })} />}
          <Checkbox label="条件取反（不符合以上值时满足）" checked={condition.negate} onChange={(event) => { const negate = event.currentTarget.checked; setEditing({ ...editing, conditions: editing.conditions.map((item, ci) => ci === index ? { ...item, negate } : item) }) }} />
        </Stack></fieldset>)}
        <Group><Button variant="light" leftSection={<IconPlus size={16} />} disabled={editing.conditions.length >= 16} onClick={() => setEditing({ ...editing, conditions: [...editing.conditions, { kind: 'path', operator: 'prefix', values: [], negate: false }] })}>添加条件</Button></Group>
        <Divider label="命中后" labelPosition="left" />
        <ActionFields action={editing.action} onChange={(action) => setEditing({ ...editing, action })} siteId={siteId} />
        {editing.action.type === 'auth' ? <Group><Button variant="subtle" onClick={() => setAccountsOpened(true)}>账户管理</Button></Group> : null}
        {ruleError ? <Alert color="red">{ruleError}</Alert> : null}
        <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>取消</Button><Button onClick={finishEditing}>确认到草稿</Button></Group>
      </Stack> : null}
    </Modal>
    <AuthAccountManager siteId={siteId} opened={accountsOpened} onClose={() => setAccountsOpened(false)} />
  </SectionCard>
}
