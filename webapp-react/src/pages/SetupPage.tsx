import { Alert, Button, Group, NumberInput, PasswordInput, Select, Stack, Stepper, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { reloadToLogin } from '@/api/gate'
import { useAuth } from '@/auth/AuthProvider'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { notifySuccess } from '@/utils/notify'

const captchaProviders = [
  { value: 'none', label: '不启用' },
  { value: 'turnstile', label: 'Cloudflare Turnstile' },
  { value: 'hcaptcha', label: 'hCaptcha' },
]

export function SetupPage() {
  const auth = useAuth()
  const [active, setActive] = useState(0)
  const [error, setError] = useState<unknown>(null)
  const [submitting, setSubmitting] = useState(false)

  const adminForm = useForm({
    initialValues: {
      username: '',
      password: '',
      confirmPassword: '',
    },
    validate: {
      username: (value) => (value.length < 2 || value.length > 32 ? '用户名长度 2-32 个字符' : null),
      password: (value) => (value.length < 8 || value.length > 64 ? '密码长度 8-64 个字符' : null),
      confirmPassword: (value, values) => (value !== values.password ? '两次输入的密码不一致' : null),
    },
  })

  const captchaForm = useForm({
    initialValues: {
      captcha_provider: 'none' as string,
      captcha_site_key: '',
      captcha_secret_key: '',
      captcha_trigger_after_failures: 3,
    },
    validate: {
      captcha_site_key: (value, values) =>
        values.captcha_provider !== 'none' && !value ? '请输入站点 Key' : null,
      captcha_secret_key: (value, values) =>
        values.captcha_provider !== 'none' && !value ? '请输入密钥' : null,
    },
  })

  const captchaEnabled = captchaForm.values.captcha_provider !== 'none'

  async function handleSubmit() {
    setSubmitting(true)
    setError(null)
    try {
      const captcha = captchaEnabled
        ? {
            captcha_provider: captchaForm.values.captcha_provider,
            captcha_site_key: captchaForm.values.captcha_site_key,
            captcha_secret_key: captchaForm.values.captcha_secret_key,
            captcha_trigger_after_failures: captchaForm.values.captcha_trigger_after_failures,
          }
        : undefined
      const resp = await auth.setupAdmin({ username: adminForm.values.username, password: adminForm.values.password, ...captcha })
      notifySuccess({ message: '管理员初始化成功，请登录' })
      reloadToLogin(resp.login_path)
    } catch (err) {
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  function handleSkip() {
    setSubmitting(true)
    setError(null)
    auth
      .setupAdmin({ username: adminForm.values.username, password: adminForm.values.password })
      .then(() => {
        notifySuccess({ message: '管理员初始化成功，请登录' })
        reloadToLogin()
      })
      .catch((err) => setError(err))
      .finally(() => setSubmitting(false))
  }

  function nextStep() {
    if (adminForm.isValid()) {
      setError(null)
      setActive(1)
    } else {
      adminForm.validate()
    }
  }

  return (
    <Stack gap="lg">
      <div>
        <Title order={2} size="h3">初始化管理员</Title>
        <Text mt={6} size="sm" c="dimmed">首次使用需要设置管理员账户，请妥善保管密码。</Text>
      </div>

      <Stepper active={active} onStepClick={setActive} orientation="vertical">
        <Stepper.Step label="设置管理员" description="创建管理员账号">
          <form onSubmit={adminForm.onSubmit(nextStep)}>
            <Stack gap="md" mt="md">
              <TextInput size="md" label="用户名" placeholder="请输入用户名" autoComplete="username" autoFocus {...adminForm.getInputProps('username')} />
              <PasswordInput size="md" label="密码" placeholder="请输入密码，至少 8 位" autoComplete="new-password" {...adminForm.getInputProps('password')} />
              <PasswordInput size="md" label="确认密码" placeholder="请再次输入密码" autoComplete="new-password" {...adminForm.getInputProps('confirmPassword')} />
              {error ? <ErrorAlert error={error} /> : null}
              <Button size="md" type="submit" fullWidth>下一步</Button>
            </Stack>
          </form>
        </Stepper.Step>

        <Stepper.Step label="验证码 (CAPTCHA)" description="可选，可跳过">
          <Stack gap="md" mt="md">
            <Alert color="blue" variant="light">
              配置验证码可在登录失败达到阈值后要求人机验证，降低暴力破解风险。也可以之后在安全设置中配置。
            </Alert>
            <Select
              size="md"
              label="验证码服务"
              data={captchaProviders}
              {...captchaForm.getInputProps('captcha_provider')}
            />
            {captchaEnabled ? (
              <>
                <TextInput size="md" label="站点 Key" placeholder="前端站点 Key" {...captchaForm.getInputProps('captcha_site_key')} />
                <PasswordInput size="md" label="密钥" placeholder="后端密钥" {...captchaForm.getInputProps('captcha_secret_key')} />
                <NumberInput size="md" label="触发阈值" min={0} max={20} description="登录失败多少次后触发验证码" {...captchaForm.getInputProps('captcha_trigger_after_failures')} />
              </>
            ) : (
              <Alert color="gray">当前未启用验证码。</Alert>
            )}
            {error ? <ErrorAlert error={error} /> : null}
            <Group>
              <Button size="md" loading={submitting} disabled={!captchaForm.isValid()} onClick={() => captchaForm.onSubmit(handleSubmit)()}>
                完成配置
              </Button>
              <Button size="md" variant="subtle" loading={submitting} onClick={handleSkip}>
                跳过此步骤
              </Button>
            </Group>
          </Stack>
        </Stepper.Step>
      </Stepper>
    </Stack>
  )
}
