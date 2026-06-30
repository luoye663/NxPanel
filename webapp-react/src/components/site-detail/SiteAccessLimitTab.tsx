import { ActionIcon, Alert, Badge, Button, Group, Modal, Select, Stack, Switch, Tabs, Text, TextInput, Textarea, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconBan, IconCheck, IconEdit, IconPlus, IconTrash, IconUsers } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useEffect, useState } from 'react'
import {
  createAuthRule,
  createDenyRule,
  createIPLimitRule,
  createHotlinkRule,
  deleteAuthRule,
  deleteDenyRule,
  deleteIPLimitRule,
  deleteHotlinkRule,
  listAuthRules,
  listDenyRules,
  listIPLimitRules,
  listHotlinkRules,
  updateAuthRule,
  updateDenyRule,
  updateIPLimitRule,
  updateHotlinkRule,
  type AuthRule,
  type DenyRule,
  type IPLimitRule,
  type HotlinkRule,
} from '@/api/accessLimit'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { MonoText } from '@/components/common/MonoText'
import { SectionCard } from '@/components/common/SectionCard'
import { DataTable } from '@/components/tables/DataTable'
import { AuthAccountManager, AuthAccountSelector, authAccountKeys } from './AuthAccountManager'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteAccessLimitTabProps {
  site: SiteDetail
  initialTab?: AccessSubTab
  singleTab?: boolean
}

interface AuthFormValues {
  name: string
  path: string
  account_ids: string[]
}

interface DenyFormValues {
  name: string
  extension_pattern: string
  path_pattern: string
}

interface IPLimitFormValues {
  name: string
  ruleType: 'allow' | 'deny'
  ipsText: string
}

interface HotlinkFormValues {
  name: string
  extensionsText: string
  referersText: string
  allow_empty_referer: boolean
  block_status: '403' | '404' | '444'
}

type AccessSubTab = 'auth' | 'deny' | 'ip-limit' | 'hotlink'

const defaultAuthForm: AuthFormValues = { name: '', path: '/', account_ids: [] }
const defaultDenyForm: DenyFormValues = { name: '', extension_pattern: '', path_pattern: '' }
const defaultIPLimitForm: IPLimitFormValues = { name: '', ruleType: 'allow', ipsText: '' }
const defaultHotlinkForm: HotlinkFormValues = { name: '', extensionsText: 'jpg, jpeg, png, gif, webp', referersText: 'server_names', allow_empty_referer: true, block_status: '403' }

export function SiteAccessLimitTab({ site, initialTab = 'auth', singleTab = false }: SiteAccessLimitTabProps) {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<AccessSubTab>(initialTab)
  const [authOpened, authHandlers] = useDisclosure(false)
  const [accountManagerOpened, accountManagerHandlers] = useDisclosure(false)
  const [denyOpened, denyHandlers] = useDisclosure(false)
  const [ipLimitOpened, ipLimitHandlers] = useDisclosure(false)
  const [hotlinkOpened, hotlinkHandlers] = useDisclosure(false)
  const [editingAuthRule, setEditingAuthRule] = useState<AuthRule | null>(null)
  const [editingDenyRule, setEditingDenyRule] = useState<DenyRule | null>(null)
  const [editingIPLimitRule, setEditingIPLimitRule] = useState<IPLimitRule | null>(null)
  const [editingHotlinkRule, setEditingHotlinkRule] = useState<HotlinkRule | null>(null)
  const authQueryKey = ['site-detail', site.id, 'auth-rules'] as const
  const denyQueryKey = ['site-detail', site.id, 'deny-rules'] as const
  const ipLimitQueryKey = ['site-detail', site.id, 'ip-limit-rules'] as const
  const hotlinkQueryKey = ['site-detail', site.id, 'hotlink-rules'] as const
  const authQuery = useQuery({ queryKey: authQueryKey, queryFn: () => listAuthRules(site.id), enabled: activeTab === 'auth' })
  const denyQuery = useQuery({ queryKey: denyQueryKey, queryFn: () => listDenyRules(site.id), enabled: activeTab === 'deny' })
  const ipLimitQuery = useQuery({ queryKey: ipLimitQueryKey, queryFn: () => listIPLimitRules(site.id), enabled: activeTab === 'ip-limit' })
  const hotlinkQuery = useQuery({ queryKey: hotlinkQueryKey, queryFn: () => listHotlinkRules(site.id), enabled: activeTab === 'hotlink' })
  const authSaveMutation = useMutation({
    mutationFn: (values: AuthFormValues) => editingAuthRule
      ? updateAuthRule(site.id, editingAuthRule.id, { name: values.name, path: values.path, account_ids: values.account_ids })
      : createAuthRule(site.id, { name: values.name, path: values.path, account_ids: values.account_ids }),
  })
  const denySaveMutation = useMutation({
    mutationFn: (values: DenyFormValues) => editingDenyRule
      ? updateDenyRule(site.id, editingDenyRule.id, values)
      : createDenyRule(site.id, values),
  })
  const ipLimitSaveMutation = useMutation({
    mutationFn: (values: IPLimitFormValues) => editingIPLimitRule
      ? updateIPLimitRule(site.id, editingIPLimitRule.id, { name: values.name, rule_type: values.ruleType, ips_text: values.ipsText })
      : createIPLimitRule(site.id, { name: values.name, rule_type: values.ruleType, ips_text: values.ipsText }),
  })
  const hotlinkSaveMutation = useMutation({
    mutationFn: (values: HotlinkFormValues) => {
      const data = {
        name: values.name,
        extensions: values.extensionsText.split(/[\n,]/).map((item) => item.trim()).filter(Boolean),
        referers: values.referersText.split(/[\n,]/).map((item) => item.trim()).filter(Boolean),
        allow_empty_referer: values.allow_empty_referer,
        block_status: Number(values.block_status) as 403 | 404 | 444,
      }
      return editingHotlinkRule ? updateHotlinkRule(site.id, editingHotlinkRule.id, data) : createHotlinkRule(site.id, data)
    },
  })
  const authToggleMutation = useMutation({ mutationFn: (rule: AuthRule) => updateAuthRule(site.id, rule.id, { enabled: !rule.enabled, account_ids: rule.account_ids || [] }) })
  const denyToggleMutation = useMutation({ mutationFn: (rule: DenyRule) => updateDenyRule(site.id, rule.id, { enabled: !rule.enabled }) })
  const hotlinkToggleMutation = useMutation({ mutationFn: (rule: HotlinkRule) => updateHotlinkRule(site.id, rule.id, { enabled: !rule.enabled }) })
  const authDeleteMutation = useMutation({ mutationFn: (ruleId: string) => deleteAuthRule(site.id, ruleId) })
  const denyDeleteMutation = useMutation({ mutationFn: (ruleId: string) => deleteDenyRule(site.id, ruleId) })
  const ipLimitDeleteMutation = useMutation({ mutationFn: (ruleId: string) => deleteIPLimitRule(site.id, ruleId) })
  const hotlinkDeleteMutation = useMutation({ mutationFn: (ruleId: string) => deleteHotlinkRule(site.id, ruleId) })
  const authForm = useForm<AuthFormValues>({
    initialValues: defaultAuthForm,
    validate: {
      name: (value) => value.trim() ? null : '请填写名称',
      path: (value) => value.trim() ? null : '请填写路径',
      account_ids: (value) => value.length > 0 ? null : '请选择至少一个账户',
    },
  })
  const denyForm = useForm<DenyFormValues>({
    initialValues: defaultDenyForm,
    validate: {
      name: (value) => value.trim() ? null : '请填写名称',
      extension_pattern: (value, values) => value.trim() || values.path_pattern.trim() ? null : '后缀和路径至少填写一项',
      path_pattern: (value, values) => value.trim() || values.extension_pattern.trim() ? null : '后缀和路径至少填写一项',
    },
  })
  const ipLimitForm = useForm<IPLimitFormValues>({
    initialValues: defaultIPLimitForm,
    validate: {
      name: (value) => value.trim() ? null : '请填写名称',
      ipsText: (value) => value.trim() ? null : '请填写至少一个 IP 或 CIDR',
    },
  })
  const hotlinkForm = useForm<HotlinkFormValues>({
    initialValues: defaultHotlinkForm,
    validate: {
      name: (value) => value.trim() ? null : '请填写名称',
      extensionsText: (value) => value.trim() ? null : '请填写至少一个文件后缀',
    },
  })

  useEffect(() => {
    setActiveTab(initialTab)
  }, [initialTab, site.id])

  const authColumns: MRT_ColumnDef<AuthRule>[] = [
    { accessorKey: 'name', header: '名称', size: 140 },
    { accessorKey: 'path', header: '路径', size: 140, Cell: ({ cell }) => <MonoText value={cell.getValue<string>()} maxWidth={160} /> },
    { accessorKey: 'accounts', header: '账户', size: 120, Cell: ({ row }) => <Text size="sm">{row.original.accounts?.length || row.original.account_ids?.length || 0} 个</Text> },
    { accessorKey: 'enabled', header: '状态', size: 90, Cell: ({ row }) => <Badge color={row.original.enabled ? 'green' : 'gray'} variant="light">{row.original.enabled ? '启用' : '禁用'}</Badge> },
  ]
  const denyColumns: MRT_ColumnDef<DenyRule>[] = [
    { accessorKey: 'name', header: '名称', size: 140 },
    { accessorKey: 'extension_pattern', header: '后缀', Cell: ({ row }) => row.original.extension_pattern ? <MonoText value={row.original.extension_pattern} maxWidth={220} /> : <Text c="dimmed" size="sm">-</Text> },
    { accessorKey: 'path_pattern', header: '路径', Cell: ({ row }) => row.original.path_pattern ? <MonoText value={row.original.path_pattern} maxWidth={220} /> : <Text c="dimmed" size="sm">-</Text> },
    { accessorKey: 'enabled', header: '状态', size: 90, Cell: ({ row }) => <Badge color={row.original.enabled ? 'green' : 'gray'} variant="light">{row.original.enabled ? '启用' : '禁用'}</Badge> },
  ]
  const hotlinkColumns: MRT_ColumnDef<HotlinkRule>[] = [
    { accessorKey: 'name', header: '名称', size: 100, minSize: 80, maxSize: 120 },
    { accessorKey: 'extensions', header: '后缀', size: 150, minSize: 120, maxSize: 180, Cell: ({ row }) => <MonoText value={row.original.extensions.join(', ')} maxWidth={150} /> },
    { accessorKey: 'referers', header: 'Referer', size: 90, minSize: 80, maxSize: 110, Cell: ({ row }) => <Text size="sm">{row.original.referers.length || 0} 个</Text> },
    { accessorKey: 'allow_empty_referer', header: '空 Referer', size: 96, minSize: 88, maxSize: 110, Cell: ({ row }) => <Badge variant="light" color={row.original.allow_empty_referer ? 'green' : 'yellow'}>{row.original.allow_empty_referer ? '允许' : '拒绝'}</Badge> },
    { accessorKey: 'enabled', header: '状态', size: 72, minSize: 64, maxSize: 80, Cell: ({ row }) => <Badge color={row.original.enabled ? 'green' : 'gray'} variant="light">{row.original.enabled ? '启用' : '禁用'}</Badge> },
  ]

  function openAuthDialog(rule?: AuthRule) {
    setEditingAuthRule(rule || null)
    authForm.setValues(rule ? { name: rule.name, path: rule.path, account_ids: rule.account_ids || [] } : defaultAuthForm)
    authForm.clearErrors()
    authHandlers.open()
  }

  function openDenyDialog(rule?: DenyRule) {
    setEditingDenyRule(rule || null)
    denyForm.setValues(rule ? { name: rule.name, extension_pattern: rule.extension_pattern || '', path_pattern: rule.path_pattern || '' } : defaultDenyForm)
    denyForm.clearErrors()
    denyHandlers.open()
  }

  function openIPLimitDialog(rule?: IPLimitRule) {
    setEditingIPLimitRule(rule || null)
    ipLimitForm.setValues(rule ? { name: rule.name, ruleType: rule.rule_type, ipsText: rule.ips.join('\n') } : defaultIPLimitForm)
    ipLimitForm.clearErrors()
    ipLimitHandlers.open()
  }

  function openHotlinkDialog(rule?: HotlinkRule) {
    setEditingHotlinkRule(rule || null)
    hotlinkForm.setValues(rule ? {
      name: rule.name,
      extensionsText: rule.extensions.join(', '),
      referersText: rule.referers.join('\n'),
      allow_empty_referer: rule.allow_empty_referer,
      block_status: String(rule.block_status) as '403' | '404' | '444',
    } : defaultHotlinkForm)
    hotlinkForm.clearErrors()
    hotlinkHandlers.open()
  }

  async function saveAuth(values: AuthFormValues) {
    try {
      await authSaveMutation.mutateAsync({ name: values.name.trim(), path: values.path.trim(), account_ids: values.account_ids })
      notifySuccess({ message: editingAuthRule ? '规则已更新' : '规则已创建' })
      authHandlers.close()
      await queryClient.invalidateQueries({ queryKey: authQueryKey })
      await queryClient.invalidateQueries({ queryKey: authAccountKeys.list(site.id) })
    } catch (error) {
      showErrorModal(error, editingAuthRule ? '保存加密访问规则失败' : '创建加密访问规则失败')
    }
  }

  async function saveDeny(values: DenyFormValues) {
    try {
      await denySaveMutation.mutateAsync({ name: values.name.trim(), extension_pattern: values.extension_pattern.trim(), path_pattern: values.path_pattern.trim() })
      notifySuccess({ message: editingDenyRule ? '规则已更新' : '规则已创建' })
      denyHandlers.close()
      await queryClient.invalidateQueries({ queryKey: denyQueryKey })
    } catch (error) {
      showErrorModal(error, editingDenyRule ? '保存禁止访问规则失败' : '创建禁止访问规则失败')
    }
  }

  async function saveIPLimit(values: IPLimitFormValues) {
    try {
      await ipLimitSaveMutation.mutateAsync({ name: values.name.trim(), ruleType: values.ruleType, ipsText: values.ipsText.trim() })
      notifySuccess({ message: editingIPLimitRule ? 'IP 限制已更新' : 'IP 限制已创建' })
      ipLimitHandlers.close()
      await queryClient.invalidateQueries({ queryKey: ipLimitQueryKey })
    } catch (error) {
      showErrorModal(error, editingIPLimitRule ? '保存 IP 限制失败' : '创建 IP 限制失败')
    }
  }

  async function saveHotlink(values: HotlinkFormValues) {
    try {
      await hotlinkSaveMutation.mutateAsync(values)
      notifySuccess({ message: editingHotlinkRule ? '防盗链规则已更新' : '防盗链规则已创建' })
      hotlinkHandlers.close()
      await hotlinkQuery.refetch()
    } catch (error) {
      showErrorModal(error, editingHotlinkRule ? '保存防盗链规则失败' : '创建防盗链规则失败')
    }
  }

  async function toggleAuth(rule: AuthRule) {
    try {
      await authToggleMutation.mutateAsync(rule)
      notifySuccess({ message: rule.enabled ? '已禁用' : '已启用' })
      await queryClient.invalidateQueries({ queryKey: authQueryKey })
    } catch (error) {
      showErrorModal(error, rule.enabled ? '禁用加密访问规则失败' : '启用加密访问规则失败')
    }
  }

  async function toggleDeny(rule: DenyRule) {
    try {
      await denyToggleMutation.mutateAsync(rule)
      notifySuccess({ message: rule.enabled ? '已禁用' : '已启用' })
      await queryClient.invalidateQueries({ queryKey: denyQueryKey })
    } catch (error) {
      showErrorModal(error, rule.enabled ? '禁用禁止访问规则失败' : '启用禁止访问规则失败')
    }
  }

  async function toggleIPLimit(rule: IPLimitRule) {
    try {
      await updateIPLimitRule(site.id, rule.id, { enabled: !rule.enabled })
      notifySuccess({ message: rule.enabled ? '已禁用' : '已启用' })
      await queryClient.invalidateQueries({ queryKey: ipLimitQueryKey })
    } catch (error) {
      showErrorModal(error, rule.enabled ? '禁用 IP 限制失败' : '启用 IP 限制失败')
    }
  }

  async function toggleHotlink(rule: HotlinkRule) {
    try {
      await hotlinkToggleMutation.mutateAsync(rule)
      notifySuccess({ message: rule.enabled ? '已禁用' : '已启用' })
      await queryClient.invalidateQueries({ queryKey: hotlinkQueryKey })
    } catch (error) {
      showErrorModal(error, rule.enabled ? '禁用防盗链规则失败' : '启用防盗链规则失败')
    }
  }

  function deleteAuth(rule: AuthRule) {
    confirmDanger({
      title: '删除规则',
      message: `确认删除加密访问规则「${rule.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除加密访问规则失败',
      onConfirm: async () => {
        await authDeleteMutation.mutateAsync(rule.id)
        notifySuccess({ message: '规则已删除' })
        await queryClient.invalidateQueries({ queryKey: authQueryKey })
      },
    })
  }

  function deleteDeny(rule: DenyRule) {
    confirmDanger({
      title: '删除规则',
      message: `确认删除禁止访问规则「${rule.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除禁止访问规则失败',
      onConfirm: async () => {
        await denyDeleteMutation.mutateAsync(rule.id)
        notifySuccess({ message: '规则已删除' })
        await queryClient.invalidateQueries({ queryKey: denyQueryKey })
      },
    })
  }

  function deleteIPLimit(rule: IPLimitRule) {
    confirmDanger({
      title: '删除规则',
      message: `确认删除 IP 限制规则「${rule.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除 IP 限制规则失败',
      onConfirm: async () => {
        await ipLimitDeleteMutation.mutateAsync(rule.id)
        notifySuccess({ message: '规则已删除' })
        await queryClient.invalidateQueries({ queryKey: ipLimitQueryKey })
      },
    })
  }

  function deleteHotlink(rule: HotlinkRule) {
    confirmDanger({
      title: '删除防盗链规则',
      message: `确认删除防盗链规则「${rule.name}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除防盗链规则失败',
      onConfirm: async () => {
        await hotlinkDeleteMutation.mutateAsync(rule.id)
        notifySuccess({ message: '规则已删除' })
        await queryClient.invalidateQueries({ queryKey: hotlinkQueryKey })
      },
    })
  }

  return (
    <SectionCard>
      <Stack gap="md">
        <Tabs value={activeTab} onChange={(value) => setActiveTab((value as AccessSubTab) || initialTab)}>
          {!singleTab ? (
            <Tabs.List>
              <Tabs.Tab value="auth">加密访问</Tabs.Tab>
              <Tabs.Tab value="deny">禁止访问</Tabs.Tab>
              <Tabs.Tab value="ip-limit">IP 限制</Tabs.Tab>
            </Tabs.List>
          ) : null}

          <Tabs.Panel value="auth">
            <Stack gap="md">
              {authQuery.isError ? <ErrorAlert error={authQuery.error} title="加载加密访问规则失败" /> : null}
              <Group gap="xs">
                <Button leftSection={<IconPlus size={16} />} onClick={() => openAuthDialog()}>新增规则</Button>
                <Button variant="default" leftSection={<IconUsers size={16} />} onClick={accountManagerHandlers.open}>账户管理</Button>
              </Group>
              <DataTable
                columns={authColumns}
                data={authQuery.data || []}
                loading={authQuery.isLoading || authQuery.isFetching}
                emptyText="暂无加密访问规则"
                renderRowActions={({ row }) => (
                  <Group gap={4} wrap="nowrap">
                    <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => openAuthDialog(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
                    <Tooltip label={row.original.enabled ? '禁用' : '启用'}><ActionIcon variant="subtle" color={row.original.enabled ? 'yellow' : 'green'} loading={authToggleMutation.isPending} onClick={() => toggleAuth(row.original)} aria-label={row.original.enabled ? '禁用' : '启用'}>{row.original.enabled ? <IconBan size={16} /> : <IconCheck size={16} />}</ActionIcon></Tooltip>
                    <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={authDeleteMutation.isPending} onClick={() => deleteAuth(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
                  </Group>
                )}
              />
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="deny">
            <Stack gap="md">
              {denyQuery.isError ? <ErrorAlert error={denyQuery.error} title="加载禁止访问规则失败" /> : null}
              <Group gap="xs">
                <Button leftSection={<IconPlus size={16} />} onClick={() => openDenyDialog()}>新增规则</Button>
              </Group>
              <DataTable
                columns={denyColumns}
                data={denyQuery.data || []}
                loading={denyQuery.isLoading || denyQuery.isFetching}
                emptyText="暂无禁止访问规则"
                renderRowActions={({ row }) => (
                  <Group gap={4} wrap="nowrap">
                    <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => openDenyDialog(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
                    <Tooltip label={row.original.enabled ? '禁用' : '启用'}><ActionIcon variant="subtle" color={row.original.enabled ? 'yellow' : 'green'} loading={denyToggleMutation.isPending} onClick={() => toggleDeny(row.original)} aria-label={row.original.enabled ? '禁用' : '启用'}>{row.original.enabled ? <IconBan size={16} /> : <IconCheck size={16} />}</ActionIcon></Tooltip>
                    <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={denyDeleteMutation.isPending} onClick={() => deleteDeny(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
                  </Group>
                )}
              />
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="ip-limit">
            <Stack gap="md">
              {ipLimitQuery.isError ? <ErrorAlert error={ipLimitQuery.error} title="加载 IP 限制失败" /> : null}
              <Alert color="orange">这是站点级限制，会作用于整个 `server`。黑名单优先，白名单未命中的请求会直接被拒绝，不会默认放行 ACME。</Alert>
              <Group gap="xs">
                <Button leftSection={<IconPlus size={16} />} onClick={() => openIPLimitDialog()}>新增限制</Button>
              </Group>
              <DataTable
                columns={[
                  { accessorKey: 'name', header: '名称', size: 140 },
                  { accessorKey: 'rule_type', header: '类型', size: 96, Cell: ({ row }) => <Badge variant="light" color={row.original.rule_type === 'deny' ? 'red' : 'blue'}>{row.original.rule_type === 'deny' ? '黑名单' : '白名单'}</Badge> },
                  { accessorKey: 'ips', header: 'IP/CIDR', Cell: ({ row }) => <MonoText value={row.original.ips.join(', ')} maxWidth={320} /> },
                  { accessorKey: 'enabled', header: '状态', size: 90, Cell: ({ row }) => <Badge color={row.original.enabled ? 'green' : 'gray'} variant="light">{row.original.enabled ? '启用' : '禁用'}</Badge> },
                ]}
                data={ipLimitQuery.data || []}
                loading={ipLimitQuery.isLoading || ipLimitQuery.isFetching}
                emptyText="暂无 IP 限制"
                renderRowActions={({ row }) => (
                  <Group gap={4} wrap="nowrap">
                    <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => openIPLimitDialog(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
                    <Tooltip label={row.original.enabled ? '禁用' : '启用'}><ActionIcon variant="subtle" color={row.original.enabled ? 'yellow' : 'green'} loading={ipLimitSaveMutation.isPending} onClick={() => toggleIPLimit(row.original)} aria-label={row.original.enabled ? '禁用' : '启用'}>{row.original.enabled ? <IconBan size={16} /> : <IconCheck size={16} />}</ActionIcon></Tooltip>
                    <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={ipLimitDeleteMutation.isPending} onClick={() => deleteIPLimit(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
                  </Group>
                )}
              />
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="hotlink" pt={singleTab ? 0 : 'md'}>
            <Stack gap="md">
              {hotlinkQuery.isError ? <ErrorAlert error={hotlinkQuery.error} title="加载防盗链规则失败" /> : null}
              <Alert color="blue">Referer 白名单可填写域名、*.example.com、server_names 或 blocked；规则保存后会重写独立 hotlink include。</Alert>
              <DataTable
                columns={hotlinkColumns}
                data={hotlinkQuery.data || []}
                loading={hotlinkQuery.isLoading || hotlinkQuery.isFetching}
                emptyText="暂无防盗链规则"
                toolbarActions={<Button leftSection={<IconPlus size={16} />} onClick={() => openHotlinkDialog()}>新增防盗链</Button>}
                renderRowActions={({ row }) => (
                  <Group gap={4} wrap="nowrap">
                    <Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => openHotlinkDialog(row.original)}><IconEdit size={16} /></ActionIcon></Tooltip>
                    <Tooltip label={row.original.enabled ? '禁用' : '启用'}><ActionIcon variant="subtle" color={row.original.enabled ? 'yellow' : 'green'} loading={hotlinkToggleMutation.isPending} onClick={() => toggleHotlink(row.original)} aria-label={row.original.enabled ? '禁用' : '启用'}>{row.original.enabled ? <IconBan size={16} /> : <IconCheck size={16} />}</ActionIcon></Tooltip>
                    <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={hotlinkDeleteMutation.isPending} onClick={() => deleteHotlink(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
                  </Group>
                )}
              />
            </Stack>
          </Tabs.Panel>
        </Tabs>

        <Modal opened={authOpened} onClose={authHandlers.close} title={editingAuthRule ? '编辑加密访问规则' : '新增加密访问规则'} size="md" closeOnClickOutside={false} centered>
          <form onSubmit={authForm.onSubmit(saveAuth)}>
            <Stack gap="md">
              <TextInput label="名称" placeholder="如：后台管理" {...authForm.getInputProps('name')} />
              <TextInput label="路径" placeholder="/admin" {...authForm.getInputProps('path')} />
              <Stack gap="xs">
                <Group justify="space-between"><Text size="sm" fw={500}>账户</Text><Button size="xs" variant="subtle" leftSection={<IconUsers size={14} />} onClick={accountManagerHandlers.open}>账户管理</Button></Group>
                <AuthAccountSelector siteId={site.id} value={authForm.values.account_ids} onChange={(value) => authForm.setFieldValue('account_ids', value)} />
                {authForm.errors.account_ids ? <Text size="xs" c="red">{authForm.errors.account_ids}</Text> : null}
              </Stack>
              <Group justify="flex-end"><Button variant="default" onClick={authHandlers.close}>取消</Button><Button type="submit" loading={authSaveMutation.isPending}>保存</Button></Group>
            </Stack>
          </form>
        </Modal>

        <AuthAccountManager siteId={site.id} opened={accountManagerOpened} onClose={accountManagerHandlers.close} />

        <Modal opened={denyOpened} onClose={denyHandlers.close} title={editingDenyRule ? '编辑禁止访问规则' : '新增禁止访问规则'} size="md" closeOnClickOutside={false} centered>
          <form onSubmit={denyForm.onSubmit(saveDeny)}>
            <Stack gap="md">
              <TextInput label="名称" placeholder="如：禁止备份文件" {...denyForm.getInputProps('name')} />
              <TextInput label="后缀" placeholder=".bak, .sql, .zip" description="多个后缀用逗号分隔，如 .bak, .sql, .zip。" {...denyForm.getInputProps('extension_pattern')} />
              <TextInput label="路径" placeholder="/admin" description="匹配的访问路径，如 /admin、/private。" {...denyForm.getInputProps('path_pattern')} />
              {/* 禁止访问支持按后缀或路径任一维度匹配，两项都为空会生成无意义规则。 */}
              <Alert color="blue">后缀和路径至少填写一项，也可同时填写。</Alert>
              <Group justify="flex-end"><Button variant="default" onClick={denyHandlers.close}>取消</Button><Button type="submit" loading={denySaveMutation.isPending}>保存</Button></Group>
            </Stack>
          </form>
        </Modal>

        <Modal opened={ipLimitOpened} onClose={ipLimitHandlers.close} title={editingIPLimitRule ? '编辑 IP 限制' : '新增 IP 限制'} size="md" closeOnClickOutside={false} centered>
          <form onSubmit={ipLimitForm.onSubmit(saveIPLimit)}>
            <Stack gap="md">
              <TextInput label="名称" placeholder="如：办公网络" {...ipLimitForm.getInputProps('name')} />
              <Select label="类型" data={[{ value: 'allow', label: '白名单' }, { value: 'deny', label: '黑名单' }]} allowDeselect={false} {...ipLimitForm.getInputProps('ruleType')} />
              <Textarea label="IP / CIDR" minRows={6} placeholder={'1.2.3.4\n1.2.3.0/24\n2001:db8::/32'} description="支持单个 IP、CIDR 和 IPv6，多条可换行或逗号分隔。" {...ipLimitForm.getInputProps('ipsText')} />
              <Alert color="orange">黑名单会优先于白名单；白名单未命中的请求会直接被拒绝。</Alert>
              <Group justify="flex-end"><Button variant="default" onClick={ipLimitHandlers.close}>取消</Button><Button type="submit" loading={ipLimitSaveMutation.isPending}>保存</Button></Group>
            </Stack>
          </form>
        </Modal>

        <Modal opened={hotlinkOpened} onClose={hotlinkHandlers.close} title={editingHotlinkRule ? '编辑防盗链规则' : '新增防盗链规则'} size="md" closeOnClickOutside={false} centered>
          <form onSubmit={hotlinkForm.onSubmit(saveHotlink)}>
            <Stack gap="md">
              <TextInput label="名称" placeholder="如：图片防盗链" {...hotlinkForm.getInputProps('name')} />
              <Textarea label="文件后缀" minRows={2} description="多个后缀用逗号或换行分隔，保存时会自动去掉开头的点。" {...hotlinkForm.getInputProps('extensionsText')} />
              <Textarea label="Referer 白名单" minRows={4} placeholder={'server_names\n*.example.com'} description="支持 server_names、blocked、example.com、*.example.com，不要填写协议或路径。" {...hotlinkForm.getInputProps('referersText')} />
              <Switch label="允许空 Referer" description="关闭后，直接输入图片 URL 或无 Referer 下载会被拦截。" {...hotlinkForm.getInputProps('allow_empty_referer', { type: 'checkbox' })} />
              {/* 防盗链只生成 valid_referers 和 return，不接受任意 Nginx 片段。 */}
              <Select label="拦截状态码" data={[{ value: '403', label: '403 Forbidden' }, { value: '404', label: '404 Not Found' }, { value: '444', label: '444 关闭连接' }]} allowDeselect={false} {...hotlinkForm.getInputProps('block_status')} />
              <Group justify="flex-end"><Button variant="default" onClick={hotlinkHandlers.close}>取消</Button><Button type="submit" loading={hotlinkSaveMutation.isPending}>保存</Button></Group>
            </Stack>
          </form>
        </Modal>
      </Stack>
    </SectionCard>
  )
}
