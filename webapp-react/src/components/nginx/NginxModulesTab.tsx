import { Alert, Anchor, Badge, Button, Group, Loader, Modal, Select, Stack, Text } from '@mantine/core'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { geoAccessKeys } from '@/api/geoAccess'
import { getNginxModuleJob, getNginxModulePreflight, getNginxModuleReleases, getNginxModules, installNginxModule, requestNginxModuleBuild, uninstallNginxModule } from '@/api/nginx'
import type { NginxModule } from '@/api/nginx'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'

const modulesKey = ['nginx', 'modules'] as const
type ModuleListing = Awaited<ReturnType<typeof getNginxModules>>
const labels: Record<NginxModule['state'], string> = {
  available: '可选版本', enabled: '已启用', configured: '已配置，未确认加载', unavailable: '不可安装',
  busy: '正在检查', drifted: '版本已变化', needs_attention: '需人工检查',
}

export function NginxModulesTab() {
  const client = useQueryClient()
  const [jobID, setJobID] = useState<string>()
  const [installModule, setInstallModule] = useState<NginxModule>()
  const handledJob = useRef<string>(undefined)
  const list = useQuery({ queryKey: modulesKey, queryFn: () => getNginxModules(), retry: false, staleTime: 0,
    refetchInterval: (query) => query.state.data?.busy ? 2_000 : false })
  const refresh = useMutation({ mutationFn: () => getNginxModules(true), onSuccess: (result) => client.setQueryData(modulesKey, result) })
  const latest = list.data?.latest_job
  const latestPendingID = latest?.state === 'pending' && latest.id !== handledJob.current ? latest.id : undefined
  const currentID = jobID ?? latestPendingID
  const job = useQuery({
    queryKey: ['nginx', 'module-job', currentID], queryFn: () => getNginxModuleJob(currentID!), enabled: !!currentID,
    refetchInterval: (query) => query.state.status !== 'error' && (!query.state.data || query.state.data.state === 'pending') ? 1_500 : false,
  })
  const current = job.data ?? (currentID || latest?.state !== 'pending' ? latest : undefined)
  const moduleName = list.data?.modules.find((module) => module.id === current?.module_id)?.name || current?.module_id
  const operation = current?.action === 'uninstall' ? '卸载' : '安装'
  const pending = !!currentID && (!job.data || job.data.state === 'pending')
  const change = useMutation({ mutationFn: ({ id, tag, confirmation }: { id: string; tag?: string; confirmation: string }) => tag
    ? installNginxModule(id, tag, confirmation) : uninstallNginxModule(id, confirmation),
    onSuccess: (result) => {
      setInstallModule(undefined)
      client.setQueryData<ModuleListing>(modulesKey, (listing) => listing && { ...listing, latest_job: result })
      setJobID(result.id)
    } })

  useEffect(() => {
    if (latestPendingID) setJobID((previous) => previous ?? latestPendingID)
  }, [latestPendingID])

  useEffect(() => {
    const completed = job.data
    if (!completed || completed.state === 'pending' || handledJob.current === completed.id) return
    handledJob.current = completed.id
    if (completed.state === 'success') notifySuccess({ message: `${moduleName} ${operation}成功。` })
    client.setQueryData<ModuleListing>(modulesKey, (listing) => listing?.latest_job?.id === completed.id
      ? { ...listing, latest_job: completed } : listing)
    setJobID((previous) => previous === completed.id ? undefined : previous)
    void client.invalidateQueries({ queryKey: modulesKey })
    void client.invalidateQueries({ queryKey: geoAccessKeys.status })
  }, [client, job.data, moduleName, operation])

  function confirmUninstall(module: NginxModule) {
    const confirmation = module.uninstall_confirmation
    if (!confirmation) return
    confirmDanger({
      title: `卸载 ${module.name}`, confirmLabel: '卸载',
      message: `将以 root 执行 ${module.name} 的已签名卸载脚本。若 Nginx 配置仍依赖该模块，卸载会被拒绝。${module.id === 'geoip2' ? 'MMDB 和规则数据会保留。' : ''}`,
      onConfirm: async () => { await change.mutateAsync({ id: module.id, confirmation }) },
    })
  }

  const changing = !!pending || change.isPending || list.isFetching || refresh.isPending || !!list.data?.busy || refresh.isError

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Text size="sm" c="dimmed">{list.data?.test_feed ? '当前使用本地测试源，仅用于联调。' : '模块由官方服务交付；Agent 验证发布者签名和本机兼容性。'}</Text>
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
      {!pending && (current?.state === 'failed' || current?.state === 'needs_attention') ?
        <Alert color={current.state === 'needs_attention' ? 'yellow' : 'red'} title={`${moduleName || '模块'}：${current.state === 'needs_attention' ? '结果需人工检查' : `${operation}未完成`}`}>{current.message}</Alert> : null}
      {!pending && current?.state !== 'success' && current?.script_detail ? <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{current.script_detail}</Text> : null}
      {list.data?.modules.map((module) => (
        <SectionCard key={module.id} title={module.name}>
          <Stack gap="sm">
            {module.description ? <Text size="sm">{module.description}</Text> : null}
            {module.repository && !list.data?.test_feed ? <Anchor size="sm" href={`https://github.com/${module.repository}`} target="_blank" rel="noreferrer">{module.repository}</Anchor> : null}
            <Group gap="xs">
              <Badge color={module.state === 'enabled' ? 'green' : 'blue'}>{labels[module.state]}</Badge>
              {module.state !== 'busy' ? <Badge variant="outline" color="gray">{module.present ? '模块已存在' : '未检测到模块文件'}</Badge> : null}
            </Group>
            {module.runtime ? <Text size="sm" c="dimmed">{module.runtime}{module.version ? ` · ${module.version}` : ''}</Text> : null}
            {module.reason ? <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{module.reason}</Text> : null}
            {module.action === 'install' ? <Group><Button disabled={changing} onClick={() => { change.reset(); setInstallModule(module) }}>安装</Button></Group> : null}
            {module.uninstall_confirmation ? <Group><Button variant="subtle" color="red" disabled={changing} onClick={() => confirmUninstall(module)}>卸载</Button></Group> : null}
          </Stack>
        </SectionCard>
      ))}
      {installModule ? <ModuleInstallDialog key={installModule.id} module={installModule} busy={changing} submitting={change.isPending}
        error={change.error} onClose={() => setInstallModule(undefined)}
        onInstall={(tag, confirmation) => change.mutate({ id: installModule.id, tag, confirmation })} /> : null}
    </Stack>
  )
}

function ModuleInstallDialog({ module, busy, submitting, error, onClose, onInstall }: {
  module: NginxModule
  busy: boolean
  submitting: boolean
  error: Error | null
  onClose: () => void
  onInstall: (tag: string, confirmation: string) => void
}) {
  const [selectedTag, setSelectedTag] = useState<string | null>(null)
  const releases = useInfiniteQuery({
    queryKey: ['nginx', 'module-releases', module.id], queryFn: ({ pageParam }) => getNginxModuleReleases(module.id, pageParam),
    initialPageParam: 1, getNextPageParam: (page) => page.next_page, retry: false,
  })
  const latest = releases.data?.pages[0]?.latest
  const tag = selectedTag ?? latest
  const tags = [...new Set(releases.data?.pages.flatMap((page) => page.tags) ?? [])]
  const preflight = useQuery({
    queryKey: ['nginx', 'module-preflight', module.id, tag], queryFn: () => getNginxModulePreflight(module.id, tag!),
    enabled: !!tag && !releases.isPending, retry: false,
    refetchInterval: (query) => query.state.data?.build?.state === 'queued' || query.state.data?.build?.state === 'building' ? 5_000 : false,
  })
  const build = useMutation({ mutationFn: (version: string) => requestNginxModuleBuild(module.id, version), onSuccess: () => { void preflight.refetch() } })
  const buildStatus = preflight.data?.build
  const buildLabels = { missing: '需构建', queued: '构建排队中', building: '构建中', ready: '已构建', failed: '构建失败', unsupported: '不支持构建' }
  const confirmation = preflight.data?.confirmation
  return (
    <Modal opened title={`安装 ${module.name}`} onClose={onClose} closeOnClickOutside={!submitting} closeOnEscape={!submitting} withCloseButton={!submitting}>
      <Stack gap="sm">
        {releases.isPending ? <Loader size="sm" /> : null}
        {releases.isError ? <><ErrorAlert error={releases.error} title="无法读取模块 Release" /><Button variant="default" onClick={() => void releases.refetch()}>重试</Button></> : null}
        {tags.length ? <Select label="Release 版本" placeholder="选择要安装的版本" value={tag ?? null}
          onChange={(value) => { build.reset(); setSelectedTag(value) }} disabled={submitting || build.isPending} searchable allowDeselect={false}
          data={tags.map((value) => ({ value, label: value === latest ? `${value}（最新正式版）` : value }))} /> : null}
        {!releases.isPending && !releases.isError && !tags.length ? <Text size="sm">未找到正式 Release。</Text> : null}
        {releases.hasNextPage ? <Button variant="default" disabled={submitting} loading={releases.isFetchingNextPage}
          onClick={() => void releases.fetchNextPage()}>加载更早版本</Button> : null}
        {preflight.isFetching ? <Text size="sm">正在验证签名及运行时兼容性…</Text> : null}
        {preflight.isError ? <><ErrorAlert error={preflight.error} title="模块预检失败" /><Button variant="default" onClick={() => void preflight.refetch()}>重新检查</Button></> : null}
        {buildStatus ? <Alert title={buildLabels[buildStatus.state]}>{buildStatus.message}</Alert> : null}
        {buildStatus?.can_submit && tag ? <Button variant="light" loading={build.isPending} onClick={() => build.mutate(tag)}>申请构建</Button> : null}
        {build.isError ? <ErrorAlert error={build.error} title="构建请求未受理" /> : null}
        {preflight.data?.reason ? <>{!buildStatus ? <Alert color="yellow" title="此版本不可安装">{preflight.data.reason}</Alert> : null}<Button variant="default" disabled={preflight.isFetching} onClick={() => void preflight.refetch()}>重新检查</Button></> : null}
        {confirmation ? <Alert color="orange" title={`确认安装 ${preflight.data?.version}（${tag}）`}>
          将在 {preflight.data?.runtime || '当前 Nginx 实例'} 上以 root 执行已签名脚本。脚本可安装依赖、修改配置并重载 Nginx。
        </Alert> : null}
        {error ? <ErrorAlert error={error} title="无法开始安装" /> : null}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose} disabled={submitting}>取消</Button>
          <Button color="red" loading={submitting} disabled={busy || preflight.isFetching || preflight.isError || !confirmation || !tag}
            onClick={() => { if (tag && confirmation) onInstall(tag, confirmation) }}>确认安装</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
