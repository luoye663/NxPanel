import { Autocomplete, Button, Checkbox, Group, Modal, Radio, Stack, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { applyCertificate, listEmails } from '@/api/acme'
import type { SiteDetail } from '@/api/types'
import { showErrorModal } from '@/utils/errorModal'
import { notifyWarning } from '@/utils/notify'

interface ACMEApplyModalProps {
  site: SiteDetail
  opened: boolean
  onClose: () => void
  onStarted: (orderId: string) => void
}

interface ApplyFormValues {
  challenge_type: 'http-01'
  email: string
  domains: string[]
}

export function ACMEApplyModal({ site, opened, onClose, onStarted }: ACMEApplyModalProps) {
  const emailsQuery = useQuery({ queryKey: ['acme', 'emails'], queryFn: listEmails, enabled: opened })
  const applyMutation = useMutation({ mutationFn: applyCertificate })
  const form = useForm<ApplyFormValues>({ initialValues: { challenge_type: 'http-01', email: '', domains: site.domains || [] } })

  useEffect(() => {
    if (!opened) return
    form.setValues({ challenge_type: 'http-01', email: '', domains: site.domains || [] })
  }, [opened, site.id])

  async function handleSubmit(values: ApplyFormValues) {
    if (!values.email.trim()) {
      notifyWarning({ message: '请输入邮箱' })
      return
    }
    if (values.domains.length === 0) {
      notifyWarning({ message: '请选择域名' })
      return
    }
    try {
      const result = await applyMutation.mutateAsync({ site_id: site.id, domains: values.domains, challenge_type: values.challenge_type, email: values.email.trim() })
      onClose()
      onStarted(result.order_id)
    } catch (error) {
      showErrorModal(error, '申请证书失败')
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="申请 SSL 证书" size="md" closeOnClickOutside={false} centered>
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <Radio.Group label="验证方式" value={form.values.challenge_type} onChange={(value) => form.setFieldValue('challenge_type', value as 'http-01')}>
            <Group mt="xs">
              <Radio value="http-01" label="文件验证" />
              <Tooltip label="即将支持" position="top">
                <Radio value="dns-01" label="DNS 验证（支持通配符）" disabled />
              </Tooltip>
            </Group>
          </Radio.Group>
          <Autocomplete
            label="邮箱"
            placeholder="请输入邮箱"
            data={emailsQuery.data || []}
            value={form.values.email}
            onChange={(value) => form.setFieldValue('email', value)}
            onFocus={() => { void emailsQuery.refetch() }}
            limit={20}
          />
          <Checkbox.Group label="申请域名" value={form.values.domains} onChange={(value) => form.setFieldValue('domains', value)}>
            <Stack gap="xs" mt="xs">
              {(site.domains || []).map((domain) => <Checkbox key={domain} value={domain} label={domain} />)}
            </Stack>
          </Checkbox.Group>
          <Group justify="flex-end"><Button variant="default" onClick={onClose}>取消</Button><Button type="submit" loading={applyMutation.isPending}>申请证书</Button></Group>
        </Stack>
      </form>
    </Modal>
  )
}
