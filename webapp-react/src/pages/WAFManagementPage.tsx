import { Alert, Badge, Button, Grid, Group, Loader, NumberFormatter, Paper, ScrollArea, Table, Text, ThemeIcon, Title } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconBan, IconEye, IconRefresh, IconServer, IconShieldCheck } from '@tabler/icons-react'
import { callPluginRPC, type WAFEvent, type WAFOverview } from '@/api/plugins'
import { pluginQueryKeys, usePluginRPC } from '@/api/pluginHooks'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageHeader } from '@/components/common/PageHeader'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess } from '@/utils/notify'

function Metric({ label, value, icon, color }: { label: string; value: number | string; icon: React.ReactNode; color: string }) {
  return <Paper withBorder p="md" radius="md"><Group wrap="nowrap"><ThemeIcon variant="light" color={color} size={38}>{icon}</ThemeIcon><div><Text c="dimmed" size="xs">{label}</Text><Title order={3} className="pluginMetricValue">{typeof value === 'number' ? <NumberFormatter value={value} thousandSeparator /> : value}</Title></div></Group></Paper>
}

function formatTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

export function WAFManagementPage({ pluginId }: { pluginId: string }) {
  const queryClient = useQueryClient()
  const overviewQuery = usePluginRPC<WAFOverview>(pluginId, 'waf.overview')
  const eventsQuery = usePluginRPC<{ items: WAFEvent[] }>(pluginId, 'waf.events.list', undefined, true)
  const ruleUpdateMutation = useMutation({
    mutationFn: () => callPluginRPC(pluginId, { method: 'waf.rules.check' }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: pluginQueryKeys.rpc(pluginId, 'waf.overview') })
      notifySuccess({ message: '规则更新检查已提交' })
    },
    onError: (error) => showErrorModal(error, '检查 WAF 规则更新失败'),
  })
  const overview = overviewQuery.data
  const events = eventsQuery.data?.items || []

  return (
    <PageShell>
      <PageHeader title="Web 防火墙" subtitle="ModSecurity v3 与 OWASP CRS 的运行状态、攻击命中和规则版本。站点策略在各网站详情中配置。" actions={<Button variant="default" leftSection={<IconRefresh size={16} />} loading={ruleUpdateMutation.isPending} onClick={() => ruleUpdateMutation.mutate()}>检查规则更新</Button>} />
      {overviewQuery.isLoading ? <Group justify="center" py="xl"><Loader /></Group> : null}
      {overviewQuery.isError ? <ErrorAlert error={overviewQuery.error} title="加载 WAF 状态失败" /> : null}
      {overview?.last_error ? <Alert color="red" title="WAF 运行异常">{overview.last_error}</Alert> : null}
      {overview ? <>
        <Grid>
          <Grid.Col span={{ base: 12, xs: 6, lg: 3 }}><Metric label="近 24 小时命中" value={overview.events_24h || 0} color="blue" icon={<IconShieldCheck size={21} />} /></Grid.Col>
          <Grid.Col span={{ base: 12, xs: 6, lg: 3 }}><Metric label="已拦截" value={overview.blocked_24h || 0} color="red" icon={<IconBan size={21} />} /></Grid.Col>
          <Grid.Col span={{ base: 12, xs: 6, lg: 3 }}><Metric label="仅观察" value={overview.observed_24h || 0} color="yellow" icon={<IconEye size={21} />} /></Grid.Col>
          <Grid.Col span={{ base: 12, xs: 6, lg: 3 }}><Metric label="受保护站点" value={overview.protected_sites || 0} color="green" icon={<IconServer size={21} />} /></Grid.Col>
        </Grid>
        <SectionCard title="引擎与规则" description="运行时指纹必须与 Provider 构建完全匹配；不兼容的模块不会被加载。">
          <Grid mt="xs">
            <Grid.Col span={{ base: 12, md: 4 }}><Text c="dimmed" size="xs">引擎 / Connector</Text><Text fw={600}>{overview.engine_version || '-'} / {overview.connector_version || '-'}</Text></Grid.Col>
            <Grid.Col span={{ base: 12, md: 4 }}><Text c="dimmed" size="xs">OWASP CRS</Text><Group gap="xs"><Text fw={600}>{overview.rules_version || '-'}</Text>{overview.rules_channel ? <Badge variant="light">{overview.rules_channel}</Badge> : null}</Group></Grid.Col>
            <Grid.Col span={{ base: 12, md: 4 }}><Text c="dimmed" size="xs">上次规则检查</Text><Text>{formatTime(overview.last_rule_check_at)}</Text></Grid.Col>
            <Grid.Col span={12}><Text c="dimmed" size="xs">运行时指纹</Text><Text ff="monospace" size="xs" className="pluginFingerprint">{overview.runtime_fingerprint || '-'}</Text></Grid.Col>
          </Grid>
        </SectionCard>
      </> : null}
      <SectionCard title="最近命中" description="这里只显示脱敏后的结构化元数据；查看完整事务需要重新验证身份。">
        {eventsQuery.isError ? <ErrorAlert error={eventsQuery.error} title="加载 WAF 命中失败" /> : null}
        {eventsQuery.isLoading ? <Group justify="center" py="lg"><Loader size="sm" /></Group> : null}
        {!eventsQuery.isLoading && events.length === 0 ? <Alert color="gray">暂无 WAF 命中记录。</Alert> : null}
        {events.length ? <ScrollArea><Table striped withTableBorder miw={860}><Table.Thead><Table.Tr><Table.Th>时间</Table.Th><Table.Th>站点</Table.Th><Table.Th>来源 IP</Table.Th><Table.Th>请求</Table.Th><Table.Th>规则</Table.Th><Table.Th>分数</Table.Th><Table.Th>动作</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{events.map((event) => <Table.Tr key={event.id}><Table.Td>{formatTime(event.occurred_at)}</Table.Td><Table.Td>{event.site_name || event.site_id || '-'}</Table.Td><Table.Td ff="monospace">{event.client_ip}</Table.Td><Table.Td><Text lineClamp={1}>{event.method} {event.uri}</Text></Table.Td><Table.Td>{event.rule_id || event.category || '-'}</Table.Td><Table.Td>{event.score ?? '-'}</Table.Td><Table.Td><Badge color={event.action === 'blocked' ? 'red' : 'yellow'} variant="light">{event.action === 'blocked' ? '已拦截' : '已观察'}</Badge></Table.Td></Table.Tr>)}</Table.Tbody></Table></ScrollArea> : null}
      </SectionCard>
    </PageShell>
  )
}
