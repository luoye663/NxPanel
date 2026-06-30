import { ActionIcon, Badge, Button, Checkbox, Group, Modal, PasswordInput, Radio, ScrollArea, Stack, Switch, Table, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEdit, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { createAuthAccount, deleteAuthAccount, listAuthAccounts, updateAuthAccount, type AuthAccount } from '@/api/accessLimit'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface AuthAccountManagerProps {
  siteId: string
  opened: boolean
  onClose: () => void
}

interface AuthAccountSelectorProps {
  siteId: string
  value: string[]
  onChange: (value: string[]) => void
}

interface AccountFormValues {
  scope: 'global' | 'site'
  username: string
  password: string
  enabled: boolean
}

const defaultAccountForm: AccountFormValues = { scope: 'site', username: '', password: '', enabled: true }

export const authAccountKeys = {
  list: (siteId: string) => ['site-detail', siteId, 'auth-accounts'] as const,
}

export function AuthAccountSelector({ siteId, value, onChange }: AuthAccountSelectorProps) {
  const query = useQuery({ queryKey: authAccountKeys.list(siteId), queryFn: () => listAuthAccounts(siteId) })
  const accounts = (query.data || []).filter((account) => account.enabled)

  function toggle(accountId: string, checked: boolean) {
    onChange(checked ? Array.from(new Set([...value, accountId])) : value.filter((id) => id !== accountId))
  }

  if (query.isLoading) return <Text size="sm" c="dimmed">账户加载中...</Text>
  if (accounts.length === 0) return <Text size="sm" c="dimmed">暂无启用账户</Text>

  return (
    <ScrollArea.Autosize mah={260} offsetScrollbars>
      <Table striped highlightOnHover withTableBorder miw={340}>
        <Table.Thead><Table.Tr><Table.Th w={48}>选择</Table.Th><Table.Th w={150}>用户名</Table.Th><Table.Th w={90}>类型</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>
          {accounts.map((account) => (
            <Table.Tr key={account.id}>
              <Table.Td><Checkbox checked={value.includes(account.id)} onChange={(event) => toggle(account.id, event.currentTarget.checked)} /></Table.Td>
              <Table.Td><Text size="sm" fw={500} truncate>{account.username}</Text></Table.Td>
              <Table.Td><Badge variant="light" color={account.scope === 'global' ? 'blue' : 'teal'}>{account.scope === 'global' ? '全局账户' : '站点账户'}</Badge></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </ScrollArea.Autosize>
  )
}

export function AuthAccountManager({ siteId, opened, onClose }: AuthAccountManagerProps) {
  const queryClient = useQueryClient()
  const [formOpened, formHandlers] = useDisclosure(false)
  const [editingAccount, setEditingAccount] = useState<AuthAccount | null>(null)
  const queryKey = authAccountKeys.list(siteId)
  const query = useQuery({ queryKey, queryFn: () => listAuthAccounts(siteId), enabled: opened })
  const saveMutation = useMutation({
    mutationFn: (values: AccountFormValues) => editingAccount
      ? updateAuthAccount(siteId, editingAccount.id, { scope: values.scope, username: values.username, password: values.password || undefined, enabled: values.enabled })
      : createAuthAccount(siteId, { scope: values.scope, username: values.username, password: values.password, enabled: values.enabled }),
  })
  const deleteMutation = useMutation({ mutationFn: (accountId: string) => deleteAuthAccount(siteId, accountId) })
  const form = useForm<AccountFormValues>({
    initialValues: defaultAccountForm,
    validate: {
      username: (value) => value.trim() ? null : '请填写用户名',
      password: (value) => editingAccount || value ? null : '请填写密码',
    },
  })

  function openCreate() {
    setEditingAccount(null)
    form.setValues(defaultAccountForm)
    form.clearErrors()
    formHandlers.open()
  }

  function openEdit(account: AuthAccount) {
    setEditingAccount(account)
    form.setValues({ scope: account.scope, username: account.username, password: '', enabled: account.enabled })
    form.clearErrors()
    formHandlers.open()
  }

  async function saveAccount(values: AccountFormValues) {
    try {
      await saveMutation.mutateAsync({ ...values, username: values.username.trim() })
      notifySuccess({ message: editingAccount ? '账户已更新' : '账户已创建' })
      formHandlers.close()
      await queryClient.invalidateQueries({ queryKey })
    } catch (error) {
      showErrorModal(error, editingAccount ? '保存账户失败' : '创建账户失败')
    }
  }

  function removeAccount(account: AuthAccount) {
    confirmDanger({
      title: '删除账户',
      message: `确认删除账户「${account.username}」？`,
      confirmLabel: '确认删除',
      errorTitle: '删除账户失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(account.id)
        notifySuccess({ message: '账户已删除' })
        await queryClient.invalidateQueries({ queryKey })
      },
    })
  }

  return (
    <Modal opened={opened} onClose={onClose} title="账户管理" size="lg" centered closeOnClickOutside={false}>
      <Stack gap="md">
        <Group justify="flex-end"><Button leftSection={<IconPlus size={16} />} onClick={openCreate}>添加账户</Button></Group>
        <ScrollArea.Autosize mah={420} offsetScrollbars>
          <Table striped highlightOnHover withTableBorder miw={540}>
            <Table.Thead><Table.Tr><Table.Th>用户名</Table.Th><Table.Th w={96}>类型</Table.Th><Table.Th w={72}>状态</Table.Th><Table.Th w={84}>操作</Table.Th></Table.Tr></Table.Thead>
            <Table.Tbody>
              {(query.data || []).map((account) => <AccountRow key={account.id} account={account} onEdit={openEdit} onDelete={removeAccount} deleting={deleteMutation.isPending} />)}
              {!query.isLoading && (query.data || []).length === 0 ? <Table.Tr><Table.Td colSpan={4}><Text c="dimmed" ta="center" py="md">暂无账户</Text></Table.Td></Table.Tr> : null}
            </Table.Tbody>
          </Table>
        </ScrollArea.Autosize>
      </Stack>

      <Modal opened={formOpened} onClose={formHandlers.close} title={editingAccount ? '编辑账户' : '添加账户'} centered>
        <form onSubmit={form.onSubmit(saveAccount)}>
          <Stack gap="md">
            <Radio.Group label="账户类型" {...form.getInputProps('scope')}>
              <Group mt="xs"><Radio value="site" label="站点账户" /><Radio value="global" label="全局账户" /></Group>
            </Radio.Group>
            <TextInput label="用户名" autoComplete="off" {...form.getInputProps('username')} />
            <PasswordInput label={editingAccount ? '新密码' : '密码'} autoComplete="new-password" {...form.getInputProps('password')} />
            <Switch label="启用账户" {...form.getInputProps('enabled', { type: 'checkbox' })} />
            <Group justify="flex-end"><Button variant="default" onClick={formHandlers.close}>取消</Button><Button type="submit" loading={saveMutation.isPending}>{editingAccount ? '保存' : '添加'}</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Modal>
  )
}

function AccountRow({ account, onEdit, onDelete, deleting }: { account: AuthAccount; onEdit: (account: AuthAccount) => void; onDelete: (account: AuthAccount) => void; deleting: boolean }) {
  return (
    <Table.Tr>
      <Table.Td><Text size="sm" fw={500}>{account.username}</Text></Table.Td>
      <Table.Td><Badge variant="light" color={account.scope === 'global' ? 'blue' : 'teal'}>{account.scope === 'global' ? '全局账户' : '站点账户'}</Badge></Table.Td>
      <Table.Td><Badge variant="light" color={account.enabled ? 'green' : 'gray'}>{account.enabled ? '启用' : '禁用'}</Badge></Table.Td>
      <Table.Td><Group gap={4} wrap="nowrap"><Tooltip label="编辑"><ActionIcon variant="subtle" onClick={() => onEdit(account)}><IconEdit size={16} /></ActionIcon></Tooltip><Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={deleting} onClick={() => onDelete(account)}><IconTrash size={16} /></ActionIcon></Tooltip></Group></Table.Td>
    </Table.Tr>
  )
}
