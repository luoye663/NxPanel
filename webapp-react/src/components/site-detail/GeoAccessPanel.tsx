import { ActionIcon, Alert, Badge, Button, Group, Loader, LoadingOverlay, Modal, MultiSelect, NumberInput, SegmentedControl, Select, SimpleGrid, Stack, Switch, Text, Textarea, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure, useMediaQuery } from '@mantine/hooks'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconBan, IconCheck, IconDeviceFloppy, IconEdit, IconPlus, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import {
  createGeoRule, deleteGeoRule, disableSiteGeoAccess, enableSiteGeoAccess, geoAccessKeys, getGeoIPStatus,
  getSiteGeoAccess, listGeoRules, reorderGeoRules, updateGeoRule, updateSiteGeoAccess,
  type GeoAction, type GeoDefaultAction, type GeoResponseType, type GeoRule,
} from '@/api/geoAccess'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

interface Props { siteId: string }
interface RuleFormValues { name: string; countries: string[]; action: GeoAction; enabled: boolean }
interface DefaultFormValues {
  default_action: GeoDefaultAction
  default_status_code: number
  default_response_type: GeoResponseType
  default_response_body: string
}

const ruleActionOptions = [
  { value: 'allow', label: '允许访问' },
  { value: 'deny_403', label: '拒绝（403）' },
  { value: 'deny_444', label: '关闭连接（444）' },
]

const defaultActionOptions = [
  { value: 'allow', label: '允许访问' },
  { value: 'respond', label: '返回自定义响应' },
]

const commonStatusCodes = ['400', '401', '403', '404', '410', '418', '429', '444', '451', '500', '502', '503', '504']
const statusOptions = [
  ...commonStatusCodes.map((code) => ({ value: code, label: code === '444' ? '444 - 直接关闭连接' : code })),
  { value: 'custom', label: '自定义状态码' },
]

const actionLabel: Record<GeoAction, string> = { allow: '允许', deny_403: '拒绝 403', deny_444: '关闭 444' }

function countryName(code: string) {
  if (code === 'ZZ') return '未知地区 (ZZ)'
  try { return `${new Intl.DisplayNames(['zh-CN'], { type: 'region' }).of(code) || code} (${code})` } catch { return code }
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KiB`
}

export function GeoAccessPanel({ siteId }: Props) {
  const queryClient = useQueryClient()
  const mobile = useMediaQuery('(max-width: 48em)')
  const [opened, handlers] = useDisclosure(false)
  const [responseOpened, responseHandlers] = useDisclosure(false)
  const [editing, setEditing] = useState<GeoRule | null>(null)
  const [statusMode, setStatusMode] = useState('403')
  const accessQuery = useQuery({ queryKey: geoAccessKeys.site(siteId), queryFn: () => getSiteGeoAccess(siteId) })
  const rulesQuery = useQuery({ queryKey: geoAccessKeys.rules(siteId), queryFn: () => listGeoRules(siteId) })
  const statusQuery = useQuery({ queryKey: geoAccessKeys.status, queryFn: getGeoIPStatus })
  const ruleForm = useForm<RuleFormValues>({
    initialValues: { name: '', countries: [], action: 'allow', enabled: true },
    validate: { name: (value) => value.trim() ? null : '请填写名称', countries: (value) => value.length ? null : '请选择至少一个国家或地区' },
  })
  const defaultForm = useForm<DefaultFormValues>({
    initialValues: { default_action: 'allow', default_status_code: 403, default_response_type: 'text', default_response_body: '' },
    validate: {
      default_status_code: (value) => Number.isInteger(value) && value >= 400 && value <= 599 ? null : '状态码必须是 400 到 599 之间的整数',
      default_response_body: (value, values) => values.default_action !== 'respond' || values.default_status_code === 444 || new TextEncoder().encode(value).byteLength <= 64 * 1024 ? null : '返回内容不能超过 64 KiB',
    },
  })

  useEffect(() => {
    if (!accessQuery.data || responseOpened) return
    const values: DefaultFormValues = {
      default_action: accessQuery.data.default_action,
      default_status_code: accessQuery.data.default_status_code,
      default_response_type: accessQuery.data.default_response_type,
      default_response_body: accessQuery.data.default_response_body,
    }
    defaultForm.setValues(values)
    defaultForm.resetDirty(values)
    setStatusMode(commonStatusCodes.includes(String(values.default_status_code)) ? String(values.default_status_code) : 'custom')
  }, [accessQuery.data, responseOpened])
  const countries = useMemo(() => (statusQuery.data?.countries || []).map((code) => ({ value: code, label: countryName(code) })), [statusQuery.data?.countries])
  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: geoAccessKeys.site(siteId) }),
      queryClient.invalidateQueries({ queryKey: geoAccessKeys.rules(siteId) }),
      queryClient.invalidateQueries({ queryKey: geoAccessKeys.status }),
    ])
  }
  const toggleSiteMutation = useMutation({
    mutationFn: (enabled: boolean) => enabled ? enableSiteGeoAccess(siteId) : disableSiteGeoAccess(siteId),
    onSuccess: async (_, enabled) => { notifySuccess({ message: enabled ? '地域访问已启用' : '地域访问已关闭，Nginx 配置已移除' }); await invalidate() },
    onError: (error, enabled) => showErrorModal(error, enabled ? '启用地域访问失败' : '关闭地域访问失败'),
  })
  const defaultMutation = useMutation({
    mutationFn: (values: DefaultFormValues) => updateSiteGeoAccess(siteId, values),
    onSuccess: async () => { responseHandlers.close(); notifySuccess({ message: '未命中响应已保存' }); await invalidate() },
    onError: (error) => showErrorModal(error, '保存未命中响应失败'),
  })
  const saveMutation = useMutation({
    mutationFn: (values: RuleFormValues) => editing
      ? updateGeoRule(siteId, editing.id, values)
      : createGeoRule(siteId, values),
  })
  const toggleRuleMutation = useMutation({ mutationFn: (rule: GeoRule) => updateGeoRule(siteId, rule.id, { enabled: !rule.enabled }) })
  const deleteMutation = useMutation({ mutationFn: (id: string) => deleteGeoRule(siteId, id) })
  const reorderMutation = useMutation({ mutationFn: (ids: string[]) => reorderGeoRules(siteId, ids) })

  function openRule(rule?: GeoRule) {
    setEditing(rule || null)
    ruleForm.setValues(rule ? { name: rule.name, countries: rule.countries, action: rule.action, enabled: rule.enabled } : { name: '', countries: [], action: 'allow', enabled: true })
    ruleForm.clearErrors()
    handlers.open()
  }
  async function saveRule(values: RuleFormValues) {
    try {
      await saveMutation.mutateAsync({ ...values, name: values.name.trim() })
      notifySuccess({ message: editing ? '地域规则已更新' : '地域规则已创建' })
      handlers.close()
      await invalidate()
    } catch (error) { showErrorModal(error, editing ? '保存地域规则失败' : '创建地域规则失败') }
  }
  async function toggleRule(rule: GeoRule) {
    try { await toggleRuleMutation.mutateAsync(rule); notifySuccess({ message: rule.enabled ? '规则已禁用' : '规则已启用' }); await invalidate() }
    catch (error) { showErrorModal(error, rule.enabled ? '禁用地域规则失败' : '启用地域规则失败') }
  }
  function removeRule(rule: GeoRule) {
    confirmDanger({ title: '删除地域规则', message: `确认删除「${rule.name}」？`, confirmLabel: '确认删除', errorTitle: '删除地域规则失败', onConfirm: async () => { await deleteMutation.mutateAsync(rule.id); notifySuccess({ message: '地域规则已删除' }); await invalidate() } })
  }
  async function moveRule(index: number, direction: -1 | 1) {
    const rules = rulesQuery.data || []
    const target = index + direction
    if (target < 0 || target >= rules.length) return
    const ids = rules.map((rule) => rule.id)
    ;[ids[index], ids[target]] = [ids[target], ids[index]]
    try { await reorderMutation.mutateAsync(ids); await invalidate() } catch (error) { showErrorModal(error, '调整规则顺序失败') }
  }
  function requestSiteToggle(enabled: boolean) {
    if (enabled) { toggleSiteMutation.mutate(true); return }
    confirmDanger({ title: '关闭地域访问', message: '关闭后会删除此站点的地域 include；若没有其他启用站点，也会删除全局地域配置。', confirmLabel: '确认关闭', errorTitle: '关闭地域访问失败', onConfirm: async () => { await toggleSiteMutation.mutateAsync(false) } })
  }

  function changeStatusMode(value: string | null) {
    if (!value) return
    setStatusMode(value)
    if (value !== 'custom') defaultForm.setFieldValue('default_status_code', Number(value))
  }

  function openResponseEditor() {
    if (!accessQuery.data) return
    const values: DefaultFormValues = {
      default_action: accessQuery.data.default_action,
      default_status_code: accessQuery.data.default_status_code,
      default_response_type: accessQuery.data.default_response_type,
      default_response_body: accessQuery.data.default_response_body,
    }
    defaultForm.setValues(values)
    defaultForm.resetDirty(values)
    defaultForm.clearErrors()
    setStatusMode(commonStatusCodes.includes(String(values.default_status_code)) ? String(values.default_status_code) : 'custom')
    responseHandlers.open()
  }

  function requestResponseClose() {
    if (defaultMutation.isPending) return
    if (!defaultForm.isDirty()) {
      responseHandlers.close()
      return
    }
    modals.openConfirmModal({
      title: '放弃未保存的更改？',
      children: '未命中响应已修改，关闭后本次更改不会保存。',
      labels: { confirm: '放弃更改', cancel: '继续编辑' },
      confirmProps: { color: 'red' },
      onConfirm: responseHandlers.close,
    })
  }

  const responseBodyBytes = useMemo(() => new TextEncoder().encode(defaultForm.values.default_response_body).byteLength, [defaultForm.values.default_response_body])
  const customResponseEnabled = defaultForm.values.default_action === 'respond'
  const connectionCloseSelected = customResponseEnabled && defaultForm.values.default_status_code === 444
  const responseBodyTooLarge = customResponseEnabled && !connectionCloseSelected && responseBodyBytes > 64 * 1024
  const savedResponseBytes = useMemo(() => new TextEncoder().encode(accessQuery.data?.default_response_body || '').byteLength, [accessQuery.data?.default_response_body])

  const columns: MRT_ColumnDef<GeoRule>[] = [
    { accessorKey: 'sort_order', header: '优先级', size: 76, Cell: ({ row }) => <Text size="sm">{row.index + 1}</Text> },
    { accessorKey: 'name', header: '名称', size: 150 },
    { accessorKey: 'countries', header: '国家/地区', Cell: ({ row }) => <Text size="sm" lineClamp={2}>{row.original.countries.map(countryName).join('、')}</Text> },
    { accessorKey: 'action', header: '动作', size: 110, Cell: ({ row }) => <Badge color={row.original.action === 'allow' ? 'green' : 'red'} variant="light">{actionLabel[row.original.action]}</Badge> },
    { accessorKey: 'enabled', header: '状态', size: 78, Cell: ({ row }) => <Badge color={row.original.enabled ? 'green' : 'gray'} variant="light">{row.original.enabled ? '启用' : '禁用'}</Badge> },
  ]

  return (
    <Stack gap="md" pos="relative">
      <LoadingOverlay visible={accessQuery.isLoading || statusQuery.isLoading} />
      {(accessQuery.isError || rulesQuery.isError || statusQuery.isError) ? <ErrorAlert error={accessQuery.error || rulesQuery.error || statusQuery.error} title="加载地域访问策略失败" /> : null}
      {!statusQuery.data?.installed ? <Alert color="orange">需要先在“面板设置 - GeoIP”安装 GeoLite2 Country 数据库，才能启用站点策略。</Alert> : null}
      {accessQuery.data?.last_error ? <Alert color="red">{accessQuery.data.last_error}</Alert> : null}
      <Group justify="space-between" align="center">
        <Switch checked={accessQuery.data?.enabled || false} disabled={!statusQuery.data?.installed || toggleSiteMutation.isPending} onChange={(event) => requestSiteToggle(event.currentTarget.checked)} label="启用地域访问" description="默认关闭；只有启用后才写入并加载 Nginx 配置。" />
      </Group>
      <Group justify="space-between" align="flex-start" gap="md" wrap="wrap">
        <Stack gap={5}>
          <Text size="sm" fw={500}>未命中规则时</Text>
          <Group gap="xs" wrap="wrap">
            {accessQuery.data && accessQuery.data.default_action !== 'respond' ? <Badge color="green" variant="light">允许访问</Badge> : null}
            {accessQuery.data?.default_action === 'respond' ? <Badge color="red" variant="light">{accessQuery.data.default_status_code}</Badge> : null}
            {accessQuery.data?.default_action === 'respond' && accessQuery.data.default_status_code === 444 ? <Text size="sm" c="dimmed">直接关闭连接</Text> : null}
            {accessQuery.data?.default_action === 'respond' && accessQuery.data.default_status_code !== 444 ? (
              <>
                <Badge color="gray" variant="light">{accessQuery.data.default_response_type === 'html' ? 'HTML' : 'TXT'}</Badge>
                <Text size="sm" c="dimmed">{savedResponseBytes === 0 ? '空内容' : formatBytes(savedResponseBytes)}</Text>
              </>
            ) : null}
          </Group>
        </Stack>
        <Button variant="default" size="compact-sm" leftSection={<IconEdit size={15} />} disabled={!accessQuery.data} onClick={openResponseEditor}>配置响应</Button>
      </Group>
      <Alert color="blue">规则按从上到下的顺序匹配，首条命中生效。IP 黑名单优先于白名单，白名单优先于地域规则。</Alert>
      <DataTable
        columns={columns}
        data={rulesQuery.data || []}
        loading={rulesQuery.isLoading || rulesQuery.isFetching}
        emptyText="暂无地域规则"
        toolbarActions={<Button leftSection={<IconPlus size={16} />} onClick={() => openRule()}>新增规则</Button>}
        renderRowActions={({ row }) => (
          <Group gap={2} wrap="nowrap">
            <Tooltip label="上移"><ActionIcon variant="subtle" disabled={row.index === 0 || reorderMutation.isPending} onClick={() => moveRule(row.index, -1)}><IconArrowUp size={16} /></ActionIcon></Tooltip>
            <Tooltip label="下移"><ActionIcon variant="subtle" disabled={row.index === (rulesQuery.data?.length || 0) - 1 || reorderMutation.isPending} onClick={() => moveRule(row.index, 1)}><IconArrowDown size={16} /></ActionIcon></Tooltip>
            <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => openRule(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
            <Tooltip label={row.original.enabled ? '禁用' : '启用'}><ActionIcon variant="subtle" color={row.original.enabled ? 'yellow' : 'green'} loading={toggleRuleMutation.isPending} onClick={() => toggleRule(row.original)}>{row.original.enabled ? <IconBan size={16} /> : <IconCheck size={16} />}</ActionIcon></Tooltip>
            <Tooltip label="删除"><ActionIcon variant="subtle" color="red" onClick={() => removeRule(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
          </Group>
        )}
      />
      <Modal
        opened={responseOpened}
        onClose={requestResponseClose}
        title="配置未命中响应"
        size="xl"
        fullScreen={mobile}
        centered
        closeOnClickOutside={false}
        closeOnEscape={!defaultMutation.isPending}
        withCloseButton={!defaultMutation.isPending}
      >
        <form onSubmit={defaultForm.onSubmit((values) => defaultMutation.mutate(values))}>
          <Stack gap="md">
            <Select label="未命中规则时" data={defaultActionOptions} allowDeselect={false} {...defaultForm.getInputProps('default_action')} />
            {customResponseEnabled ? (
              <SimpleGrid cols={{ base: 1, sm: statusMode === 'custom' ? 2 : 1 }}>
                <Select label="返回状态码" data={statusOptions} value={statusMode} onChange={changeStatusMode} allowDeselect={false} searchable />
                {statusMode === 'custom' ? <NumberInput label="自定义状态码" min={400} max={599} allowDecimal={false} clampBehavior="strict" {...defaultForm.getInputProps('default_status_code')} /> : null}
              </SimpleGrid>
            ) : null}
            {connectionCloseSelected ? (
              <Alert color="orange">444 会由 Nginx 直接关闭连接，不发送 HTTP 状态行、响应头或响应内容。</Alert>
            ) : null}
            {customResponseEnabled && !connectionCloseSelected ? (
              <Stack gap="xs">
                <SegmentedControl
                  aria-label="返回内容类型"
                  data={[{ value: 'html', label: 'HTML' }, { value: 'text', label: 'TXT' }]}
                  {...defaultForm.getInputProps('default_response_type')}
                />
                {defaultForm.values.default_response_type === 'html' ? (
                  <Suspense fallback={<Group justify="center" py="xl"><Loader size="sm" /><Text size="sm" c="dimmed">正在加载编辑器...</Text></Group>}>
                    <NginxCodeEditor language="html" value={defaultForm.values.default_response_body} onChange={(value) => defaultForm.setFieldValue('default_response_body', value)} />
                  </Suspense>
                ) : (
                  <Textarea label="返回内容" autosize minRows={8} maxRows={18} placeholder="可留空" {...defaultForm.getInputProps('default_response_body')} />
                )}
                <Text size="xs" c={responseBodyTooLarge ? 'red' : 'dimmed'} ta="right" aria-live="polite">
                  {responseBodyBytes.toLocaleString()} / 65,536 字节
                </Text>
                {defaultForm.errors.default_response_body ? <Text size="xs" c="red">{defaultForm.errors.default_response_body}</Text> : null}
              </Stack>
            ) : null}
            <Group justify="flex-end">
              <Button variant="default" disabled={defaultMutation.isPending} onClick={requestResponseClose}>取消</Button>
              <Button type="submit" loading={defaultMutation.isPending} disabled={!defaultForm.isDirty() || responseBodyTooLarge} leftSection={<IconDeviceFloppy size={16} />}>保存</Button>
            </Group>
          </Stack>
        </form>
      </Modal>
      <Modal opened={opened} onClose={handlers.close} title={editing ? '编辑地域规则' : '新增地域规则'} size="lg" centered closeOnClickOutside={false}>
        <form onSubmit={ruleForm.onSubmit(saveRule)}>
          <Stack gap="md">
            <TextInput label="名称" maxLength={100} placeholder="如：放行XX" {...ruleForm.getInputProps('name')} />
            <MultiSelect label="国家/地区" data={countries} searchable clearable nothingFoundMessage="没有匹配项" maxDropdownHeight={300} {...ruleForm.getInputProps('countries')} />
            <Select label="动作" data={ruleActionOptions} allowDeselect={false} {...ruleForm.getInputProps('action')} />
            <Switch label="立即启用此规则" {...ruleForm.getInputProps('enabled', { type: 'checkbox' })} />
            <Group justify="flex-end"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={saveMutation.isPending}>保存</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  )
}
