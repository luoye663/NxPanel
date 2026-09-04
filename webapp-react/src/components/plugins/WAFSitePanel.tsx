import { Alert, Button, Divider, Grid, Group, Modal, NumberInput, SegmentedControl, Select, Stack, Switch, Text } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconDeviceFloppy, IconShieldCheck } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { callPluginRPC, type WAFSiteConfig } from '@/api/plugins'
import { pluginQueryKeys, usePluginRPC } from '@/api/pluginHooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { SectionCard } from '@/components/common/SectionCard'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

const DEFAULT_CONFIG: WAFSiteConfig = {
  site_id: '',
  enabled: false,
  mode: 'DetectionOnly',
  paranoia_level: 1,
  inbound_threshold: 5,
  outbound_threshold: 4,
  response_status: 403,
  request_body_limit: 13 * 1024 * 1024,
  response_body_limit: 512 * 1024,
}

export function WAFSitePanel({ pluginId, siteId }: { pluginId: string; siteId: string }) {
  const queryClient = useQueryClient()
  const configQuery = usePluginRPC<WAFSiteConfig>(pluginId, 'waf.site.get', { site_id: siteId })
  const [config, setConfig] = useState<WAFSiteConfig>({ ...DEFAULT_CONFIG, site_id: siteId })
  const [enableMode, setEnableMode] = useState<'DetectionOnly' | 'On' | null>(null)
  const [enableOpened, enableHandlers] = useDisclosure(false)

  useEffect(() => {
    if (configQuery.data) setConfig(configQuery.data)
  }, [configQuery.data])

  const saveMutation = useMutation({
    mutationFn: (next: WAFSiteConfig) => callPluginRPC<WAFSiteConfig>(pluginId, { method: 'waf.site.save', payload: next, context: { site_id: siteId } }),
    onSuccess: async (saved) => {
      setConfig(saved)
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.rpc(pluginId, 'waf.site.get', { site_id: siteId }) })
      notifySuccess({ message: 'WAF 站点策略已应用，Nginx 配置测试通过' })
    },
    onError: (error) => showErrorModal(error, '保存 WAF 站点策略失败'),
  })

  function set<K extends keyof WAFSiteConfig>(key: K, value: WAFSiteConfig[K]) {
    setConfig((current) => ({ ...current, [key]: value }))
  }

  function requestToggle(enabled: boolean) {
    if (enabled && !config.enabled) {
      setEnableMode(null)
      enableHandlers.open()
    }
    else saveMutation.mutate({ ...config, enabled: false })
  }

  function confirmEnable() {
    enableHandlers.close()
    if (!enableMode) return
    saveMutation.mutate({ ...config, enabled: true, mode: enableMode })
  }

  if (configQuery.isError) return <ErrorAlert error={configQuery.error} title="加载 WAF 站点策略失败" />

  return (
    <Stack>
      <SectionCard title="Web 防火墙" description="使用 ModSecurity 与 OWASP CRS 检查当前站点请求。配置保存后会先执行 nginx -t，再原子应用。" actions={<Switch checked={config.enabled} disabled={configQuery.isLoading || saveMutation.isPending} label={config.enabled ? '已启用' : '未启用'} onChange={(event) => requestToggle(event.currentTarget.checked)} />}>
        <Stack mt="sm">
          {!config.enabled ? <Alert color="gray" title="当前站点未受 WAF 保护">首次启用时必须明确选择观察或拦截模式。建议先观察命中情况，再切换到拦截。</Alert> : null}
          {config.enabled && config.mode === 'DetectionOnly' ? <Alert color="blue" title="观察模式">攻击行为会被记录但不会阻断。确认误报可控后，再启用拦截模式。</Alert> : null}
          <Grid>
            <Grid.Col span={{ base: 12, sm: 6 }}>
              <Select label="运行模式" value={config.mode} data={[{ value: 'DetectionOnly', label: '观察（DetectionOnly）' }, { value: 'On', label: '拦截（On）' }]} allowDeselect={false} onChange={(value) => value && set('mode', value as WAFSiteConfig['mode'])} />
            </Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}>
              <Select label="规则敏感度" description="更高等级覆盖更多攻击变体，也更可能产生误报。" value={String(config.paranoia_level)} data={[1, 2, 3, 4].map((value) => ({ value: String(value), label: `PL${value}` }))} allowDeselect={false} onChange={(value) => value && set('paranoia_level', Number(value) as WAFSiteConfig['paranoia_level'])} />
            </Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}><NumberInput label="入站异常阈值" min={1} max={100} value={config.inbound_threshold} onChange={(value) => set('inbound_threshold', Number(value) || 5)} /></Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}><NumberInput label="出站异常阈值" min={1} max={100} value={config.outbound_threshold} onChange={(value) => set('outbound_threshold', Number(value) || 4)} /></Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}><NumberInput label="拦截响应状态码" min={400} max={599} value={config.response_status} onChange={(value) => set('response_status', Number(value) || 403)} /></Grid.Col>
          </Grid>
          <Divider label="内容检查上限" labelPosition="left" />
          <Grid>
            <Grid.Col span={{ base: 12, sm: 6 }}><NumberInput label="请求体上限（MiB）" min={1} max={128} decimalScale={1} value={config.request_body_limit / 1024 / 1024} onChange={(value) => set('request_body_limit', Math.round((Number(value) || 13) * 1024 * 1024))} /></Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}><NumberInput label="响应体上限（KiB）" min={64} max={8192} value={config.response_body_limit / 1024} onChange={(value) => set('response_body_limit', Math.round((Number(value) || 512) * 1024))} /></Grid.Col>
          </Grid>
          <Alert color="orange" title="审计日志可能包含敏感数据">命中事务可能保存请求头、Cookie、表单或部分响应内容。完整内容需要再次验证身份后才能查看或下载。</Alert>
          <Group justify="flex-end"><Button leftSection={<IconDeviceFloppy size={16} />} loading={saveMutation.isPending} disabled={configQuery.isLoading} onClick={() => saveMutation.mutate(config)}>保存并应用</Button></Group>
        </Stack>
      </SectionCard>
      <Modal opened={enableOpened} onClose={enableHandlers.close} title="首次启用 Web 防火墙" size="md" closeOnClickOutside={false}>
        <Stack>
          <Text>请选择首次启用后的处理方式。此选择会立即影响当前站点流量。</Text>
          <SegmentedControl fullWidth value={enableMode || ''} onChange={(value) => setEnableMode(value as WAFSiteConfig['mode'])} data={[{ value: 'DetectionOnly', label: '仅观察' }, { value: 'On', label: '立即拦截' }]} />
          {!enableMode ? <Alert color="blue">请选择首次启用模式后继续。</Alert> : enableMode === 'On' ? <Alert color="orange" title="立即拦截可能影响正常请求">建议先使用观察模式收集命中并处理误报。</Alert> : <Alert color="blue">观察模式只记录命中，不会阻断请求。</Alert>}
          <Group justify="flex-end"><Button variant="default" onClick={enableHandlers.close}>取消</Button><Button color={enableMode === 'On' ? 'orange' : 'blue'} leftSection={<IconShieldCheck size={16} />} loading={saveMutation.isPending} disabled={!enableMode} onClick={confirmEnable}>确认启用</Button></Group>
        </Stack>
      </Modal>
    </Stack>
  )
}
