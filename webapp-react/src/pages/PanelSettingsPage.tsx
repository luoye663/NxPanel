import { Button, Grid, Group, LoadingOverlay, Stack, Tabs, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { queryKeys, useSecuritySettings } from '@/api/hooks'
import { gatePath, reloadToLogin } from '@/api/gate'
import { updateBranding, updateSecuritySettings } from '@/api/settings'
import type { BrandingSettings, SecuritySettings, UpdateSecuritySettingsRequest } from '@/api/types'
import { useBranding } from '@/branding/BrandingProvider'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { CaptchaSettings } from '@/components/security/CaptchaSettings'
import { PasswordSection } from '@/components/security/PasswordSection'
import { RuntimeSecuritySettings } from '@/components/security/RuntimeSecuritySettings'
import { TLSSettings } from '@/components/security/TLSSettings'
import { TwoFASection } from '@/components/security/TwoFASection'
import { GeoIPSettingsPanel } from '@/components/security/GeoIPSettings'
import type { CaptchaProvider, SecuritySettingsFormValues } from '@/components/security/types'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const defaultSecurityValues: SecuritySettingsFormValues = {
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

function toSecurityFormValues(settings: SecuritySettings): SecuritySettingsFormValues {
  return {
    ...defaultSecurityValues,
    ...settings,
    trusted_proxies: [...settings.trusted_proxies],
    captcha_provider: (settings.captcha_provider || 'none') as CaptchaProvider,
    captcha_secret_key: '',
  }
}

function toSecurityUpdateRequest(values: SecuritySettingsFormValues): UpdateSecuritySettingsRequest {
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
    request.captcha_secret_key = values.captcha_secret_key.trim()
  }
  return request
}

export function PanelSettingsPage() {
  const [activeTab, setActiveTab] = useState<string | null>('branding')
  const queryClient = useQueryClient()
  const { branding, isLoading: brandingLoading, isError: brandingError, error: brandingLoadError } = useBranding()
  const securityQuery = useSecuritySettings()
  const brandingForm = useForm<BrandingSettings>({
    initialValues: branding,
    validate: {
      site_name: (value) => value.trim() ? null : '请输入网站名称',
    },
  })
  const securityForm = useForm<SecuritySettingsFormValues>({ initialValues: defaultSecurityValues })

  const brandingMutation = useMutation({
    mutationFn: updateBranding,
    onSuccess: (settings) => {
      queryClient.setQueryData(queryKeys.branding, settings)
      brandingForm.setValues(settings)
      brandingForm.resetDirty(settings)
      notifySuccess({ message: '品牌信息已保存' })
    },
    onError: (error) => showErrorModal(error, '保存品牌信息失败'),
  })
  const securityMutation = useMutation({
    mutationFn: (values: SecuritySettingsFormValues) => updateSecuritySettings(toSecurityUpdateRequest(values)),
    onSuccess: async (settings) => {
      notifySuccess({ message: '安全配置已保存' })
      if (settings.login_path && settings.login_path !== gatePath) {
        reloadToLogin(settings.login_path)
        return
      }
      securityForm.setValues(toSecurityFormValues(settings))
      await queryClient.invalidateQueries({ queryKey: queryKeys.securitySettings })
    },
    onError: (error) => showErrorModal(error, '保存安全配置失败'),
  })

  useEffect(() => {
    brandingForm.setValues(branding)
    brandingForm.resetDirty(branding)
  }, [branding])

  useEffect(() => {
    if (securityQuery.data) securityForm.setValues(toSecurityFormValues(securityQuery.data))
  }, [securityQuery.data])

  function saveBranding(values: BrandingSettings) {
    brandingMutation.mutate({
      site_name: values.site_name.trim(),
      subtitle: values.subtitle.trim(),
    })
  }

  function saveSecurity() {
    securityMutation.mutate(securityForm.values)
  }

  return (
    <PageShell>
      <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}>
        <Tabs.List>
          <Tabs.Tab value="branding">品牌设置</Tabs.Tab>
          <Tabs.Tab value="account">账户安全</Tabs.Tab>
          <Tabs.Tab value="security">安全配置</Tabs.Tab>
          <Tabs.Tab value="geoip">GeoIP</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="branding">
          {brandingError ? <ErrorAlert error={brandingLoadError} title="加载品牌信息失败，当前显示默认值" /> : null}
          <SectionCard title="品牌信息" pos="relative">
            <LoadingOverlay visible={brandingLoading} />
            <form onSubmit={brandingForm.onSubmit(saveBranding)}>
              <Stack gap="md">
                <TextInput label="网站名称" required maxLength={80} {...brandingForm.getInputProps('site_name')} />
                <TextInput label="副标题" maxLength={160} {...brandingForm.getInputProps('subtitle')} />
                <Group justify="flex-end">
                  <Button
                    type="submit"
                    leftSection={<IconDeviceFloppy size={16} />}
                    loading={brandingMutation.isPending}
                    disabled={!brandingForm.isDirty()}
                  >
                    保存
                  </Button>
                </Group>
              </Stack>
            </form>
          </SectionCard>
        </Tabs.Panel>

        <Tabs.Panel value="account">
          <Grid gutter="md">
            <Grid.Col span={{ base: 12, lg: 6 }}><PasswordSection /></Grid.Col>
            <Grid.Col span={{ base: 12, lg: 6 }}><TwoFASection /></Grid.Col>
          </Grid>
        </Tabs.Panel>

        <Tabs.Panel value="security" pos="relative">
          {securityQuery.isError ? <ErrorAlert error={securityQuery.error} title="加载安全配置失败" /> : null}
          <LoadingOverlay visible={securityQuery.isLoading} />
          <Grid gutter="md">
            <Grid.Col span={{ base: 12, lg: 6 }}><RuntimeSecuritySettings form={securityForm} saving={securityMutation.isPending} onSave={saveSecurity} /></Grid.Col>
            <Grid.Col span={{ base: 12, lg: 6 }}><CaptchaSettings form={securityForm} secretMasked={securityQuery.data?.captcha_secret_key_masked || ''} saving={securityMutation.isPending} onSave={saveSecurity} /></Grid.Col>
            <Grid.Col span={12}><TLSSettings form={securityForm} saving={securityMutation.isPending} onSave={saveSecurity} /></Grid.Col>
          </Grid>
        </Tabs.Panel>

        <Tabs.Panel value="geoip">
          <GeoIPSettingsPanel />
        </Tabs.Panel>
      </Tabs>
    </PageShell>
  )
}
