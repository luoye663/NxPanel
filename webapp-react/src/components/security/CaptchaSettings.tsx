import { Alert, Button, Group, NumberInput, PasswordInput, Select, Stack, TextInput } from '@mantine/core'
import type { UseFormReturnType } from '@mantine/form'
import { SectionCard } from '@/components/common/SectionCard'
import type { CaptchaProvider, SecuritySettingsFormValues } from './types'

interface CaptchaSettingsProps {
  form: UseFormReturnType<SecuritySettingsFormValues>
  secretMasked: string
  saving: boolean
  onSave: () => void
}

const providers: { value: CaptchaProvider; label: string }[] = [
  { value: 'none', label: '不启用' },
  { value: 'turnstile', label: 'Cloudflare Turnstile' },
  { value: 'hcaptcha', label: 'hCaptcha' },
]

export function CaptchaSettings({ form, secretMasked, saving, onSave }: CaptchaSettingsProps) {
  const enabled = form.values.captcha_provider !== 'none'

  return (
    <SectionCard title="验证码 (CAPTCHA)" description="登录失败达到阈值后要求人机验证，降低暴力破解风险。">
      <Stack gap="md">
        <Select
          label="验证码服务"
          data={providers}
          value={form.values.captcha_provider}
          onChange={(value) => form.setFieldValue('captcha_provider', (value || 'none') as CaptchaProvider)}
        />
        {enabled ? (
          <>
            <TextInput label="站点 Key" placeholder="前端站点 Key" {...form.getInputProps('captcha_site_key')} />
            <PasswordInput
              label="密钥"
              placeholder={secretMasked ? '已设置（留空不修改）' : '后端密钥'}
              description="出于安全考虑，后端只返回脱敏状态；留空会保留现有密钥。"
              {...form.getInputProps('captcha_secret_key')}
            />
            <NumberInput label="触发阈值" min={0} max={20} {...form.getInputProps('captcha_trigger_after_failures')} />
          </>
        ) : <Alert color="gray">当前未启用验证码，登录失败不会触发外部 Captcha 校验。</Alert>}
        <Group justify="flex-start"><Button loading={saving} onClick={onSave}>保存验证码配置</Button></Group>
      </Stack>
    </SectionCard>
  )
}
