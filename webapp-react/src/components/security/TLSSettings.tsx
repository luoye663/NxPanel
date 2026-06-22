import { Alert, Button, Group, Stack, Switch, TextInput } from '@mantine/core'
import type { UseFormReturnType } from '@mantine/form'
import { SectionCard } from '@/components/common/SectionCard'
import type { SecuritySettingsFormValues } from './types'

interface TLSSettingsProps {
  form: UseFormReturnType<SecuritySettingsFormValues>
  saving: boolean
  onSave: () => void
}

export function TLSSettings({ form, saving, onSave }: TLSSettingsProps) {
  return (
    <SectionCard title="TLS / HTTPS" description="控制 API 服务 HTTPS 监听和自签名证书。">
      <Stack gap="md">
        <Switch label="启用 TLS" {...form.getInputProps('tls_enabled', { type: 'checkbox' })} />
        <Group grow align="flex-start">
          <TextInput label="证书路径" placeholder="为空则自动生成自签名证书" {...form.getInputProps('tls_cert')} />
          <TextInput label="私钥路径" placeholder="为空则自动生成自签名证书" {...form.getInputProps('tls_key')} />
        </Group>
        <TextInput label="证书有效期" placeholder="8760h" {...form.getInputProps('tls_cert_validity')} />
        <Alert color="yellow">修改 TLS 配置后需要重启 API 服务才能完全生效。</Alert>
        <Group justify="flex-start"><Button loading={saving} onClick={onSave}>保存 TLS 配置</Button></Group>
      </Stack>
    </SectionCard>
  )
}
