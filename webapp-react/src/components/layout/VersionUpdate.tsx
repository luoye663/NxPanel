import { useEffect, useState } from 'react'
import { ActionIcon, Alert, Anchor, Badge, Button, Group, Popover, Progress, SegmentedControl, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { IconRefresh } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { queryKeys, useUpgradeCheckMutation } from '@/api/hooks'
import { ApiError } from '@/api/client'
import { activateUpgrade, cancelUpgrade, getUpgradeStatus, prepareUpgrade, setUpgradeChannel } from '@/api/system'
import type { UpgradeStatus } from '@/api/types'
import { notifyError } from '@/utils/notify'

const reconnectKey = 'nxpanel-upgrade-reconnect'
const reconnectLimit = 15 * 60 * 1000

type Reconnect = { id: string; startedAt: number }

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

export function VersionUpdate() {
  const [opened, setOpened] = useState(false)
  const [reconnect, setReconnect] = useState(loadReconnect)
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
  const prepare = useMutation({ mutationFn: prepareUpgrade, onSuccess: updateCache, onError: showError })
  const cancel = useMutation({ mutationFn: cancelUpgrade, onSuccess: updateCache, onError: showError })
  const activate = useMutation({
    mutationFn: activateUpgrade,
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        setReconnect(null)
        saveReconnect(null)
        showError(error)
      }
    },
  })

  useEffect(() => {
    if (!reconnect) return
    const finish = () => {
      saveReconnect(null)
      setReconnect(null)
    }
    if (task?.id === reconnect.id && task.phase === 'committed' && status?.current_version === task.version) {
      saveReconnect(null)
      window.location.reload()
      return
    }
    if (task?.id === reconnect.id && ['rolled_back', 'blocked', 'failed'].includes(task.phase)) {
      finish()
      return
    }
    const timeout = window.setTimeout(() => {
      finish()
      notifyError({ message: '未能确认更新结果。请重新检查面板状态；如无法访问，请查看更新服务日志。' })
    }, Math.max(0, reconnectLimit - (Date.now() - reconnect.startedAt)))
    return () => window.clearTimeout(timeout)
  }, [reconnect, status?.current_version, task?.id, task?.phase, task?.version])

  function confirmRestart(id: string, version: string) {
    modals.openConfirmModal({
      title: `重启并应用 ${version}？`,
      children: <Text size="sm">面板和 Agent 会短暂断开，站点 Nginx 不重启。请不要重复确认。</Text>,
      labels: { confirm: '重启并应用', cancel: '稍后' },
      onConfirm: () => {
        const pending = { id, startedAt: Date.now() }
        saveReconnect(pending)
        setReconnect(pending)
        activate.mutate(id)
      },
    })
  }

  if (!status) return null

  const prepared = task?.phase === 'prepared'
  const applying = Boolean(task && ['applying', 'trial', 'restoring', 'verifying_previous'].includes(task.phase))
  const downloading = Boolean(download && !download.error)
  const busy = check.isPending || channel.isPending || prepare.isPending || cancel.isPending || Boolean(reconnect) || applying || downloading

  return (
    <Popover opened={opened} onChange={setOpened} position="bottom-end" withArrow shadow="md" width={340}>
      <Popover.Target>
        <Badge component="button" type="button" className="versionBadge" onClick={() => {
          if (!opened && !busy && !prepared && (!status.checked_at || new Date(status.checked_at).getUTCFullYear() === 1)) {
            check.mutate()
          }
          setOpened(!opened)
        }}
          color={status.has_upgrade || prepared || reconnect ? 'orange' : 'gray'} variant={status.has_upgrade ? 'light' : 'outline'}>
          程序版本: {status.current_version || '版本未知'}
        </Badge>
      </Popover.Target>
      <Popover.Dropdown>
        <Stack gap="xs">
          <Group justify="space-between">
            <Text fw={600}>程序更新</Text>
            <ActionIcon variant="subtle" aria-label="检查更新" disabled={busy} loading={check.isPending}
              onClick={() => check.mutate(undefined, { onError: showError })}>
              <IconRefresh size={16} />
            </ActionIcon>
          </Group>
          <SegmentedControl value={status.channel || 'stable'} disabled={busy || prepared}
            data={[{ label: '稳定版', value: 'stable' }, { label: '预览版', value: 'preview' }]}
            onChange={(value) => channel.mutate(value === 'preview' ? 'preview' : 'stable')} />
          <Group justify="space-between"><Text size="sm" c="dimmed">当前版本</Text><Text size="sm">{status.current_version}</Text></Group>
          <Group justify="space-between"><Text size="sm" c="dimmed">通道版本</Text><Text size="sm">{status.latest_version || '尚未检查'}</Text></Group>
          {status.published_at && <Text size="xs" c="dimmed">发布于 {new Date(status.published_at).toLocaleString()}</Text>}
          {status.body && <Text size="xs" c="dimmed" lineClamp={3}>{status.body}</Text>}
          {status.release_url && <Anchor href={status.release_url} target="_blank" rel="noopener noreferrer" size="sm">查看发布说明</Anchor>}
          {status.error && <Alert color="red">{status.error}</Alert>}
          {download && !download.error && (
            <><Text size="sm">正在下载及校验 {download.version}</Text><Progress aria-label="下载进度" value={Math.min(100, 100 * download.received / Math.max(1, download.total))} /></>
          )}
          {download?.error && <Alert color="red">{download.error}</Alert>}
          {task?.error && <Alert color={task.phase === 'rolled_back' ? 'yellow' : 'red'}>{task.error}</Alert>}
          {(reconnect || applying) && <Alert color="blue">正在应用更新，等待面板重新连接…</Alert>}
          {status.docker && <Text size="xs" c="dimmed">仅更新容器内的面板程序；删除并重建容器后，使用所选镜像的版本。</Text>}
          {prepared && task ? (
            <>
              <Text size="sm">{task.version} 已通过验证，等待手动重启。</Text>
              <Group grow>
                <Button disabled={busy} onClick={() => confirmRestart(task.id, task.version)}>重启并应用</Button>
                <Button variant="default" disabled={busy} onClick={() => cancel.mutate(task.id)}>放弃</Button>
              </Group>
            </>
          ) : status.installable ? (
            <Button loading={prepare.isPending} disabled={busy} onClick={() => prepare.mutate(status.latest_version)}>立即更新</Button>
          ) : status.has_upgrade && !applying && (
            <Text size="xs" c="dimmed">{status.managed ? '此版本没有可验证的兼容更新包。' : '当前部署未启用本机更新服务。'}</Text>
          )}
        </Stack>
      </Popover.Dropdown>
    </Popover>
  )
}
