import { Alert, Button, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { getCaptchaConfig } from '@/api/auth'
import { ApiError } from '@/api/client'
import { useAuth } from '@/auth/AuthProvider'
import { CaptchaWidget, type CaptchaWidgetRef } from '@/components/auth/CaptchaWidget'
import { notifySuccess } from '@/utils/notify'

type LoginStep = 'credentials' | 'totp'

function getInlineErrorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return '操作失败，请重试'
}

function safeRedirect(url: string | null): string {
  if (!url) return '/'
  try {
    const parsed = new URL(url, window.location.origin)
    if (parsed.origin === window.location.origin && parsed.pathname.startsWith('/')) {
      return parsed.pathname + parsed.search
    }
  } catch {
    // 非 URL 字符串继续走相对路径检查。
  }
  return url.startsWith('/') && !url.startsWith('//') ? url : '/'
}

export function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const captchaRef = useRef<CaptchaWidgetRef | null>(null)
  const [step, setStep] = useState<LoginStep>('credentials')
  const [tempToken, setTempToken] = useState('')
  const [showRecovery, setShowRecovery] = useState(false)
  const [captchaRequired, setCaptchaRequired] = useState(false)
  const [captchaProvider, setCaptchaProvider] = useState('')
  const [captchaSiteKey, setCaptchaSiteKey] = useState('')
  const [captchaToken, setCaptchaToken] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [submitting, setSubmitting] = useState(false)

  const credentialsForm = useForm({
    initialValues: { username: '', password: '' },
    validate: {
      username: (value) => (value ? null : '请输入用户名'),
      password: (value) => (value ? null : '请输入密码'),
    },
  })
  const totpForm = useForm({ initialValues: { code: '' }, validate: { code: (value) => (/^\d{6}$/.test(value) ? null : '请输入 6 位验证码') } })
  const recoveryForm = useForm({ initialValues: { recoveryCode: '' }, validate: { recoveryCode: (value) => (value ? null : '请输入恢复码') } })

  const syncCaptchaConfig = useCallback(async () => {
    const config = await getCaptchaConfig()
    setCaptchaRequired(config.required)
    if (config.required && (config.provider === 'turnstile' || config.provider === 'hcaptcha') && config.site_key) {
      setCaptchaProvider(config.provider)
      setCaptchaSiteKey(config.site_key)
      return config
    }
    setCaptchaProvider('')
    setCaptchaSiteKey('')
    setCaptchaToken('')
    captchaRef.current?.reset()
    return config
  }, [])

  useEffect(() => {
    syncCaptchaConfig().catch(() => undefined)
  }, [syncCaptchaConfig])

  const redirectAfterLogin = useCallback(() => {
    notifySuccess({ message: '登录成功' })
    navigate(safeRedirect(searchParams.get('redirect')), { replace: true })
  }, [navigate, searchParams])

  async function handleCredentials(values: typeof credentialsForm.values) {
    setSubmitting(true)
    setError(null)
    try {
      const resp = await auth.login(values.username, values.password, captchaToken || undefined)
      if (resp.requires_2fa) {
        setStep('totp')
        setTempToken(resp.temp_token || '')
        setCaptchaToken('')
        return
      }
      redirectAfterLogin()
    } catch (err) {
      setCaptchaToken('')
      captchaRef.current?.reset()
      if (err instanceof ApiError && err.code === 'CAPTCHA_FAILED') {
        await syncCaptchaConfig().catch(() => undefined)
      }
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleTOTP(values: typeof totpForm.values) {
    setSubmitting(true)
    setError(null)
    try {
      await auth.login2FA(tempToken, values.code)
      redirectAfterLogin()
    } catch (err) {
      totpForm.setFieldValue('code', '')
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleRecovery(values: typeof recoveryForm.values) {
    setSubmitting(true)
    setError(null)
    try {
      await auth.loginRecover(tempToken, values.recoveryCode)
      redirectAfterLogin()
    } catch (err) {
      recoveryForm.setFieldValue('recoveryCode', '')
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  function backToCredentials() {
    setStep('credentials')
    setTempToken('')
    setShowRecovery(false)
    setError(null)
    totpForm.reset()
    recoveryForm.reset()
  }

  const showCaptcha = captchaRequired && !!captchaProvider && !!captchaSiteKey
  const credentialsError = step === 'credentials' && error ? getInlineErrorMessage(error) : ''
  const otpError = step === 'totp' && !showRecovery && error ? getInlineErrorMessage(error) : ''
  const recoveryError = step === 'totp' && showRecovery && error ? getInlineErrorMessage(error) : ''
  const canSubmitCredentials = credentialsForm.values.username.trim() !== '' && credentialsForm.values.password !== '' && (!showCaptcha || captchaToken !== '') && !submitting
  const canSubmitTOTP = /^\d{6}$/.test(totpForm.values.code) && !submitting
  const canSubmitRecovery = recoveryForm.values.recoveryCode.trim() !== '' && !submitting

  return (
    <Stack gap="md">
      {step === 'credentials' ? (
        <form onSubmit={credentialsForm.onSubmit(handleCredentials)} autoComplete="on">
          <Stack gap="md">
            <TextInput size="md" label="用户名" placeholder="请输入用户名" autoComplete="username" name="username" {...credentialsForm.getInputProps('username')} />
            <PasswordInput size="md" label="密码" placeholder="请输入密码" autoComplete="current-password" name="password" {...credentialsForm.getInputProps('password')} />
            {credentialsError ? <Text size="sm" c="red">{credentialsError}</Text> : null}
            {showCaptcha ? (
              <Alert color="blue" title="需要人机验证">
                <CaptchaWidget
                  ref={captchaRef}
                  provider={captchaProvider}
                  siteKey={captchaSiteKey}
                  onVerified={setCaptchaToken}
                  onExpired={() => setCaptchaToken('')}
                  onError={() => setCaptchaToken('')}
                />
              </Alert>
            ) : null}
            <Button size="md" type="submit" loading={submitting} disabled={!canSubmitCredentials} fullWidth>登录</Button>
          </Stack>
        </form>
      ) : (
        <Stack gap="md">
          <div>
            <Title order={3} size="h4">{showRecovery ? '恢复码登录' : '两步验证'}</Title>
            <Text mt={6} size="sm" c="dimmed">{showRecovery ? '请输入一个未使用的恢复码。' : '请输入身份验证器中的 6 位数字验证码。'}</Text>
          </div>
          {showRecovery ? (
            <form onSubmit={recoveryForm.onSubmit(handleRecovery)}>
              <Stack gap="md">
                <TextInput size="md" label="恢复码" placeholder="输入恢复码" autoFocus {...recoveryForm.getInputProps('recoveryCode')} />
                {recoveryError ? <Text size="sm" c="red">{recoveryError}</Text> : null}
                <Button size="md" type="submit" loading={submitting} disabled={!canSubmitRecovery} fullWidth>验证</Button>
              </Stack>
            </form>
          ) : (
            <form onSubmit={totpForm.onSubmit(handleTOTP)}>
              <Stack gap="md">
                <TextInput size="md" label="验证码" placeholder="6 位数字验证码" maxLength={6} autoFocus autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]*" name="one-time-code" {...totpForm.getInputProps('code')} />
                {otpError ? <Text size="sm" c="red">{otpError}</Text> : null}
                <Button size="md" type="submit" loading={submitting} disabled={!canSubmitTOTP} fullWidth>验证</Button>
              </Stack>
            </form>
          )}
          <Button size="md" variant="subtle" onClick={() => { setShowRecovery((value) => !value); setError(null) }}>{showRecovery ? '使用验证码登录' : '使用恢复码登录'}</Button>
          <Button size="md" variant="subtle" color="gray" onClick={backToCredentials}>返回登录</Button>
        </Stack>
      )}
      {showCaptcha && !captchaToken ? <Text size="xs" c="dimmed">请先完成人机验证后再提交。</Text> : null}
    </Stack>
  )
}
