import { ActionIcon, Button, Group, Modal, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { deleteEmail, listEmails, saveEmail } from '@/api/acme'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface ACMEEmailModalProps {
  opened: boolean
  onClose: () => void
}

export function ACMEEmailModal({ opened, onClose }: ACMEEmailModalProps) {
  const queryClient = useQueryClient()
  const [email, setEmail] = useState('')
  const emailsQuery = useQuery({ queryKey: ['acme', 'emails'], queryFn: listEmails, enabled: opened })
  const saveMutation = useMutation({ mutationFn: saveEmail })
  const deleteMutation = useMutation({ mutationFn: deleteEmail })

  async function handleAdd() {
    const value = email.trim()
    if (!value) return
    try {
      await saveMutation.mutateAsync(value)
      setEmail('')
      notifySuccess({ message: '邮箱已保存' })
      await queryClient.invalidateQueries({ queryKey: ['acme', 'emails'] })
    } catch (error) {
      showErrorModal(error, '保存邮箱失败')
    }
  }

  async function handleDelete(value: string) {
    try {
      await deleteMutation.mutateAsync(value)
      notifySuccess({ message: '邮箱已删除' })
      await queryClient.invalidateQueries({ queryKey: ['acme', 'emails'] })
    } catch (error) {
      showErrorModal(error, '删除邮箱失败')
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="邮箱管理" size="sm" centered>
      <Stack gap="md">
        {emailsQuery.isError ? <ErrorAlert error={emailsQuery.error} title="加载邮箱失败" /> : null}
        {(emailsQuery.data || []).length > 0 ? (emailsQuery.data || []).map((item) => (
          <Group key={item} justify="space-between" gap="sm">
            <Text size="sm">{item}</Text>
            <Tooltip label="删除"><ActionIcon color="red" variant="subtle" loading={deleteMutation.isPending} onClick={() => handleDelete(item)}><IconTrash size={16} /></ActionIcon></Tooltip>
          </Group>
        )) : <Text c="dimmed" size="sm">暂无历史邮箱</Text>}
        <Group align="flex-end"><TextInput style={{ flex: 1 }} label="新增邮箱" placeholder="输入新邮箱" value={email} onChange={(event) => setEmail(event.currentTarget.value)} onKeyDown={(event) => { if (event.key === 'Enter') void handleAdd() }} /><Button loading={saveMutation.isPending} onClick={handleAdd}>添加</Button></Group>
      </Stack>
    </Modal>
  )
}
