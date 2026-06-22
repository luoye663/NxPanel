import { Button, Group, PasswordInput, Stack } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { gatePath } from '@/api/gate'
import * as authApi from '@/api/auth'
import { useAuth } from '@/auth/AuthProvider'
import { SectionCard } from '@/components/common/SectionCard'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

export function PasswordSection() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [saving, setSaving] = useState(false)
  const form = useForm({
    initialValues: { current_password: '', new_password: '', confirm_password: '' },
    validate: {
      current_password: (value) => (value ? null : '请输入原密码'),
      new_password: (value, values) => {
        if (!value) return '请输入新密码'
        if (value === values.current_password) return '新密码不能与原密码相同'
        return null
      },
      confirm_password: (value, values) => (value === values.new_password ? null : '两次输入的新密码不一致'),
    },
  })

  async function handleSubmit(values: typeof form.values) {
    setSaving(true)
    try {
      await authApi.changePassword({ current_password: values.current_password, new_password: values.new_password })
      notifySuccess({ message: '密码修改成功，请重新登录' })
      // 后端会清理所有 session，前端同步清理认证态并回到登录页。
      auth.clearAuth()
      navigate(gatePath, { replace: true })
    } catch (err) {
      showErrorModal(err, '修改密码失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionCard title="修改密码" description="修改后所有会话会失效，需要重新登录。">
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <PasswordInput label="原密码" autoComplete="current-password" {...form.getInputProps('current_password')} />
          <PasswordInput
            label="新密码"
            description="至少 8 位，建议包含大小写字母、数字和特殊字符。"
            autoComplete="new-password"
            {...form.getInputProps('new_password')}
          />
          <PasswordInput label="确认新密码" autoComplete="new-password" {...form.getInputProps('confirm_password')} />
          <Group justify="flex-start"><Button type="submit" loading={saving}>修改密码</Button></Group>
        </Stack>
      </form>
    </SectionCard>
  )
}
