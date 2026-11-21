import { Alert, Badge, Button, Group, Loader, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { geoAccessKeys } from '@/api/geoAccess'
import { getNginxModuleJob, getNginxModules, installNginxModule, uninstallNginxModule } from '@/api/nginx'
import type { NginxModule } from '@/api/nginx'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'

const modulesKey = ['nginx', 'modules'] as const
const labels: Record<NginxModule['state'], string> = {
  available: '兼容', enabled: '已启用', configured: '已配置，未确认加载', unavailable: '不可安装',
  busy: '正在检查', drifted: '版本已变化', needs_attention: '需人工检查',
}

export function NginxModulesTab() {
  const client = useQueryClient()
  const [jobID, setJobID] = useState<string>()
  const list = useQuery({ queryKey: modulesKey, queryFn: () => getNginxModules(), retry: false, staleTime: 0,
    refetchInterval: (query) => query.state.data?.busy ? 2_000 : false })
  const refresh = useMutation({ mutationFn: () => getNginxModules(true), onSuccess: (result) => client.setQueryData(modulesKey, result) })
  const latest = list.data?.latest_job
  const currentID = latest?.state === 'pending' ? latest.id : jobID
  const job = useQuery({
    queryKey: ['nginx', 'module-job', currentID], queryFn: () => getNginxModuleJob(currentID!), enabled: !!currentID,
    refetchInterval: (query) => query.state.status !== 'error' && (!query.state.data || query.state.data.state === 'pending') ? 1_500 : false,
  })
  const current = job.data ?? latest
  const moduleName = list.data?.modules.find((module) => module.id === current?.module_id)?.name || current?.module_id
  const operation = current?.action === 'uninstall' ? '卸载' : '安装'
  const pending = (currentID && !job.data) || current?.state === 'pending'
  const change = useMutation({ mutationFn: ({ module, uninstall }: { module: NginxModule; uninstall: boolean }) => uninstall
    ? uninstallNginxModule(module.id, module.uninstall_confirmation!) : installNginxModule(module.id, module.confirmation!),
    onSuccess: (result) => setJobID(result.id) })

  useEffect(() => {
    if (!job.data || job.data.state === 'pending') return
    const completedID = job.data.id
    void client.invalidateQueries({ queryKey: modulesKey }).then(() => {
      if (client.getQueryData<Awaited<ReturnType<typeof getNginxModules>>>(modulesKey)?.latest_job?.id === completedID) {
        setJobID((previous) => previous === completedID ? undefined : previous)
      }
    })
    void client.invalidateQueries({ queryKey: geoAccessKeys.status })
  }, [client, job.data])

  function confirm(module: NginxModule, uninstall = false) {
    const action = uninstall ? '卸载' : '安装并启用'
    confirmDanger({
      title: `${action} ${module.name}`, confirmLabel: action,
      message: uninstall ? `将以 root 执行 ${module.name} 的已签名卸载脚本。若 Nginx 配置仍依赖该模块，卸载会被拒绝。${module.id === 'geoip2' ? 'MMDB 和规则数据会保留。' : ''}`
        : `将为 ${module.runtime || '当前 Nginx 实例'}${action} ${module.name}。已签名脚本以 root 运行，可从系统软件源安装依赖，修改并重载 Nginx。`,
      onConfirm: async () => { await change.mutateAsync({ module, uninstall }) },
    })
  }

  const changing = !!pending || change.isPending || list.isFetching || refresh.isPending || !!list.data?.busy || refresh.isError

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">{list.data?.test_feed ? '当前使用本地测试源，仅用于联调。' : '在线目录来自 GitHub，Agent 验签后只安装与当前 Nginx / OpenResty 匹配的构建。'}</Text>
        <Button variant="default" size="xs" loading={list.isFetching || refresh.isPending} onClick={() => refresh.mutate()}>刷新在线目录</Button>
      </Group>
      {list.isLoading ? <Loader size="sm" /> : null}
      {list.isError ? <ErrorAlert error={list.error} title="加载模块列表失败" /> : null}
      {refresh.isError ? <ErrorAlert error={refresh.error} title="刷新模块列表失败" /> : null}
      {list.data?.feed_state === 'stale' || list.data?.feed_state === 'error' ? <Alert color="yellow" title={list.data.feed_state === 'stale' ? '刷新失败，在线条目仅供查看' : '在线目录不可用'}>{list.data.feed_message}</Alert> : null}
      {list.data?.busy && !pending ? <Alert>模块操作或 Nginx 配置检查正在进行。</Alert> : null}
      {list.data?.feed_state === 'online' && !list.data.busy && list.data.modules.length === 0 ? <Text size="sm" c="dimmed">当前目录没有可展示的模块。</Text> : null}
      {job.isError ? <><ErrorAlert error={job.error} title="查询模块任务失败" /><Group><Button size="xs" variant="default" onClick={() => void job.refetch()}>重新查询任务</Button></Group></> : null}
      {pending ? <Alert title={`正在${operation}${moduleName ? ` ${moduleName}` : '模块'}`}>正在检查依赖、验证配置或等待 Nginx 新 worker 接管。可以离开此页，返回后继续查看。</Alert> : null}
      {!pending && current?.state === 'success' ? <Alert color="green">{moduleName} {operation}成功。</Alert> : null}
      {!pending && (current?.state === 'failed' || current?.state === 'needs_attention') ?
        <Alert color={current.state === 'needs_attention' ? 'yellow' : 'red'} title={`${moduleName || '模块'}：${current.state === 'needs_attention' ? '结果需人工检查' : `${operation}未完成`}`}>{current.message}</Alert> : null}
      {!pending && current?.script_detail ? <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{current.script_detail}</Text> : null}
      {list.data?.modules.map((module) => (
        <SectionCard key={module.id} title={module.name}>
          <Stack gap="sm">
            {module.description ? <Text size="sm">{module.description}</Text> : null}
            <Group gap="xs">
              <Badge color={module.state === 'enabled' ? 'green' : 'blue'}>{labels[module.state]}</Badge>
              {module.state !== 'busy' ? <Badge variant="outline" color="gray">{module.present ? '模块已存在' : '未检测到模块文件'}</Badge> : null}
            </Group>
            {module.runtime ? <Text size="sm" c="dimmed">{module.runtime}{module.version ? ` · ${module.version}` : ''}</Text> : null}
            {module.reason ? <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{module.reason}</Text> : null}
            {module.confirmation ? <Group><Button disabled={changing} onClick={() => confirm(module)}>安装</Button></Group> : null}
            {module.uninstall_confirmation ? <Group><Button variant="subtle" color="red" disabled={changing} onClick={() => confirm(module, true)}>卸载</Button></Group> : null}
          </Stack>
        </SectionCard>
      ))}
    </Stack>
  )
}
