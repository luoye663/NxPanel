import { ActionIcon, Alert, Badge, Button, Group, LoadingOverlay, Modal, MultiSelect, Select, Stack, Switch, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconBan, IconCheck, IconEdit, IconPlus, IconTrash } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useEffect, useMemo, useState } from 'react'
import {
  createGeoRule, deleteGeoRule, disableSiteGeoAccess, enableSiteGeoAccess, geoAccessKeys, getGeoIPStatus,
  getSiteGeoAccess, listGeoRules, reorderGeoRules, updateGeoRule, updateSiteGeoAccess,
  type GeoAction, type GeoRule,
} from '@/api/geoAccess'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface Props { siteId: string }
interface RuleFormValues { name: string; countries: string[]; action: GeoAction; enabled: boolean }

const actionOptions = [
  { value: 'allow', label: '允许访问' },
  { value: 'deny_403', label: '拒绝（403）' },
  { value: 'deny_444', label: '关闭连接（444）' },
]

const actionLabel: Record<GeoAction, string> = { allow: '允许', deny_403: '拒绝 403', deny_444: '关闭 444' }

function countryName(code: string) {
  if (code === 'ZZ') return '未知地区 (ZZ)'
  try { return `${new Intl.DisplayNames(['zh-CN'], { type: 'region' }).of(code) || code} (${code})` } catch { return code }
}

export function GeoAccessPanel({ siteId }: Props) {
  const queryClient = useQueryClient()
  const [opened, handlers] = useDisclosure(false)
  const [editing, setEditing] = useState<GeoRule | null>(null)
  const [defaultAction, setDefaultAction] = useState<GeoAction>('allow')
  const accessQuery = useQuery({ queryKey: geoAccessKeys.site(siteId), queryFn: () => getSiteGeoAccess(siteId) })
  const rulesQuery = useQuery({ queryKey: geoAccessKeys.rules(siteId), queryFn: () => listGeoRules(siteId) })
  const statusQuery = useQuery({ queryKey: geoAccessKeys.status, queryFn: getGeoIPStatus })
  const form = useForm<RuleFormValues>({
    initialValues: { name: '', countries: [], action: 'allow', enabled: true },
    validate: { name: (value) => value.trim() ? null : '请填写名称', countries: (value) => value.length ? null : '请选择至少一个国家或地区' },
  })

  useEffect(() => { if (accessQuery.data) setDefaultAction(accessQuery.data.default_action) }, [accessQuery.data])
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
    mutationFn: () => updateSiteGeoAccess(siteId, defaultAction),
    onSuccess: async () => { notifySuccess({ message: '默认动作已保存' }); await invalidate() },
    onError: (error) => showErrorModal(error, '保存默认动作失败'),
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
    form.setValues(rule ? { name: rule.name, countries: rule.countries, action: rule.action, enabled: rule.enabled } : { name: '', countries: [], action: 'allow', enabled: true })
    form.clearErrors()
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
      <Group justify="space-between" align="end">
        <Switch checked={accessQuery.data?.enabled || false} disabled={!statusQuery.data?.installed || toggleSiteMutation.isPending} onChange={(event) => requestSiteToggle(event.currentTarget.checked)} label="启用地域访问" description="默认关闭；只有启用后才写入并加载 Nginx 配置。" />
        <Group align="end">
          <Select label="未命中规则时" data={actionOptions} value={defaultAction} onChange={(value) => setDefaultAction((value || 'allow') as GeoAction)} allowDeselect={false} />
          <Button variant="default" loading={defaultMutation.isPending} disabled={defaultAction === accessQuery.data?.default_action} onClick={() => defaultMutation.mutate()}>保存默认动作</Button>
        </Group>
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
      <Modal opened={opened} onClose={handlers.close} title={editing ? '编辑地域规则' : '新增地域规则'} size="lg" centered closeOnClickOutside={false}>
        <form onSubmit={form.onSubmit(saveRule)}>
          <Stack gap="md">
            <TextInput label="名称" maxLength={100} placeholder="如：放行XX" {...form.getInputProps('name')} />
            <MultiSelect label="国家/地区" data={countries} searchable clearable nothingFoundMessage="没有匹配项" maxDropdownHeight={300} {...form.getInputProps('countries')} />
            <Select label="动作" data={actionOptions} allowDeselect={false} {...form.getInputProps('action')} />
            <Switch label="立即启用此规则" {...form.getInputProps('enabled', { type: 'checkbox' })} />
            <Group justify="flex-end"><Button variant="default" onClick={handlers.close}>取消</Button><Button type="submit" loading={saveMutation.isPending}>保存</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  )
}
