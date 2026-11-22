import { Box, Group, Paper, Progress, Stack, Text } from '@mantine/core'
import type { UpgradeProgressStage, UpgradeStatus } from '@/api/types'

const labels: Record<UpgradeProgressStage, string> = {
  downloading: '正在下载更新包',
  validating: '正在校验并暂存',
  download_failed: '下载或校验失败，可重试',
  interrupted: '下载或校验已中断，可重试',
  prepared: '等待手动重启',
  applying: '正在应用更新',
  trial: '正在验证新版本',
  restoring: '正在恢复原版本',
  verifying_previous: '正在验证恢复的版本',
  committed: '更新完成',
  rolled_back: '更新失败，已恢复原版本',
  blocked: '自动恢复失败，请查看更新服务日志',
  failed: '更新失败',
  cancelled: '已放弃本次更新',
}

const timeFormat = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
const failureStages = new Set(['download_failed', 'interrupted', 'rolled_back', 'blocked', 'failed'])

export function isApplyingUpdate(phase?: string) {
  return phase === 'applying' || phase === 'trial' || phase === 'restoring' || phase === 'verifying_previous'
}

function stageLabel(stage: string) {
  return Object.prototype.hasOwnProperty.call(labels, stage) ? labels[stage as UpgradeProgressStage] : '更新状态未知'
}

type Props = {
  status: UpgradeStatus
  activating: boolean
  waiting: boolean
  unconfirmed: boolean
}

export function UpgradeProgress({ status, activating, waiting, unconfirmed }: Props) {
  const { download, progress, task } = status
  const inFlight = Boolean(download && !download.error)
  const activeTask = task && (task.phase === 'prepared' || isApplyingUpdate(task.phase)) ? task : undefined
  const matchingDownload = progress?.version === download?.version && (progress?.stage === 'downloading' || progress?.stage === 'validating')
  const history = activeTask
    ? (progress?.task_id === activeTask.id && progress.stage === activeTask.phase ? progress : undefined)
    : inFlight && !matchingDownload ? undefined : progress
  const reportedStage = activeTask?.phase || (inFlight ? history?.stage || 'downloading'
    : download?.error || (task && failureStages.has(task.phase)) ? undefined : history?.stage || task?.phase)
  const stage = reportedStage && !failureStages.has(reportedStage) && (inFlight || (reportedStage !== 'downloading' && reportedStage !== 'validating'))
    ? reportedStage : undefined
  const version = stage && (activeTask?.version || (inFlight ? download?.version : history?.version || task?.version))

  if (!stage && !activating && !waiting && !unconfirmed) return null

  const percent = stage === 'downloading' && download?.total && inFlight
    ? Math.min(100, Math.floor(100 * download.received / download.total))
    : null
  const summary = unconfirmed ? '更新结果尚未确认，请重新检查面板状态。'
    : waiting ? '正在等待面板重新连接…'
    : activating && (!stage || stage === 'prepared') ? labels.applying
    : stage ? stageLabel(stage) : null

  return (
    <Paper withBorder p="sm" component="section" aria-label="更新进度">
      <Stack gap="xs">
        <Group justify="space-between" gap="xs">
          <Text fw={600} size="sm">更新进度</Text>
          {version && <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{version}</Text>}
        </Group>
        <Text size="sm" role="status">{summary}</Text>
        {percent !== null && (
          <Group gap="xs" wrap="nowrap">
            <Progress aria-label="下载进度" value={percent} style={{ flex: 1 }} />
            <Text size="xs" w={38} ta="right">{percent}%</Text>
          </Group>
        )}
        {stage && history?.stage === stage && (
          <Box tabIndex={0} role="region" aria-label="更新日志" style={{ maxHeight: 'min(8rem, 24dvh)', overflowY: 'auto' }}>
            <Stack component="ol" gap={4} m={0} p={0} style={{ listStyle: 'none' }}>
              {history.events.slice().reverse().map((event) => (
                <Box component="li" key={event.stage}>
                  <Group gap="xs" wrap="nowrap" align="flex-start">
                    <Text component="time" dateTime={event.at} size="xs" c="dimmed" ff="monospace">{timeFormat.format(new Date(event.at))}</Text>
                    <Text size="xs">{stageLabel(event.stage)}</Text>
                  </Group>
                </Box>
              ))}
            </Stack>
          </Box>
        )}
      </Stack>
    </Paper>
  )
}
