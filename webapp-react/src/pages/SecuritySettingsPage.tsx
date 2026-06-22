import { Grid, LoadingOverlay, Tabs } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { queryKeys, useSecuritySettings } from '@/api/hooks'
import { gatePath, reloadToLogin } from '@/api/gate'
import { updateSecuritySettings } from '@/api/settings'
import type { SecuritySettings, UpdateSecuritySettingsRequest } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { CaptchaSettings } from '@/components/security/CaptchaSettings'
import { PasswordSection } from '@/components/security/PasswordSection'
import { RuntimeSecuritySettings } from '@/components/security/RuntimeSecuritySettings'
import { TLSSettings } from '@/components/security/TLSSettings'
import { TwoFASection } from '@/components/security/TwoFASection'
import type { CaptchaProvider, SecuritySettingsFormValues } from '@/components/security/types'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const defaultValues: SecuritySettingsFormValues = {
  login_path: gatePath,
  public_health: false,
  rate_limit_max_failures: 5,
  rate_limit_window: '15m',
  max_sessions: 5,
  bind_session_ip: true,
  bind_session_ua: true,
  trusted_proxies: [],
  captcha_provider: 'none',
  captcha_site_key: '',
  captcha_secret_key: '',
  captcha_trigger_after_failures: 3,
  tls_enabled: true,
  tls_cert: '',
  tls_key: '',
  tls_cert_validity: '8760h',
}

function toFormValues(settings: SecuritySettings): SecuritySettingsFormValues {
  return {
    ...defaultValues,
    ...settings,
    trusted_proxies: [...settings.trusted_proxies],
    captcha_provider: (settings.captcha_provider || 'none') as CaptchaProvider,
    captcha_secret_key: '',
  }
}

function toUpdateRequest(values: SecuritySettingsFormValues): UpdateSecuritySettingsRequest {
  const request: UpdateSecuritySettingsRequest = {
    login_path: values.login_path,
    public_health: values.public_health,
    rate_limit_max_failures: values.rate_limit_max_failures,
    rate_limit_window: values.rate_limit_window,
    max_sessions: values.max_sessions,
    bind_session_ip: values.bind_session_ip,
    bind_session_ua: values.bind_session_ua,
    trusted_proxies: values.trusted_proxies,
    captcha_provider: values.captcha_provider,
    captcha_site_key: values.captcha_site_key,
    captcha_trigger_after_failures: values.captcha_trigger_after_failures,
    tls_enabled: values.tls_enabled,
    tls_cert: values.tls_cert,
    tls_key: values.tls_key,
    tls_cert_validity: values.tls_cert_validity,
  }
  if (values.captcha_secret_key.trim()) {
    // Captcha secret 留空表示不修改，避免把脱敏占位符当作真实 secret 提交。
    request.captcha_secret_key = values.captcha_secret_key.trim()
  }
  return request
}

export function SecuritySettingsPage() {
  const [activeTab, setActiveTab] = useState<string | null>('account')
  const queryClient = useQueryClient()
  const settingsQuery = useSecuritySettings()
  const form = useForm<SecuritySettingsFormValues>({ initialValues: defaultValues })
  const mutation = useMutation({
    mutationFn: (values: SecuritySettingsFormValues) => updateSecuritySettings(toUpdateRequest(values)),
    onSuccess: async (settings) => {
      notifySuccess({ message: '安全配置已保存' })
      if (settings.login_path && settings.login_path !== gatePath) {
        reloadToLogin(settings.login_path)
        return
      }
      form.setValues(toFormValues(settings))
      await queryClient.invalidateQueries({ queryKey: queryKeys.securitySettings })
    },
    onError: (error) => showErrorModal(error, '保存安全配置失败'),
  })

  useEffect(() => {
    if (settingsQuery.data) form.setValues(toFormValues(settingsQuery.data))
  }, [settingsQuery.data])

  function saveConfig() {
    mutation.mutate(form.values)
  }

  return (
    <PageShell>
      {settingsQuery.isError ? <ErrorAlert error={settingsQuery.error} title="加载安全配置失败" /> : null}
      <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}>
        <Tabs.List>
          <Tabs.Tab value="account">账户安全</Tabs.Tab>
          <Tabs.Tab value="config">安全配置</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="account">
          <Grid gutter="md">
            <Grid.Col span={{ base: 12, lg: 6 }}><PasswordSection /></Grid.Col>
            <Grid.Col span={{ base: 12, lg: 6 }}><TwoFASection /></Grid.Col>
          </Grid>
        </Tabs.Panel>
        <Tabs.Panel value="config" pos="relative">
          <LoadingOverlay visible={settingsQuery.isLoading} />
          <Grid gutter="md">
            <Grid.Col span={{ base: 12, lg: 6 }}><RuntimeSecuritySettings form={form} saving={mutation.isPending} onSave={saveConfig} /></Grid.Col>
            <Grid.Col span={{ base: 12, lg: 6 }}><CaptchaSettings form={form} secretMasked={settingsQuery.data?.captcha_secret_key_masked || ''} saving={mutation.isPending} onSave={saveConfig} /></Grid.Col>
            <Grid.Col span={12}><TLSSettings form={form} saving={mutation.isPending} onSave={saveConfig} /></Grid.Col>
          </Grid>
        </Tabs.Panel>
      </Tabs>
    </PageShell>
  )
}
