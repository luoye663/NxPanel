import { useEffect, useRef, useState } from 'react'
import { ActionIcon, Alert, Anchor, Badge, Box, Button, Group, Modal, Paper, SegmentedControl, SimpleGrid, Stack, Text, useModalsStack } from '@mantine/core'
import { IconRefresh } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { queryKeys, useUpgradeCheckMutation } from '@/api/hooks'
import { ApiError } from '@/api/client'
import { activateUpgrade, cancelUpgrade, getUpgradeStatus, prepareUpgrade, setUpgradeChannel } from '@/api/system'
import type { UpgradeStatus } from '@/api/types'
import { notifyError } from '@/utils/notify'
import { isApplyingUpdate, UpgradeProgress } from './UpgradeProgress'

const reconnectKey = 'nxpanel-upgrade-reconnect'
const reconnectLimit = 15 * 60 * 1000

type Reconnect = { id: string; startedAt: number; reopen?: boolean }
type RestartTarget = { id: string; version: string }
type DownloadAttempt = { version: string; previousStartedAt?: string }

function loadReconnect(): Reconnect | null {
  try {
    const stored = JSON.parse(sessionStorage.getItem(reconnectKey) || 'null') as Reconnect | null
    return stored && Date.now() - stored.startedAt < reconnectLimit ? stored : null
  } catch {
    return null
  }
}

function saveReconnect(pending: Reconnect | null) {
  try {
    if (pending) sessionStorage.setItem(reconnectKey, JSON.stringify(pending))
    else sessionStorage.removeItem(reconnectKey)
  } catch {
    return
  }
}

function showError(error: Error) {
  notifyError({ message: error.message })
}

function activationFailure(task: NonNullable<UpgradeStatus['task']>) {
  switch (task.phase) {
    case 'rolled_back': return task.error || '更新失败，已恢复原版本。'
    case 'blocked': return task.error || '自动恢复失败，请查看更新服务日志。'
    case 'failed': return task.error || '应用更新失败，请查看更新服务日志。'
    default: return null
  }
}

export function VersionUpdate() {
  const dialogs = useModalsStack(['release', 'restart'])
  const opened = dialogs.state.release
  const openDialog = dialogs.open
  const [restartTarget, setRestartTarget] = useState<RestartTarget | null>(null)
  const [reconnect, setReconnect] = useState(loadReconnect)
  const [unconfirmedTask, setUnconfirmedTask] = useState<string | null>(null)
  const downloadAttempt = useRef<DownloadAttempt | null>(null)
  const localActivation = useRef<string | null>(null)
  const cache = useQueryClient()
  const query = useQuery({
    queryKey: queryKeys.upgradeStatus,
    queryFn: getUpgradeStatus,
    retry: false,
    refetchInterval: (query) => reconnect || opened || (query.state.data?.download && !query.state.data.download.error) ? 2000 : 6 * 60 * 60 * 1000,
    refetchIntervalInBackground: Boolean(reconnect),
  })
  const status = query.data
  const task = status?.task
  const download = status?.download
  const updateCache = (data: UpgradeStatus) => cache.setQueryData(queryKeys.upgradeStatus, data)
  const check = useUpgradeCheckMutation()
  const channel = useMutation({ mutationFn: setUpgradeChannel, onSuccess: updateCache, onError: showError })
  const prepare = useMutation({
    mutationFn: prepareUpgrade,
    onMutate: () => {
      downloadAttempt.current = null
      return cache.getQueryData<UpgradeStatus>(queryKeys.upgradeStatus)?.progress?.events[0]?.at
    },
    onSuccess: async (data, version, previousStartedAt) => {
      await cache.cancelQueries({ queryKey: queryKeys.upgradeStatus, exact: true })
      if (data.download?.version === version) {
        if (data.download.error) notifyError({ message: data.download.error })
        else downloadAttempt.current = { version, previousStartedAt }
      }
      updateCache(data)
    },
    onError: showError,
  })
  const cancel = useMutation({ mutationFn: cancelUpgrade, onSuccess: updateCache, onError: showError })
  const activate = useMutation({
    mutationFn: activateUpgrade,
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        localActivation.current = null
        setReconnect(null)
        saveReconnect(null)
        showError(error)
      }
    },
  })

  useEffect(() => {
    const attempt = downloadAttempt.current
    if (!attempt || !status) return
    const download = status.download?.version === attempt.version ? status.download : undefined
    if (download && !download.error) return
    downloadAttempt.current = null
    if (download?.error) {
      notifyError({ message: download.error })
      return
    }
    const progress = status.progress
    if (!status.download && progress?.version === attempt.version && progress.events[0]?.at !== attempt.previousStartedAt) {
      if (progress.stage === 'interrupted') notifyError({ message: '下载或校验已中断，请重试。' })
      if (progress.stage === 'download_failed') notifyError({ message: '下载或校验失败，请重试；如持续失败，请查看面板服务日志。' })
    }
  }, [status])

  useEffect(() => {
    if (!reconnect) return
    const finish = () => {
      localActivation.current = null
      saveReconnect(null)
      setReconnect(null)
    }
    if (task?.id === reconnect.id && task.phase === 'committed' && status?.current_version === task.version) {
      if (reconnect.reopen) {
        openDialog('release')
        finish()
        return
      }
      saveReconnect({ ...reconnect, reopen: true, startedAt: Date.now() })
      window.location.reload()
      return
    }
    if (task?.id === reconnect.id) {
      const failure = activationFailure(task)
      if (failure) {
        const notify = localActivation.current === task.id
        finish()
        if (notify) notifyError({ message: failure })
        return
      }
    }
    const timeout = window.setTimeout(() => {
      finish()
      setUnconfirmedTask(reconnect.id)
      notifyError({ message: '未能确认更新结果。请重新检查面板状态；如无法访问，请查看更新服务日志。' })
    }, Math.max(0, reconnectLimit - (Date.now() - reconnect.startedAt)))
    return () => window.clearTimeout(timeout)
  }, [openDialog, reconnect, status?.current_version, task])

  function confirmRestart() {
    if (!restartTarget) return
    const pending = { id: restartTarget.id, startedAt: Date.now() }
    localActivation.current = pending.id
    saveReconnect(pending)
    setReconnect(pending)
    setUnconfirmedTask(null)
    dialogs.close('restart')
    activate.mutate(restartTarget.id)
  }

  if (!status) return null

  const prepared = task?.phase === 'prepared'
  const applying = isApplyingUpdate(task?.phase)
  const blocked = task?.phase === 'blocked'
  const downloading = Boolean(download && !download.error)
  const busy = check.isPending || channel.isPending || prepare.isPending || cancel.isPending || Boolean(reconnect) || applying || downloading
  const unconfirmed = Boolean(unconfirmedTask && task?.id === unconfirmedTask && (applying || prepared))
  const activating = Boolean(reconnect && !reconnect.reopen && !(task?.id === reconnect.id && activationFailure(task)))
  const checked = Boolean(status.checked_at && new Date(status.checked_at).getUTCFullYear() > 1)
  const channelVersion = status.latest_version || (
    status.error ? '检查失败' : !checked ? '尚未检查' : status.channel === 'preview' ? '暂无预览版' : '暂无稳定版'
  )

  return (
    <>
      <Badge component="button" type="button" className="versionBadge" aria-haspopup="dialog" onClick={() => {
        if (!busy && !prepared && !checked) {
          check.mutate()
        }
        dialogs.open('release')
      }}
        color={status.has_upgrade || prepared || reconnect ? 'orange' : 'gray'} variant={status.has_upgrade ? 'light' : 'outline'}>
        程序版本: {status.current_version || '版本未知'}
      </Badge>
      <Modal.Stack>
        <Modal {...dialogs.register('release')} title="程序更新" size="lg" centered
          closeButtonProps={{ 'aria-label': '关闭程序更新' }}>
          <Stack gap="sm">
            <Group justify="space-between">
              <SegmentedControl value={status.channel || 'stable'} disabled={busy || prepared}
                data={[{ label: '稳定版', value: 'stable' }, { label: '预览版', value: 'preview' }]}
                onChange={(value) => channel.mutate(value === 'preview' ? 'preview' : 'stable')} />
              <ActionIcon variant="subtle" aria-label="检查更新" disabled={busy} loading={check.isPending}
                onClick={() => check.mutate(undefined, { onError: showError })}>
                <IconRefresh size={16} />
              </ActionIcon>
            </Group>
            <SimpleGrid cols={2} spacing="xs">
              <Paper withBorder bg="var(--mantine-color-default-hover)" px="sm" py="xs">
                <Text size="xs" c="dimmed">当前版本</Text>
                <Text fw={700} size="md" style={{ overflowWrap: 'anywhere' }}>{status.current_version || '版本未知'}</Text>
              </Paper>
              <Paper withBorder bg="var(--mantine-color-default-hover)" px="sm" py="xs">
                <Text size="xs" c="dimmed">通道版本</Text>
                <Text fw={700} size="md" c={status.has_upgrade ? 'blue' : undefined} style={{ overflowWrap: 'anywhere' }}>{channelVersion}</Text>
              </Paper>
            </SimpleGrid>
            <UpgradeProgress status={status} activating={activating} waiting={(activating || applying) && query.isError} unconfirmed={unconfirmed} />
            {status.published_at && <Text size="xs" c="dimmed">发布于 {new Date(status.published_at).toLocaleString()}</Text>}
            {status.body && (
              <Stack gap={4}>
                <Text fw={600} size="sm">更新内容</Text>
                <Paper withBorder p="sm">
                  <Box tabIndex={0} role="region" aria-label="更新内容" style={{ maxHeight: 'min(24rem, 40dvh)', overflowY: 'auto' }}>
                    <Text size="sm" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{status.body}</Text>
                  </Box>
                </Paper>
              </Stack>
            )}
            {status.release_url && <Anchor href={status.release_url} target="_blank" rel="noopener noreferrer" size="sm">查看发布说明</Anchor>}
            {status.error && <Alert color="red">{status.error}</Alert>}
            {blocked && <Alert color="red">{activationFailure(task)}</Alert>}
            {status.docker && <Text size="xs" c="dimmed">仅更新容器内的面板程序；删除并重建容器后，使用所选镜像的版本。</Text>}
            {prepared && task ? (
              <>
                <Text size="sm">{task.version} 已通过验证，等待手动重启。</Text>
                <Group grow>
                  <Button disabled={busy} onClick={() => {
                    setRestartTarget({ id: task.id, version: task.version })
                    dialogs.open('restart')
                  }}>重启并应用</Button>
                  <Button variant="default" disabled={busy} onClick={() => cancel.mutate(task.id)}>放弃</Button>
                </Group>
              </>
            ) : status.installable ? (
              <Button loading={prepare.isPending} disabled={busy} onClick={() => prepare.mutate(status.latest_version)}>立即更新</Button>
            ) : status.has_upgrade && !applying && !blocked && (
              <Text size="xs" c="dimmed">{status.managed ? '此版本没有可验证的兼容更新包。' : '当前部署未启用本机更新服务。'}</Text>
            )}
          </Stack>
        </Modal>
        <Modal {...dialogs.register('restart')} title={`重启并应用 ${restartTarget?.version ?? ''}？`} centered
          closeButtonProps={{ 'aria-label': '关闭重启确认' }}>
          <Stack gap="sm">
            <Text size="sm">面板和 Agent 会短暂断开，站点 Nginx 不重启。请不要重复确认。</Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => dialogs.close('restart')}>稍后</Button>
              <Button onClick={confirmRestart}>重启并应用</Button>
            </Group>
          </Stack>
        </Modal>
      </Modal.Stack>
    </>
  )
}
