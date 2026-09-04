import { Alert, Badge, Button, Code, CopyButton, Group, Loader, Modal, Paper, Progress, Stack, Text, Title } from '@mantine/core'
import { IconCheck, IconCopy, IconExternalLink, IconKey, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { PluginAuthorizationSummary, PluginCatalogItem, PluginEntitlementDeniedReason } from '@/api/plugins'
import { useBindPluginAuthorization, useCreatePluginAuthorizationDevice, useDeletePluginAuthorization, usePluginAuthorizations, usePollPluginAuthorizationDevice } from '@/api/pluginHooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

export interface PendingAuthorizedInstall {
  plugin: PluginCatalogItem
  permissions: string[]
  update: boolean
}

interface PluginAuthorizationModalProps {
  opened: boolean
  request: PendingAuthorizedInstall | null
  suggestedAuthorizations?: PluginAuthorizationSummary[]
  deniedReason?: PluginEntitlementDeniedReason
  onClose: () => void
  onAuthorized: () => Promise<void>
}

const deniedMessages: Record<PluginEntitlementDeniedReason, string> = {
  not_granted: '当前账户没有这个插件的下载权益。请在插件服务中取得授权，或手动选择另一个账户。',
  expired: '当前账户的插件权益已过期。续期后可以重试，或手动选择另一个账户。',
  instance_limit_reached: '当前账户已达到允许的实例数量。请在插件服务中移除旧实例，或手动选择另一个账户。',
  account_disabled: '当前插件服务账户已被停用。请联系管理员，或手动选择另一个账户。',
}

function accountName(account: PluginAuthorizationSummary) {
  return account.display_name || account.email_masked || account.account_id
}

function AuthorizationRow({ account, busy, onUse, onDelete }: { account: PluginAuthorizationSummary; busy: boolean; onUse: () => void; onDelete: () => void }) {
  const usable = account.status === 'active'
  return (
    <Paper withBorder p="sm" radius="md" className="pluginAuthorizationRow">
      <Group justify="space-between" align="center" wrap="nowrap" className="pluginAuthorizationRowInner">
        <div className="pluginIdentityCopy">
          <Group gap="xs"><Text fw={600}>{accountName(account)}</Text><Badge variant="light" color={usable ? 'green' : 'orange'}>{usable ? '可用' : '需要重新登录'}</Badge></Group>
          {account.email_masked && account.email_masked !== account.display_name ? <Text size="xs" c="dimmed">{account.email_masked}</Text> : null}
        </div>
        <Group gap="xs" wrap="nowrap">
          <Button size="xs" variant="default" disabled={!usable || busy} onClick={onUse}>使用此账户</Button>
          <Button size="xs" variant="subtle" color="red" aria-label={`撤销 ${accountName(account)}`} disabled={busy} onClick={onDelete}><IconTrash size={15} /></Button>
        </Group>
      </Group>
    </Paper>
  )
}

export function PluginAuthorizationModal({ opened, request, suggestedAuthorizations = [], deniedReason, onClose, onAuthorized }: PluginAuthorizationModalProps) {
  const authorizationsQuery = usePluginAuthorizations(opened)
  const createMutation = useCreatePluginAuthorizationDevice()
  const bindMutation = useBindPluginAuthorization()
  const deleteMutation = useDeletePluginAuthorization()
  const [attemptId, setAttemptId] = useState<string | null>(null)
  const [intervalSeconds, setIntervalSeconds] = useState(5)
  const [continuing, setContinuing] = useState(false)
  const [accountError, setAccountError] = useState<string>()
  const startedFor = useRef('')
  const completedAttempt = useRef('')
  const pollQuery = usePollPluginAuthorizationDevice(attemptId, intervalSeconds)
  const challenge = createMutation.data
  const accounts = useMemo(() => {
    const merged = new Map<string, PluginAuthorizationSummary>()
    suggestedAuthorizations.forEach((item) => merged.set(item.authorization_id, item))
    authorizationsQuery.data?.forEach((item) => merged.set(item.authorization_id, item))
    return Array.from(merged.values())
  }, [authorizationsQuery.data, suggestedAuthorizations])

  async function startDeviceAuthorization() {
    if (!request) return
    setAttemptId(null)
    completedAttempt.current = ''
    try {
      const next = await createMutation.mutateAsync({ pluginId: request.plugin.id, version: request.plugin.version })
      setIntervalSeconds(next.interval || 5)
      setAttemptId(next.attempt_id)
    }
    catch (error) {
      showErrorModal(error, '创建插件授权请求失败')
    }
  }

  async function bindAndContinue(authorizationId: string) {
    if (!request || continuing) return
    setContinuing(true)
    setAccountError(undefined)
    try {
      await bindMutation.mutateAsync({ pluginId: request.plugin.id, authorizationId })
      await onAuthorized()
    }
    catch (error) {
      const message = error instanceof Error ? error.message : '当前账户无法用于下载此插件。'
      setAccountError(message)
    }
    finally {
      setContinuing(false)
    }
  }

  useEffect(() => {
    if (!opened || !request) return
    const key = `${request.plugin.id}@${request.plugin.version}`
    if (startedFor.current === key) return
    startedFor.current = key
    void startDeviceAuthorization()
    // 每个授权弹窗只自动创建一次挑战；之后由用户显式重试。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened, request?.plugin.id, request?.plugin.version])

  useEffect(() => {
    if (opened) return
    startedFor.current = ''
    completedAttempt.current = ''
    setAttemptId(null)
    setAccountError(undefined)
    createMutation.reset()
    // Reset only after the parent has closed the dialog, including successful auto-continuation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened])

  useEffect(() => {
    const result = pollQuery.data
    if (!attemptId || result?.status !== 'authorized' || !result.authorization || completedAttempt.current === attemptId) return
    completedAttempt.current = attemptId
    void bindAndContinue(result.authorization.authorization_id)
    // 授权成功后的续装必须只执行一次。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attemptId, pollQuery.data?.status, pollQuery.data?.authorization?.authorization_id])

  function close() {
    startedFor.current = ''
    completedAttempt.current = ''
    setAttemptId(null)
    createMutation.reset()
    onClose()
  }

  const pollStatus = pollQuery.data?.status
  const verificationUrl = challenge?.verification_uri_complete || challenge?.verification_uri
  const expiresAt = challenge?.expires_at ? new Date(challenge.expires_at) : null
  const expiryLabel = expiresAt && !Number.isNaN(expiresAt.getTime()) ? expiresAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) : '约 10 分钟后'

  return (
    <Modal opened={opened} onClose={close} title="登录插件服务以继续" size="lg" closeOnClickOutside={false} returnFocus>
      <Stack gap="md" aria-live="polite">
        <div><Title order={3} size="h5">{request?.plugin.name}</Title><Text c="dimmed">插件服务要求确认账户权益。nxPanel 不会读取或保存你的账户密码。</Text></div>
        {deniedReason ? <Alert color="orange" title="当前账户无法下载">{deniedMessages[deniedReason]}</Alert> : null}
        {createMutation.isPending ? <Group justify="center" py="lg"><Loader size="sm" /><Text c="dimmed">正在向插件服务申请验证码…</Text></Group> : null}
        {createMutation.isError ? <ErrorAlert error={createMutation.error} title="无法连接插件服务" /> : null}
        {authorizationsQuery.isError ? <ErrorAlert error={authorizationsQuery.error} title="读取已保存账户失败" /> : null}
        {accountError ? <Alert color="orange" title="所选账户无法下载">{accountError} 系统没有自动尝试其他账户，请手动选择或重新登录。</Alert> : null}
        {challenge ? (
          <Paper p="md" radius="md" className="pluginDeviceChallenge">
            <Stack align="center" gap="sm">
              <Text size="sm" fw={600}>在插件服务页面输入验证码</Text>
              <Group gap="xs" wrap="nowrap"><Code className="pluginDeviceCode">{challenge.user_code}</Code><CopyButton value={challenge.user_code}>{({ copied, copy }) => <Button variant="subtle" size="sm" leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />} onClick={copy}>{copied ? '已复制' : '复制'}</Button>}</CopyButton></Group>
              <Button component="a" href={verificationUrl} target="_blank" rel="noopener noreferrer" leftSection={<IconExternalLink size={16} />}>打开安全授权页面</Button>
              <Text size="xs" c="dimmed">验证码将在 {expiryLabel} 失效。授权页面由官方插件服务提供。</Text>
              {pollStatus === 'pending' || pollStatus === 'slow_down' || (!pollStatus && attemptId) ? <><Progress value={100} animated size="xs" w="100%" aria-label="等待授权" /><Text size="sm" c="dimmed">等待你在新页面完成登录和授权…</Text></> : null}
              {pollStatus === 'denied' ? <Alert color="red" w="100%" title="授权已拒绝">插件没有下载。你可以重新发起授权或关闭窗口。</Alert> : null}
              {pollStatus === 'expired' ? <Alert color="yellow" w="100%" title="验证码已过期">重新发起授权后会生成新的验证码。</Alert> : null}
              {pollQuery.isError ? <ErrorAlert error={pollQuery.error} title="检查授权状态失败" /> : null}
            </Stack>
          </Paper>
        ) : null}
        {(pollStatus === 'denied' || pollStatus === 'expired' || pollQuery.isError) ? <Button variant="default" leftSection={<IconRefresh size={16} />} loading={createMutation.isPending} onClick={startDeviceAuthorization}>重新发起授权</Button> : null}
        {accounts.length ? (
          <Stack gap="xs">
            <Group gap="xs"><IconKey size={17} /><Text fw={600}>也可以手动选择已保存账户</Text></Group>
            <Text size="xs" c="dimmed">系统不会自动改用其他账户。选择后会立即重新校验这个插件的权益。</Text>
            {accounts.map((account) => <AuthorizationRow key={account.authorization_id} account={account} busy={continuing || bindMutation.isPending || deleteMutation.isPending} onUse={() => void bindAndContinue(account.authorization_id)} onDelete={() => confirmDanger({ title: '撤销插件服务账户', message: `撤销「${accountName(account)}」后，关联插件的后续下载和更新需要重新登录。已安装插件不会停用。`, confirmLabel: '撤销', onConfirm: async () => { await deleteMutation.mutateAsync(account.authorization_id); notifySuccess({ message: '插件服务账户已撤销' }) } })} />)}
          </Stack>
        ) : null}
        {continuing ? <Alert color="blue" icon={<Loader size={16} />}>授权已确认，正在继续下载并安装插件…</Alert> : null}
        <Group justify="flex-end"><Button variant="default" onClick={close} disabled={continuing}>取消下载</Button></Group>
      </Stack>
    </Modal>
  )
}
