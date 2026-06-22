import { Button, Group, Stack, Switch, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation } from '@tanstack/react-query'
import { useExistingFiles } from '@/api/ssl'
import type { SiteDetail } from '@/api/types'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteExistingCertificatePanelProps {
  site: SiteDetail
  onChanged: () => Promise<void>
}

interface ExistingFormValues {
  cert_path: string
  key_path: string
  force_https: boolean
}

export function SiteExistingCertificatePanel({ site, onChanged }: SiteExistingCertificatePanelProps) {
  const saveMutation = useMutation({ mutationFn: (values: ExistingFormValues) => useExistingFiles(site.id, values) })
  const form = useForm<ExistingFormValues>({
    initialValues: { cert_path: '', key_path: '', force_https: true },
    validate: {
      cert_path: (value) => value.trim() ? null : '请输入证书路径',
      key_path: (value) => value.trim() ? null : '请输入私钥路径',
    },
  })

  async function handleSave(values: ExistingFormValues) {
    try {
      await saveMutation.mutateAsync({ cert_path: values.cert_path.trim(), key_path: values.key_path.trim(), force_https: values.force_https })
      notifySuccess({ message: 'SSL 已启用（使用已有证书）' })
      await onChanged()
    } catch (error) {
      showErrorModal(error, '启用已有证书失败')
    }
  }

  return (
    <form onSubmit={form.onSubmit(handleSave)}>
      <Stack gap="md" maw={760}>
        <TextInput label="证书路径" placeholder="/etc/letsencrypt/live/example.com/fullchain.pem" {...form.getInputProps('cert_path')} />
        <TextInput label="私钥路径" placeholder="/etc/letsencrypt/live/example.com/privkey.pem" {...form.getInputProps('key_path')} />
        <Switch label="强制 HTTPS" {...form.getInputProps('force_https', { type: 'checkbox' })} />
        <Group><Button type="submit" loading={saveMutation.isPending}>启用 SSL</Button></Group>
      </Stack>
    </form>
  )
}
