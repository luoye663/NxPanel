import { ActionIcon, Alert, Badge, Button, Checkbox, Group, NumberInput, Select, SimpleGrid, Stack, Tabs, Text, TextInput, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDownload, IconRefresh, IconRestore, IconTrash, IconUpload } from '@tabler/icons-react'
import type { MRT_ColumnDef } from 'mantine-react-table'
import { useEffect, useMemo, useState } from 'react'
import { createSiteBackup, deleteSiteBackup, getSiteBackupSchedule, listSiteBackups, restoreSiteBackup, saveSiteBackupSchedule, siteBackupDownloadURL, siteBackupTaskStreamURL } from '@/api/siteBackup'
import type { SiteBackup, SiteBackupScheduleSaveRequest, SiteBackupTask, SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { TimeCell } from '@/components/common/TimeCell'
import { DataTable } from '@/components/tables/DataTable'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

interface SiteBackupTabProps {
  site: Pick<SiteDetail, 'id'>
}

const backupTypeOptions = [
  { value: 'config', label: '配置备份' },
  { value: 'root', label: '根目录备份' },
  { value: 'ssl', label: '证书备份' },
  { value: 'full', label: '完整备份' },
]

const backupTypeLabels: Record<string, string> = {
  config: '配置',
  root: '根目录',
  ssl: '证书',
  full: '完整',
}

export function SiteBackupTab({ site }: SiteBackupTabProps) {
  const queryClient = useQueryClient()
  const queryKey = ['site-detail', site.id, 'backups']
  const form = useForm({ initialValues: { backup_type: 'full', name: '', backup_dir: '' } })
  const scheduleForm = useForm({ initialValues: { enabled: false, backup_type: 'full', backup_dir: '', retention_count: 7, schedule_type: 'daily', schedule_time: '02:00', weekday: 1, month_day: 1, last_run_at: null as string | null } })
  const [task, setTask] = useState<SiteBackupTask | null>(null)
  const backupsQuery = useQuery({ queryKey, queryFn: () => listSiteBackups(site.id) })
  const scheduleQuery = useQuery({ queryKey: ['site-detail', site.id, 'backup-schedule'], queryFn: () => getSiteBackupSchedule(site.id) })
  const createMutation = useMutation({ mutationFn: () => createSiteBackup(site.id, form.values) })
  const restoreMutation = useMutation({ mutationFn: (backup: SiteBackup) => restoreSiteBackup(site.id, backup.id, restoreOptions(backup.backup_type)) })
  const deleteMutation = useMutation({ mutationFn: (backup: SiteBackup) => deleteSiteBackup(site.id, backup.id) })
  const saveScheduleMutation = useMutation({ mutationFn: (payload: SiteBackupScheduleSaveRequest) => saveSiteBackupSchedule(site.id, payload) })

  useEffect(() => {
    if (scheduleQuery.data) scheduleForm.setValues(scheduleQuery.data)
  }, [scheduleQuery.data])

  useEffect(() => {
    if (!task || task.status !== 'running') return
    const source = new EventSource(siteBackupTaskStreamURL(site.id, task.task_id))
    source.onmessage = (event) => {
      if (event.data === '[DONE]') {
        source.close()
        queryClient.invalidateQueries({ queryKey })
        return
      }
      try {
        setTask(JSON.parse(event.data) as SiteBackupTask)
      } catch {
        // SSE 数据只展示任务摘要，解析失败时保持当前状态，避免影响页面其它操作。
      }
    }
    source.onerror = () => source.close()
    return () => source.close()
  }, [task?.task_id, task?.status, site.id])

  const columns = useMemo<MRT_ColumnDef<SiteBackup>[]>(() => [
    { accessorKey: 'name', header: '名称', size: 240 },
    { accessorKey: 'backup_type', header: '类型', size: 100, Cell: ({ row }) => <Badge variant="light">{backupTypeLabels[row.original.backup_type] || row.original.backup_type}</Badge> },
    { accessorKey: 'size_bytes', header: '大小', size: 110, Cell: ({ row }) => formatBytes(row.original.size_bytes) },
    { accessorKey: 'status', header: '状态', size: 110, Cell: ({ row }) => <Badge color={row.original.status === 'success' ? 'green' : row.original.status === 'failed' ? 'red' : 'yellow'}>{statusText(row.original.status)}</Badge> },
    { accessorKey: 'created_at', header: '创建时间', size: 180, Cell: ({ row }) => <TimeCell value={row.original.created_at} /> },
  ], [])

  async function handleCreate() {
    try {
      await createMutation.mutateAsync()
        .then((createdTask) => setTask(createdTask))
      notifySuccess({ message: '备份任务已启动' })
      form.setFieldValue('name', '')
    } catch (error) {
      showErrorModal(error, '创建站点备份失败')
    }
  }

  function handleRestore(backup: SiteBackup) {
    confirmDanger({
      title: '恢复站点备份',
      message: `确认恢复备份「${backup.name}」？该操作会覆盖当前对应文件，后端会先创建快照并在配置测试失败时回滚。`,
      confirmLabel: '确认恢复',
      errorTitle: '恢复站点备份失败',
      onConfirm: async () => {
        await restoreMutation.mutateAsync(backup)
          .then((createdTask) => setTask(createdTask))
        notifySuccess({ message: '恢复任务已启动' })
      },
    })
  }

  async function handleSaveSchedule() {
    try {
      const payload: SiteBackupScheduleSaveRequest = {
        enabled: scheduleForm.values.enabled,
        backup_type: scheduleForm.values.backup_type,
        backup_dir: scheduleForm.values.backup_dir,
        retention_count: scheduleForm.values.retention_count,
        schedule_type: scheduleForm.values.schedule_type,
        schedule_time: scheduleForm.values.schedule_time,
        weekday: scheduleForm.values.weekday,
        month_day: scheduleForm.values.month_day,
      }
      await saveScheduleMutation.mutateAsync(payload)
      notifySuccess({ message: '定时备份配置已保存' })
      await queryClient.invalidateQueries({ queryKey: ['site-detail', site.id, 'backup-schedule'] })
    } catch (error) {
      showErrorModal(error, '保存定时备份配置失败')
    }
  }

  function handleDelete(backup: SiteBackup) {
    confirmDanger({
      title: '删除站点备份',
      message: `确认删除备份「${backup.name}」？删除后无法从页面恢复。`,
      confirmLabel: '确认删除',
      errorTitle: '删除站点备份失败',
      onConfirm: async () => {
        await deleteMutation.mutateAsync(backup)
        notifySuccess({ message: '站点备份已删除' })
        await queryClient.invalidateQueries({ queryKey })
      },
    })
  }

  return (
    <Stack gap="md">
      {backupsQuery.isError ? <ErrorAlert error={backupsQuery.error} title="加载站点备份失败" /> : null}
      {task ? <Alert color={task.status === 'failed' ? 'red' : task.status === 'success' ? 'green' : 'blue'} title={task.message}>{task.error || (task.status === 'running' ? '任务正在后台执行，可保持页面打开查看完成状态。' : '任务已结束。')}</Alert> : null}

      <SectionCard title="备份策略" description="手动备份用于立即执行一次任务，定时备份用于按周期自动创建并清理旧备份。">
        <Tabs defaultValue="manual" keepMounted={false}>
          <Tabs.List>
            <Tabs.Tab value="manual">手动备份</Tabs.Tab>
            <Tabs.Tab value="schedule">定时备份</Tabs.Tab>
          </Tabs.List>

          <Tabs.Panel value="manual">
            <Stack gap="md">
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
                <Select label="备份类型" data={backupTypeOptions} allowDeselect={false} {...form.getInputProps('backup_type')} />
                <TextInput label="备份名称" placeholder="留空自动生成" {...form.getInputProps('name')} />
                <TextInput label="备份位置" placeholder="留空使用默认目录" {...form.getInputProps('backup_dir')} />
              </SimpleGrid>
              <Group justify="space-between" align="center">
                <Text size="sm" c="dimmed">如自定义备份位置不在白名单内，请在 `agent.allowed_roots` 中加入该目录。</Text>
                <Button leftSection={<IconUpload size={16} />} loading={createMutation.isPending} onClick={handleCreate}>创建备份</Button>
              </Group>
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="schedule">
            <Stack gap="md">
              <Group justify="space-between" align="center">
                <Checkbox label="启用定时备份" checked={scheduleForm.values.enabled} onChange={(event) => scheduleForm.setFieldValue('enabled', event.currentTarget.checked)} />
                <Button variant="light" loading={saveScheduleMutation.isPending || scheduleQuery.isFetching} onClick={handleSaveSchedule}>保存定时备份</Button>
              </Group>
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="md">
                <Select label="备份类型" data={backupTypeOptions} allowDeselect={false} {...scheduleForm.getInputProps('backup_type')} />
                <Select label="周期" data={[{ value: 'daily', label: '每日' }, { value: 'weekly', label: '每周' }, { value: 'monthly', label: '每月' }]} allowDeselect={false} {...scheduleForm.getInputProps('schedule_type')} />
                <TextInput label="执行时间" placeholder="02:00" {...scheduleForm.getInputProps('schedule_time')} />
                <NumberInput label="保留数量" min={1} max={365} {...scheduleForm.getInputProps('retention_count')} />
                {scheduleForm.values.schedule_type === 'weekly' ? <NumberInput label="星期（0=周日）" min={0} max={6} {...scheduleForm.getInputProps('weekday')} /> : null}
                {scheduleForm.values.schedule_type === 'monthly' ? <NumberInput label="每月日期" min={1} max={31} {...scheduleForm.getInputProps('month_day')} /> : null}
                <TextInput label="备份位置" placeholder="留空使用默认目录" {...scheduleForm.getInputProps('backup_dir')} />
              </SimpleGrid>
            </Stack>
          </Tabs.Panel>
        </Tabs>
      </SectionCard>

      <SectionCard title="备份列表" description="下载、恢复或删除当前网站已有备份。">
        <DataTable
          columns={columns}
          data={backupsQuery.data?.items || []}
          loading={backupsQuery.isLoading || backupsQuery.isFetching}
          emptyText="暂无站点备份"
          toolbarActions={<Button variant="light" leftSection={<IconRefresh size={16} />} loading={backupsQuery.isFetching} onClick={() => backupsQuery.refetch()}>刷新</Button>}
          renderRowActions={({ row }) => (
            <Group gap={4} wrap="nowrap">
              <Tooltip label="下载"><ActionIcon component="a" href={siteBackupDownloadURL(site.id, row.original.id)} variant="subtle" aria-label="下载备份"><IconDownload size={16} /></ActionIcon></Tooltip>
              <Tooltip label="恢复"><ActionIcon variant="subtle" color="blue" aria-label="恢复备份" loading={restoreMutation.isPending} onClick={() => handleRestore(row.original)}><IconRestore size={16} /></ActionIcon></Tooltip>
              <Tooltip label="删除"><ActionIcon variant="subtle" color="red" aria-label="删除备份" loading={deleteMutation.isPending} onClick={() => handleDelete(row.original)}><IconTrash size={16} /></ActionIcon></Tooltip>
            </Group>
          )}
        />
      </SectionCard>
    </Stack>
  )
}

function restoreOptions(type: string) {
  return {
    restore_config: type === 'config' || type === 'full',
    restore_root: type === 'root' || type === 'full',
    restore_ssl: type === 'ssl' || type === 'full',
  }
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function statusText(status: string): string {
  if (status === 'success') return '成功'
  if (status === 'failed') return '失败'
  if (status === 'pending') return '进行中'
  return status
}
