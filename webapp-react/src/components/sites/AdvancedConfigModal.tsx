import { Alert, Button, Group, LoadingOverlay, Modal, NumberInput, Radio, ScrollArea, Select, Stack, Switch, Tabs, Text, Textarea, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMediaQuery } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconFileText, IconLock, IconRotateClockwise, IconServer } from '@tabler/icons-react'
import { Suspense, lazy, useEffect, useState } from 'react'
import { listCertificates } from '@/api/certificates'
import { getHTTPSHijack, getDefaultPages, getDefaultSite, getLogRotation, updateHTTPSHijack, updateDefaultPages, updateDefaultSite, updateLogRotation } from '@/api/settings'
import { getSites } from '@/api/sites'
import type { DefaultPagesSettings, HTTPSHijackSettings, LogRotateSettings } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { notifySuccess } from '@/utils/notify'

const NginxCodeEditor = lazy(() => import('@/components/editor/NginxCodeEditor'))

interface AdvancedConfigModalProps {
  opened: boolean
  onClose: () => void
}

type AdvancedTab = 'pages' | 'default-site' | 'https-hijack' | 'log-rotation'

const emptyPages: DefaultPagesSettings = {
  new_site_page: '',
  page_404: '',
  site_not_found_page: '',
  site_disabled_page: '',
}

const intervalOptions = [
  { label: '每 1 小时', value: '1h' },
  { label: '每 6 小时', value: '6h' },
  { label: '每 12 小时', value: '12h' },
  { label: '每 24 小时（每天）', value: '24h' },
  { label: '每 168 小时（每周）', value: '168h' },
]

const minSizeOptions = [
  { label: '50MB', value: '50M' },
  { label: '100MB', value: '100M' },
  { label: '200MB', value: '200M' },
  { label: '500MB', value: '500M' },
  { label: '1GB', value: '1G' },
  { label: '5GB', value: '5G' },
]

export function AdvancedConfigModal({ opened, onClose }: AdvancedConfigModalProps) {
  const isSmallScreen = useMediaQuery('(max-width: 48rem)')
  const [activeTab, setActiveTab] = useState<AdvancedTab>('pages')
  const [loadedTabs, setLoadedTabs] = useState<Set<AdvancedTab>>(() => new Set(['pages']))

  function switchTab(value: string | null) {
    if (!value) return
    const nextTab = value as AdvancedTab
    setActiveTab(nextTab)
    setLoadedTabs((current) => new Set(current).add(nextTab))
  }

  useEffect(() => {
    if (!opened) {
      setActiveTab('pages')
      setLoadedTabs(new Set(['pages']))
    }
  }, [opened])

  return (
    <Modal opened={opened} onClose={onClose} title="高级配置" size="min(1160px, 94vw)" fullScreen={isSmallScreen} closeOnClickOutside={false} classNames={{ body: 'advancedConfigModalBody' }}>
      <Tabs value={activeTab} onChange={switchTab} orientation={isSmallScreen ? 'horizontal' : 'vertical'} className="advancedConfigTabs" keepMounted={false}>
        <Tabs.List className="advancedConfigTabList">
          <Tabs.Tab value="pages" leftSection={<IconFileText size={15} />}>修改默认页面</Tabs.Tab>
          <Tabs.Tab value="default-site" leftSection={<IconServer size={15} />}>默认站点</Tabs.Tab>
          <Tabs.Tab value="https-hijack" leftSection={<IconLock size={15} />}>HTTPS 防窜站</Tabs.Tab>
          <Tabs.Tab value="log-rotation" leftSection={<IconRotateClockwise size={15} />}>日志切割</Tabs.Tab>
        </Tabs.List>

        <ScrollArea className="advancedConfigPanelScroll">
          <Tabs.Panel value="pages">
            <DefaultPagesPanel enabled={loadedTabs.has('pages')} />
          </Tabs.Panel>
          <Tabs.Panel value="default-site">
            <DefaultSitePanel enabled={loadedTabs.has('default-site')} />
          </Tabs.Panel>
          <Tabs.Panel value="https-hijack">
            <HTTPSHijackPanel enabled={loadedTabs.has('https-hijack')} />
          </Tabs.Panel>
          <Tabs.Panel value="log-rotation">
            <LogRotationPanel enabled={loadedTabs.has('log-rotation')} />
          </Tabs.Panel>
        </ScrollArea>
      </Tabs>
    </Modal>
  )
}

function DefaultPagesPanel({ enabled }: { enabled: boolean }) {
  const [pageTab, setPageTab] = useState<keyof DefaultPagesSettings>('new_site_page')
  const pagesForm = useForm<DefaultPagesSettings>({ initialValues: emptyPages })
  const pagesQuery = useQuery({ queryKey: ['settings', 'default-pages'], queryFn: getDefaultPages, enabled })
  const savePagesMutation = useMutation({ mutationFn: updateDefaultPages })

  useEffect(() => {
    if (pagesQuery.data) pagesForm.setValues(pagesQuery.data)
  }, [pagesQuery.data])

  async function savePages(values: DefaultPagesSettings) {
    const result = await savePagesMutation.mutateAsync(values)
    pagesForm.setValues(result)
    notifySuccess({ message: '默认页面已保存' })
  }

  return (
    <form onSubmit={pagesForm.onSubmit(savePages)}>
      <Stack gap="md" className="advancedConfigPanel">
        <Alert color="blue" title="默认页面模板">
          这些页面模板在新建网站时会复制到网站根目录。支持变量：{'{{domain}}'} 替换为站点域名，{'{{year}}'} 替换为当前年份。
        </Alert>
        {pagesQuery.isError ? <ErrorAlert error={pagesQuery.error} title="加载默认页面失败" /> : null}
        {savePagesMutation.isError ? <ErrorAlert error={savePagesMutation.error} title="保存默认页面失败" /> : null}
        <Tabs value={pageTab} onChange={(value) => value && setPageTab(value as keyof DefaultPagesSettings)} keepMounted>
          <Tabs.List>
            <Tabs.Tab value="new_site_page">新建网站默认页面</Tabs.Tab>
            <Tabs.Tab value="page_404">404 错误页面</Tabs.Tab>
            <Tabs.Tab value="site_not_found_page">网站不存在默认页面</Tabs.Tab>
            <Tabs.Tab value="site_disabled_page">网站停用默认页面</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value={pageTab}>
            <div className="nginxEditorShell advancedConfigTemplateEditor">
              <Suspense fallback={<Text size="sm" c="dimmed" p="md">编辑器加载中...</Text>}>
                <NginxCodeEditor language="html" value={pagesForm.values[pageTab]} onChange={(value) => pagesForm.setFieldValue(pageTab, value)} />
              </Suspense>
            </div>
          </Tabs.Panel>
        </Tabs>
        <Group justify="flex-end">
          <Button type="submit" loading={savePagesMutation.isPending || pagesQuery.isFetching}>保存默认页面</Button>
        </Group>
      </Stack>
    </form>
  )
}

function DefaultSitePanel({ enabled }: { enabled: boolean }) {
  const defaultSiteForm = useForm({ initialValues: { site_id: '' } })
  const defaultSiteQuery = useQuery({ queryKey: ['settings', 'default-site'], queryFn: getDefaultSite, enabled })
  const sitesQuery = useQuery({ queryKey: ['sites', 'advanced-config-options'], queryFn: () => getSites({ page: 1, page_size: 200 }), enabled })
  const saveDefaultSiteMutation = useMutation({ mutationFn: updateDefaultSite })

  useEffect(() => {
    if (defaultSiteQuery.data) defaultSiteForm.setValues({ site_id: defaultSiteQuery.data.site_id || '' })
  }, [defaultSiteQuery.data])

  async function saveDefaultSite(values: { site_id: string }) {
    await saveDefaultSiteMutation.mutateAsync(values)
    notifySuccess({ message: '默认站点已保存' })
  }

  const siteOptions = (sitesQuery.data?.items ?? []).map((site) => ({
    value: site.id,
    label: site.status === 'enabled' ? site.primary_domain : `${site.primary_domain}（${site.status === 'disabled' ? '已禁用' : site.status}）`,
  }))

  return (
    <form onSubmit={defaultSiteForm.onSubmit(saveDefaultSite)}>
      <Stack gap="md" className="advancedConfigPanel advancedConfigNarrowPanel">
        <Alert color="blue" title="默认站点">
          设置默认站点后，当请求的 Host 不匹配任何已绑定的域名时，Nginx 将使用该站点的配置进行响应。
        </Alert>
        {defaultSiteQuery.isError ? <ErrorAlert error={defaultSiteQuery.error} title="加载默认站点失败" /> : null}
        {sitesQuery.isError ? <ErrorAlert error={sitesQuery.error} title="加载站点列表失败" /> : null}
        {saveDefaultSiteMutation.isError ? <ErrorAlert error={saveDefaultSiteMutation.error} title="保存默认站点失败" /> : null}
        <Select label="默认站点" placeholder="不设置默认站点" clearable searchable data={siteOptions} disabled={defaultSiteQuery.isFetching || sitesQuery.isFetching} {...defaultSiteForm.getInputProps('site_id')} />
        <Text size="xs" c="dimmed">留空表示不设置默认站点。</Text>
        <Group justify="flex-end"><Button type="submit" loading={saveDefaultSiteMutation.isPending}>保存默认站点</Button></Group>
      </Stack>
    </form>
  )
}

function HTTPSHijackPanel({ enabled }: { enabled: boolean }) {
  const queryClient = useQueryClient()
  const hijackForm = useForm<Pick<HTTPSHijackSettings, 'enabled' | 'cert_mode'> & { return_status_code: string; custom_cert_id: string }>({
    initialValues: { enabled: false, return_status_code: '444', cert_mode: 'self_signed', custom_cert_id: '' },
    validate: {
      return_status_code: (value) => {
        const statusCode = Number(value)
        if (!/^\d+$/.test(value)) return '请输入数字状态码'
        if (statusCode < 400 || statusCode > 599) return '状态码范围为 400-599'
        return null
      },
    },
  })
  const hijackQuery = useQuery({ queryKey: ['settings', 'https-hijack'], queryFn: getHTTPSHijack, enabled })
  const certsQuery = useQuery({ queryKey: ['certificates'], queryFn: listCertificates, enabled })
  const saveHijackMutation = useMutation({ mutationFn: updateHTTPSHijack })

  useEffect(() => {
    if (!hijackQuery.data) return
    hijackForm.setValues({
      enabled: hijackQuery.data.enabled,
      return_status_code: String(hijackQuery.data.return_status_code || 444),
      cert_mode: hijackQuery.data.cert_mode || 'self_signed',
      custom_cert_id: hijackQuery.data.custom_cert_id || '',
    })
  }, [hijackQuery.data])

  async function saveHijack(values: typeof hijackForm.values) {
    await saveHijackMutation.mutateAsync({
      enabled: values.enabled,
      return_status_code: Number(values.return_status_code),
      cert_mode: values.cert_mode,
      custom_cert_id: values.cert_mode === 'custom' ? values.custom_cert_id : undefined,
    })
    notifySuccess({ message: 'HTTPS 防窜站配置已保存' })
    await queryClient.invalidateQueries({ queryKey: ['settings', 'https-hijack'] })
  }

  const certOptions = (certsQuery.data ?? []).map((cert) => ({
    value: cert.id,
    label: `${cert.name || cert.subject} (${cert.issuer})`,
  }))

  return (
    <form onSubmit={hijackForm.onSubmit(saveHijack)}>
      <Stack gap="md" className="advancedConfigPanel advancedConfigNarrowPanel">
        <Alert color="blue" title="HTTPS 防窜站">
          当用户通过 HTTPS 访问未配置 SSL 的域名时，Nginx 会返回第一个匹配 443 端口的站点证书。启用后将创建默认 443 server 拦截未匹配请求。
        </Alert>
        {hijackQuery.isError ? <ErrorAlert error={hijackQuery.error} title="加载 HTTPS 防窜站配置失败" /> : null}
        {certsQuery.isError ? <ErrorAlert error={certsQuery.error} title="加载证书夹失败" /> : null}
        {saveHijackMutation.isError ? <ErrorAlert error={saveHijackMutation.error} title="保存 HTTPS 防窜站配置失败" /> : null}
        <LoadingOverlay visible={hijackQuery.isFetching && !hijackQuery.data} />
        <Switch label="启用 HTTPS 防窜站" {...hijackForm.getInputProps('enabled', { type: 'checkbox' })} />
        {hijackForm.values.enabled ? (
          <Stack gap="md">
            <TextInput
              label="返回状态码"
              inputMode="numeric"
              description="推荐 444（直接关闭连接）或 403（禁止访问）。"
              {...hijackForm.getInputProps('return_status_code')}
              onChange={(event) => hijackForm.setFieldValue('return_status_code', event.currentTarget.value.replace(/\D/g, ''))}
            />
            <Radio.Group label="证书来源" {...hijackForm.getInputProps('cert_mode')}>
              <Group mt="xs"><Radio value="self_signed" label="自动生成自签证书" /><Radio value="custom" label="从证书夹选择" /></Group>
            </Radio.Group>
            {hijackForm.values.cert_mode === 'custom' ? <Select label="选择证书" placeholder="选择证书" searchable data={certOptions} {...hijackForm.getInputProps('custom_cert_id')} /> : null}
          </Stack>
        ) : null}
        <Group justify="flex-end"><Button type="submit" loading={saveHijackMutation.isPending}>保存 HTTPS 防窜站配置</Button></Group>
      </Stack>
    </form>
  )
}

function LogRotationPanel({ enabled }: { enabled: boolean }) {
  const logRotateForm = useForm<LogRotateSettings>({ initialValues: { enabled: false, interval: '1h', max_count: 30, max_age: '720h', min_size: '100M' } })
  const logRotateQuery = useQuery({ queryKey: ['settings', 'log-rotation'], queryFn: getLogRotation, enabled })
  const saveLogRotateMutation = useMutation({ mutationFn: updateLogRotation })

  useEffect(() => {
    if (logRotateQuery.data) logRotateForm.setValues(logRotateQuery.data)
  }, [logRotateQuery.data])

  async function saveLogRotate(values: LogRotateSettings) {
    const result = await saveLogRotateMutation.mutateAsync(values)
    logRotateForm.setValues(result)
    notifySuccess({ message: '日志切割配置已保存' })
  }

  return (
    <form onSubmit={logRotateForm.onSubmit(saveLogRotate)}>
      <Stack gap="md" className="advancedConfigPanel advancedConfigNarrowPanel">
        <Alert color="blue" title="日志切割">
          自动按时间切割 Nginx 站点日志，并按双条件策略清理旧文件：仅当切割数量超过最大保留数量且时间超过最大保留时间时才删除。
        </Alert>
        {logRotateQuery.isError ? <ErrorAlert error={logRotateQuery.error} title="加载日志切割配置失败" /> : null}
        {saveLogRotateMutation.isError ? <ErrorAlert error={saveLogRotateMutation.error} title="保存日志切割配置失败" /> : null}
        <LoadingOverlay visible={logRotateQuery.isFetching && !logRotateQuery.data} />
        <Switch label="启用日志切割" {...logRotateForm.getInputProps('enabled', { type: 'checkbox' })} />
        {logRotateForm.values.enabled ? (
          <Stack gap="md">
            <Select label="切割间隔" data={intervalOptions} {...logRotateForm.getInputProps('interval')} />
            <NumberInput label="最大保留数量" min={1} max={365} clampBehavior="strict" description="每个日志文件最多保留的切割份数。" {...logRotateForm.getInputProps('max_count')} />
            <Textarea label="最大保留时间" autosize minRows={1} maxRows={2} description="支持 h（小时）、d（天）等。例如 720h = 30 天。" {...logRotateForm.getInputProps('max_age')} />
            <Select label="最小切割大小" data={minSizeOptions} description="日志文件小于此值时跳过切割，避免低流量站点产生大量小文件。" {...logRotateForm.getInputProps('min_size')} />
          </Stack>
        ) : null}
        <Group justify="flex-end"><Button type="submit" loading={saveLogRotateMutation.isPending}>保存日志切割配置</Button></Group>
      </Stack>
    </form>
  )
}
