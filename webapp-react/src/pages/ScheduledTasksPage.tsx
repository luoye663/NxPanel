import { Button, Group, Loader, Stack, Tabs, Text } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconRefresh } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import {
  createScheduledTask,
  deleteScheduledTask,
  getScheduledTaskDefinitions,
  getScheduledTasks,
  runScheduledTaskNow,
  toggleScheduledTask,
  updateScheduledTask,
  type CreateScheduledTaskPayload,
  type ScheduledTaskItem,
  type UpdateScheduledTaskPayload,
} from '@/api/scheduledTasks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { TaskLogTab } from '@/components/logs/TaskLogTab'
import { ScheduledTaskEditorModal } from '@/components/scheduled-tasks/ScheduledTaskEditorModal'
import { ScheduledTaskList } from '@/components/scheduled-tasks/ScheduledTaskList'
import { ScheduledTaskRunDrawer } from '@/components/scheduled-tasks/ScheduledTaskRunDrawer'
import { confirmDanger } from '@/utils/confirm'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const taskQueryKey = ['scheduled-tasks'] as const

interface ScheduledTaskListResponse {
  items: ScheduledTaskItem[]
}

export function ScheduledTasksPage() {
  const queryClient = useQueryClient()
  const [editorOpened, editor] = useDisclosure(false)
  const [drawerOpened, drawer] = useDisclosure(false)
  const [editingTask, setEditingTask] = useState<ScheduledTaskItem | null>(null)
  const [historyTask, setHistoryTask] = useState<ScheduledTaskItem | null>(null)
  const [togglingId, setTogglingId] = useState<string>()
  const definitionsQuery = useQuery({ queryKey: ['scheduled-task-definitions'], queryFn: getScheduledTaskDefinitions })
  const tasksQuery = useQuery({ queryKey: taskQueryKey, queryFn: getScheduledTasks })
  const definitions = definitionsQuery.data?.items ?? []
  const tasks = tasksQuery.data?.items ?? []

  useEffect(() => {
    if (!tasks.some((task) => task.status === 'running')) return
    const timer = window.setInterval(() => { void tasksQuery.refetch() }, 1000)
    return () => window.clearInterval(timer)
  }, [tasks, tasksQuery])

  const saveMutation = useMutation({
    mutationFn: (payload: CreateScheduledTaskPayload | UpdateScheduledTaskPayload) => editingTask ? updateScheduledTask(editingTask.id, payload) : createScheduledTask(payload as CreateScheduledTaskPayload),
    onSuccess: async () => {
      notifySuccess({ message: editingTask ? '计划任务已更新' : '计划任务已创建' })
      editor.close()
      setEditingTask(null)
      await queryClient.invalidateQueries({ queryKey: taskQueryKey })
    },
  })
  const toggleMutation = useMutation({
    mutationFn: ({ task, enabled }: { task: ScheduledTaskItem; enabled: boolean }) => toggleScheduledTask(task.id, enabled),
    onMutate: ({ task }) => setTogglingId(task.id),
    onSettled: async () => {
      setTogglingId(undefined)
      await queryClient.invalidateQueries({ queryKey: taskQueryKey })
    },
  })
  const runMutation = useMutation({
    mutationFn: (task: ScheduledTaskItem) => runScheduledTaskNow(task.id),
    onMutate: async (task) => {
      await queryClient.cancelQueries({ queryKey: taskQueryKey })
      const previous = queryClient.getQueryData<ScheduledTaskListResponse>(taskQueryKey)
      queryClient.setQueryData<ScheduledTaskListResponse>(taskQueryKey, (current) => ({
        items: (current?.items ?? []).map((item) => (item.id === task.id ? { ...item, status: 'running' } : item)),
      }))
      return { previous }
    },
    onError: (_error, _task, context) => {
      if (context?.previous) queryClient.setQueryData(taskQueryKey, context.previous)
    },
    onSuccess: async () => {
      notifySuccess({ message: '已提交立即执行请求' })
      window.setTimeout(() => { void queryClient.invalidateQueries({ queryKey: taskQueryKey }) }, 800)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (task: ScheduledTaskItem) => deleteScheduledTask(task.id),
    onSuccess: async () => {
      notifySuccess({ message: '计划任务已删除' })
      await queryClient.invalidateQueries({ queryKey: taskQueryKey })
    },
  })

  function openCreate() {
    setEditingTask(null)
    editor.open()
  }

  function openEdit(task: ScheduledTaskItem) {
    setEditingTask(task)
    editor.open()
  }

  function openHistory(task: ScheduledTaskItem) {
    setHistoryTask(task)
    drawer.open()
  }

  async function handleSave(payload: CreateScheduledTaskPayload | UpdateScheduledTaskPayload) {
    try {
      await saveMutation.mutateAsync(payload)
    } catch (error) {
      showErrorModal(error, editingTask ? '保存计划任务失败' : '创建计划任务失败')
    }
  }

  async function handleToggle(task: ScheduledTaskItem, enabled: boolean) {
    try {
      await toggleMutation.mutateAsync({ task, enabled })
    } catch (error) {
      showErrorModal(error, enabled ? '启用计划任务失败' : '禁用计划任务失败')
    }
  }

  async function handleRun(task: ScheduledTaskItem) {
    try {
      await runMutation.mutateAsync(task)
    } catch (error) {
      showErrorModal(error, '立即执行计划任务失败')
    }
  }

  function handleDelete(task: ScheduledTaskItem) {
    confirmDanger({
      title: '删除计划任务',
      message: `确定要删除计划任务 "${task.name}" 吗？执行历史会一并删除。`,
      confirmLabel: '删除',
      errorTitle: '删除计划任务失败',
      onConfirm: async () => { await deleteMutation.mutateAsync(task) },
    })
  }

  return (
    <PageShell>
      <SectionCard>
        <Tabs defaultValue="tasks" keepMounted={false}>
          <Tabs.List>
            <Tabs.Tab value="tasks">计划任务</Tabs.Tab>
            <Tabs.Tab value="logs">计划任务日志</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="tasks">
            <Stack gap="md">
              <Group justify="space-between">
               
                <Group gap="xs">
                  <Button variant="light" leftSection={<IconRefresh size={16} />} loading={tasksQuery.isFetching} onClick={() => tasksQuery.refetch()}>刷新</Button>
                  <Button leftSection={<IconPlus size={16} />} disabled={definitions.length === 0} onClick={openCreate}>新增任务</Button>
                </Group>
              </Group>
              {definitions.length === 0 ? <Text size="sm" c="dimmed">暂无已注册任务类型，请检查后端计划任务 handler 注册是否成功。</Text> : null}
              {tasksQuery.isError ? <ErrorAlert error={tasksQuery.error} title="加载计划任务失败" /> : null}
              {definitionsQuery.isError ? <ErrorAlert error={definitionsQuery.error} title="加载任务类型失败" /> : null}
              {tasksQuery.isLoading ? <Group justify="center" py="xl"><Loader size="sm" /></Group> : (
                <ScheduledTaskList
                  tasks={tasks}
                  definitions={definitions}
                  togglingId={togglingId}
                  onEdit={openEdit}
                  onToggle={handleToggle}
                  onRun={handleRun}
                  onHistory={openHistory}
                  onDelete={handleDelete}
                />
              )}
            </Stack>
          </Tabs.Panel>
          <Tabs.Panel value="logs">
           
            <TaskLogTab />
          </Tabs.Panel>
        </Tabs>
      </SectionCard>
      <ScheduledTaskEditorModal
        opened={editorOpened}
        definitions={definitions}
        task={editingTask}
        saving={saveMutation.isPending}
        onClose={() => { editor.close(); setEditingTask(null) }}
        onSubmit={handleSave}
      />
      <ScheduledTaskRunDrawer opened={drawerOpened} task={historyTask} onClose={drawer.close} />
    </PageShell>
  )
}
