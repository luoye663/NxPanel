import { Alert, Badge, Button, Code, Grid, Group, Image, Modal, Stack, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import * as authApi from '@/api/auth'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { notifySuccess } from '@/utils/notify'

type SetupStep = 'idle' | 'setup' | 'done'
type CodeAction = 'disable' | 'regenerate' | null

export function TwoFASection() {
  const [loading, setLoading] = useState(false)
  const [enabled, setEnabled] = useState(false)
  const [hasRecovery, setHasRecovery] = useState(false)
  const [setupStep, setSetupStep] = useState<SetupStep>('idle')
  const [secret, setSecret] = useState('')
  const [qrDataUrl, setQrDataUrl] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [error, setError] = useState<unknown>(null)
  const [actionError, setActionError] = useState<unknown>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [codeAction, setCodeAction] = useState<CodeAction>(null)
  const verifyForm = useForm({ initialValues: { code: '' }, validate: { code: (value) => (/^\d{6}$/.test(value) ? null : '请输入 6 位验证码') } })
  const actionForm = useForm({ initialValues: { code: '' }, validate: { code: (value) => (/^\d{6}$/.test(value) ? null : '请输入 6 位验证码') } })

  async function loadStatus() {
    setLoading(true)
    setError(null)
    try {
      const status = await authApi.get2FAStatus()
      setEnabled(status.enabled)
      setHasRecovery(status.has_recovery)
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void loadStatus() }, [])

  async function startSetup() {
    setLoading(true)
    setError(null)
    try {
      const resp = await authApi.setup2FA()
      setSecret(resp.secret)
      setQrDataUrl(await QRCode.toDataURL(resp.url, { width: 220, margin: 2 }))
      setSetupStep('setup')
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }

  async function verifyAndEnable(values: typeof verifyForm.values) {
    setLoading(true)
    setError(null)
    try {
      const resp = await authApi.enable2FA({ code: values.code })
      setRecoveryCodes(resp.recovery_codes)
      setEnabled(true)
      setHasRecovery(resp.recovery_codes.length > 0)
      setSetupStep('done')
      notifySuccess({ message: '两步验证已启用' })
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }

  async function submitCodeAction(values: typeof actionForm.values) {
    if (!codeAction) return
    setActionLoading(true)
    setActionError(null)
    try {
      if (codeAction === 'disable') {
        await authApi.disable2FA({ code: values.code })
        setEnabled(false)
        setHasRecovery(false)
        setSetupStep('idle')
        notifySuccess({ message: '两步验证已禁用' })
      } else {
        const resp = await authApi.regenerateRecoveryCodes(values.code)
        setRecoveryCodes(resp.recovery_codes)
        setHasRecovery(true)
        notifySuccess({ message: '恢复码已重新生成' })
      }
      setCodeAction(null)
      actionForm.reset()
      await loadStatus()
    } catch (err) {
      setActionError(err)
    } finally {
      setActionLoading(false)
    }
  }

  function openCodeAction(action: Exclude<CodeAction, null>) {
    setActionError(null)
    actionForm.reset()
    setCodeAction(action)
  }

  function closeCodeAction() {
    setCodeAction(null)
    setActionError(null)
    actionForm.reset()
  }

  const actionErrorMessage = actionError instanceof Error ? actionError.message : (actionError ? '未知错误' : '')

  async function copyCodes() {
    await navigator.clipboard.writeText(recoveryCodes.join('\n'))
    // 恢复码属于敏感信息，通知只提示复制成功，不回显具体内容。
    notifySuccess({ message: '恢复码已复制' })
  }

  return (
    <>
      <SectionCard
        title="两步验证 (2FA)"
        description="启用后登录时需要输入 TOTP 动态验证码。"
        actions={<Badge color={enabled ? 'green' : 'gray'}>{enabled ? '已启用' : '未启用'}</Badge>}
      >
        <Stack gap="md">
          {error ? <ErrorAlert error={error} /> : null}
          <Text size="sm" c="dimmed">恢复码状态：{hasRecovery ? '已生成' : '未生成或不可用'}</Text>
          {!enabled && setupStep === 'idle' ? <Button loading={loading} onClick={startSetup}>启用两步验证</Button> : null}
          {setupStep === 'setup' ? (
            <Grid gutter="lg">
              <Grid.Col span={{ base: 12, sm: 5 }}><Image src={qrDataUrl} alt="TOTP 二维码" fit="contain" /></Grid.Col>
              <Grid.Col span={{ base: 12, sm: 7 }}>
                <form onSubmit={verifyForm.onSubmit(verifyAndEnable)}>
                  <Stack gap="md">
                    <TextInput label="验证码" placeholder="6 位验证码" maxLength={6} autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]*" name="one-time-code" {...verifyForm.getInputProps('code')} />
                    <TextInput label="手动输入密钥" value={secret} readOnly description="无法扫描二维码时可复制此密钥到身份验证器。" />
                    <Button type="submit" loading={loading} className="securityActionButton">验证并启用</Button>
                  </Stack>
                </form>
              </Grid.Col>
            </Grid>
          ) : null}
          {recoveryCodes.length > 0 ? (
            <Stack gap="md">
              <Alert color="yellow">请妥善保存这些恢复码。每个恢复码只能使用一次。</Alert>
              <div className="recoveryCodeGrid">{recoveryCodes.map((code) => <Code key={code}>{code}</Code>)}</div>
              <Group><Button variant="light" onClick={copyCodes}>复制恢复码</Button><Button variant="subtle" onClick={() => setRecoveryCodes([])}>关闭</Button></Group>
            </Stack>
          ) : null}
          {enabled && setupStep !== 'setup' ? (
            <Group>
              <Button variant="light" loading={loading} onClick={() => openCodeAction('regenerate')}>重新生成恢复码</Button>
              <Button color="red" variant="light" loading={loading} onClick={() => openCodeAction('disable')}>禁用两步验证</Button>
            </Group>
          ) : null}
        </Stack>
      </SectionCard>
      <Modal opened={!!codeAction} onClose={closeCodeAction} title={codeAction === 'disable' ? '禁用两步验证' : '重新生成恢复码'} centered>
        <form onSubmit={actionForm.onSubmit(submitCodeAction)}>
          <Stack gap="md">
            <Text size="sm" c="dimmed">请输入当前身份验证器中的 6 位验证码以确认操作。</Text>
            <TextInput label="验证码" placeholder="6 位验证码" maxLength={6} autoFocus autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]*" name="one-time-code" {...actionForm.getInputProps('code')} />
            <Text size="sm" c="red" mih={20} style={{ visibility: actionErrorMessage ? 'visible' : 'hidden' }}>{actionErrorMessage || ' '}</Text>
            <Group justify="flex-end"><Button variant="default" onClick={closeCodeAction}>取消</Button><Button type="submit" color={codeAction === 'disable' ? 'red' : 'blue'} loading={actionLoading}>确认</Button></Group>
          </Stack>
        </form>
      </Modal>
    </>
  )
}
