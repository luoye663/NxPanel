import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import {
  Alert,
  Badge,
  Box,
  Button,
  Checkbox,
  Group,
  Modal,
  Progress,
  RingProgress,
  ScrollArea,
  SegmentedControl,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Tooltip,
} from '@mantine/core'
import { IconActivity, IconCpu, IconDatabase, IconInfoCircle, IconPlayerPause, IconPlayerPlay, IconRefresh, IconServer } from '@tabler/icons-react'
import { RealtimeLineChart, type RealtimePoint } from '@/components/charts/RealtimeLineChart'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { useSystemOverview } from '@/api/hooks'
import { systemMetricsStreamURL } from '@/api/system'
import type { SystemMetricsSnapshot } from '@/api/types'
import { useEventSource } from '@/hooks/useEventSource'
import { notifyError, notifySuccess } from '@/utils/notify'

type DetailType = 'load' | 'cpu' | 'memory' | 'disk'
type UnitMode = 'auto' | 'B' | 'KB' | 'MB' | 'GB'
type DiskMountView = 'counted' | 'all'

const HISTORY_LIMIT = 60
const CHART_WINDOW_SECONDS = 180
const STALE_SAMPLE_TOLERANCE_MS = 1000

export function DashboardPage() {
  const overviewQuery = useSystemOverview()
  const [metrics, setMetrics] = useState<SystemMetricsSnapshot | null>(null)
  const [detailType, setDetailType] = useState<DetailType | null>(null)
  const [detailPaused, setDetailPaused] = useState(false)
  const [pausedMetrics, setPausedMetrics] = useState<SystemMetricsSnapshot | null>(null)
  const [selectedMounts, setSelectedMounts] = useState<string[]>([])
  const [initializedDiskMountKey, setInitializedDiskMountKey] = useState('')
  const [diskMountView, setDiskMountView] = useState<DiskMountView>('counted')
  const [selectedNetwork, setSelectedNetwork] = useState('全部')
  const [selectedDiskIO, setSelectedDiskIO] = useState('全部')
  const [networkUnit, setNetworkUnit] = useState<UnitMode>('auto')
  const [diskIOUnit, setDiskIOUnit] = useState<UnitMode>('auto')
  const [networkHistory, setNetworkHistory] = useState<RealtimePoint[]>([])
  const [diskIOHistory, setDiskIOHistory] = useState<RealtimePoint[]>([])
  const [chartNow, setChartNow] = useState(() => Date.now())
  const chartStartedAtRef = useRef(Date.now())
  const metricsScope = detailType ? `summary,network,disk_io,${detailType}_detail` : 'summary,network,disk_io'

  const { status: sseStatus } = useEventSource(systemMetricsStreamURL(metricsScope), {
    onMessage: (event) => {
      const next = JSON.parse(event.data) as SystemMetricsSnapshot
      const sampleTs = normalizeMetricTimestamp(next.timestamp)
      setMetrics(next)
      if (sampleTs < chartStartedAtRef.current - STALE_SAMPLE_TOLERANCE_MS) {
        return
      }
      setChartNow(Date.now())
      // Dashboard 长时间打开时只保留固定历史点，避免内存和 SVG DOM 数据无限增长。
      setNetworkHistory((current) => nextHistory(current, sampleTs, next.network.total.rx_bytes_per_sec, next.network.total.tx_bytes_per_sec))
      setDiskIOHistory((current) => nextHistory(current, sampleTs, next.disk_io.total.read_bytes_per_sec, next.disk_io.total.write_bytes_per_sec))
    },
  })
  const diskMountKey = (metrics?.disks || []).map((disk) => `${disk.usage_key}:${disk.mountpoint}:${disk.counted}`).join('|')

  useEffect(() => {
    if (metrics?.disks.length && diskMountKey !== initializedDiskMountKey) {
      const countedMounts = metrics.disks.filter(isCountedDisk).map((disk) => disk.mountpoint)
      setSelectedMounts(countedMounts.length > 0 ? countedMounts : metrics.disks.map((disk) => disk.mountpoint))
      setInitializedDiskMountKey(diskMountKey)
    }
  }, [diskMountKey, initializedDiskMountKey, metrics?.disks])

  useEffect(() => {
    setDetailPaused(false)
    setPausedMetrics(null)
  }, [detailType])

  const overview = overviewQuery.data
  const countedDisks = (metrics?.disks || []).filter(isCountedDisk)
  const diskAggregate = aggregateDisks(countedDisks.length > 0 ? countedDisks : metrics?.disks || [])
  const selectedDiskAggregate = aggregateDisks((metrics?.disks || []).filter((disk) => selectedMounts.includes(disk.mountpoint) && isCountedDisk(disk)))
  const diskSummary = selectedMounts.length > 0 ? selectedDiskAggregate : { total: 0, used: 0, percent: 0 }
  const networkItems = [metrics?.network.total, ...(metrics?.network.interfaces || [])].filter(Boolean) as SystemMetricsSnapshot['network']['interfaces']
  const diskIOItems = [metrics?.disk_io.total, ...(metrics?.disk_io.devices || [])].filter(Boolean) as SystemMetricsSnapshot['disk_io']['devices']
  const currentNetwork = networkItems.find((item) => item.name === selectedNetwork) || metrics?.network.total
  const currentDiskIO = diskIOItems.find((item) => item.name === selectedDiskIO) || metrics?.disk_io.total
  const detailCanPause = detailType === 'load' || detailType === 'cpu' || detailType === 'memory'
  const detailMetrics = detailCanPause && detailPaused && pausedMetrics ? pausedMetrics : metrics

  function toggleDetailPaused() {
    if (detailPaused) {
      setDetailPaused(false)
      setPausedMetrics(null)
      return
    }
    setPausedMetrics(metrics)
    setDetailPaused(true)
  }

  function openDetail(type: DetailType) {
    setDetailPaused(false)
    setPausedMetrics(null)
    setDetailType(type)
  }

  return (
    <PageShell>
      {overviewQuery.error ? <ErrorAlert error={overviewQuery.error} /> : null}

      <SectionCard className="dashboardStatusCard">
        <Group justify="space-between" align="center" gap="md">
          <Group gap="sm">
            <span className={`statusDot ${overview?.agent.available ? 'online' : 'offline'}`} />
            <Text fw={700}>Agent {boolLabel(overview?.agent.available)}</Text>
            <Text size="sm" c="dimmed">{overview?.agent.version || '未知版本'}</Text>
            <Badge variant="light" color={statusColor(sseStatus)}>SSE {sseStatus}</Badge>
          </Group>
          <Group gap="md" className="dashboardStatusMeta">
            <Text size="sm" c="dimmed">{metrics?.system.pretty_name || metrics?.system.name || '-'}</Text>
            <Text size="sm" c="dimmed">运行 {formatUptime(metrics?.system.uptime_seconds)}</Text>
            <Text size="sm" c="dimmed">{overview?.agent.socket || '-'}</Text>
            <Button leftSection={<IconRefresh size={16} />} variant="light" onClick={() => overviewQuery.refetch()}>刷新</Button>
          </Group>
        </Group>
      </SectionCard>

      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="md">
        <MetricCard title="负载状态" percent={metrics?.load.percent || 0} value={<LoadValue metrics={metrics} />} icon={<IconActivity size={18} />} onClick={() => openDetail('load')} />
        <MetricCard title="CPU 使用率" percent={metrics?.cpu.percent || 0} value={cpuValue(metrics)} icon={<IconCpu size={18} />} onClick={() => openDetail('cpu')} />
        <MetricCard title="内存使用率" percent={metrics?.memory.percent || 0} value={metrics ? `${formatBytes(metrics.memory.used_bytes)} / ${formatBytes(metrics.memory.total_bytes)}` : '-'} icon={<IconServer size={18} />} onClick={() => openDetail('memory')} />
        <MetricCard title="硬盘占用" percent={diskAggregate.percent} value={`${formatBytes(diskAggregate.used)} / ${formatBytes(diskAggregate.total)}`} icon={<IconDatabase size={18} />} onClick={() => openDetail('disk')} />
      </SimpleGrid>

      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
        <SectionCard
          title="当前网卡流量"
          actions={<ChartActions items={networkItems.map((item) => item.name)} value={selectedNetwork} unit={networkUnit} onValueChange={setSelectedNetwork} onUnitChange={setNetworkUnit} />}
        >
          <StatsRow items={[`下行 ${formatRate(currentNetwork?.rx_bytes_per_sec, networkUnit)}`, `上行 ${formatRate(currentNetwork?.tx_bytes_per_sec, networkUnit)}`, `总接收 ${formatBytes(currentNetwork?.rx_bytes)}`, `总发送 ${formatBytes(currentNetwork?.tx_bytes)}`]} />
          <RealtimeLineChart points={networkHistory} labelA="下载" labelB="上传" unit={chartUnit(networkUnit)} windowSeconds={CHART_WINDOW_SECONDS} maxPoints={HISTORY_LIMIT} now={chartNow} />
        </SectionCard>
        <SectionCard
          title="磁盘 IO"
          actions={<ChartActions items={diskIOItems.map((item) => item.name)} value={selectedDiskIO} unit={diskIOUnit} onValueChange={setSelectedDiskIO} onUnitChange={setDiskIOUnit} />}
        >
          <StatsRow items={[`读取 ${formatRate(currentDiskIO?.read_bytes_per_sec, diskIOUnit)}`, `写入 ${formatRate(currentDiskIO?.write_bytes_per_sec, diskIOUnit)}`, `每秒读写 ${(currentDiskIO?.iops || 0).toFixed(1)} ops/s`, `IO 延迟 ${(currentDiskIO?.latency_ms || 0).toFixed(2)} ms`]} />
          <RealtimeLineChart points={diskIOHistory} labelA="读" labelB="写" unit={chartUnit(diskIOUnit)} windowSeconds={CHART_WINDOW_SECONDS} maxPoints={HISTORY_LIMIT} now={chartNow} />
        </SectionCard>
      </SimpleGrid>

      <Modal opened={Boolean(detailType)} onClose={() => setDetailType(null)} title={detailTitle(detailType)} size={detailType === 'disk' ? 1120 : 'xl'}>
        {detailMetrics ? (
          <Stack gap="md">
            {detailCanPause ? <DetailRefreshControl paused={detailPaused} timestamp={detailMetrics.timestamp} onToggle={toggleDetailPaused} /> : null}
            <DetailContent metrics={detailMetrics} detailType={detailType} selectedMounts={selectedMounts} diskSummary={diskSummary} diskMountView={diskMountView} onDiskMountViewChange={setDiskMountView} onMountsChange={setSelectedMounts} />
          </Stack>
        ) : <Alert color="blue" title="等待实时指标">SSE 连接建立后会显示详情数据。</Alert>}
      </Modal>
    </PageShell>
  )
}

function DetailRefreshControl({ paused, timestamp, onToggle }: { paused: boolean; timestamp: number; onToggle: () => void }) {
  return (
    <Group justify="space-between" gap="md" className="detailRefreshBar">
      <Text size="xs" c="dimmed">{paused ? '已暂停刷新' : '实时刷新中'}，快照时间 {formatDateTime(timestamp)}</Text>
      <Button variant={paused ? 'filled' : 'light'} leftSection={paused ? <IconPlayerPlay size={16} /> : <IconPlayerPause size={16} />} onClick={onToggle}>{paused ? '恢复刷新' : '暂停刷新'}</Button>
    </Group>
  )
}

function MetricCard({ title, percent, value, icon, onClick }: { title: string; percent: number; value: ReactNode; icon: ReactNode; onClick: () => void }) {
  return (
    <SectionCard className="metricCard" onClick={onClick} role="button" tabIndex={0} onKeyDown={(event) => { if (event.key === 'Enter') onClick() }}>
      <Group justify="space-between" mb="sm">
        <Group gap="xs"><span className="metricIcon">{icon}</span><Text fw={700}>{title}</Text></Group>
        <Tooltip label="点击查看详情"><IconInfoCircle size={16} color="var(--mantine-color-gray-5)" /></Tooltip>
      </Group>
      <Stack align="center" gap={8}>
        <RingProgress size={118} thickness={10} roundCaps sections={[{ value: clampPercent(percent), color: progressColor(percent) }]} label={<Text ta="center" fw={800}>{percent.toFixed(1)}%</Text>} />
        <Box className="metricValue">{value}</Box>
      </Stack>
    </SectionCard>
  )
}

function LoadValue({ metrics }: { metrics: SystemMetricsSnapshot | null }) {
  if (!metrics) return <Text size="sm" c="dimmed">-</Text>
  const cores = metrics.cpu.info.logical_cores || 1
  return (
    <Group gap={4} justify="center" className="loadValue">
      <Text size="sm" fw={700} c={loadColor(metrics.load.load1, cores)}>{metrics.load.load1.toFixed(2)}</Text>
      <Text size="xs" c="dimmed">/</Text>
      <Text size="sm" fw={700} c={loadColor(metrics.load.load5, cores)}>{metrics.load.load5.toFixed(2)}</Text>
      <Text size="xs" c="dimmed">/</Text>
      <Text size="sm" fw={700} c={loadColor(metrics.load.load15, cores)}>{metrics.load.load15.toFixed(2)}</Text>
      <Tooltip multiline label={<LoadLegend cores={cores} />}>
        <IconInfoCircle size={15} color="var(--mantine-color-gray-5)" />
      </Tooltip>
    </Group>
  )
}

function LoadLegend({ cores }: { cores: number }) {
  return (
    <Stack gap={3} w={320}>
      <Text size="xs">绿色：负载 &lt; 核心数 × 0.7（正常）</Text>
      <Text size="xs">黄色：核心数 × 0.7 ≤ 负载 &lt; 核心数（偏高）</Text>
      <Text size="xs">红色：负载 ≥ 核心数（过载）</Text>
      <Text size="xs">当前逻辑核心数：{cores}；三个值分别为 1 / 5 / 15 分钟平均负载。</Text>
    </Stack>
  )
}

function ChartActions({ items, value, unit, onValueChange, onUnitChange }: { items: string[]; value: string; unit: UnitMode; onValueChange: (value: string) => void; onUnitChange: (value: UnitMode) => void }) {
  return (
    <Group gap="xs" className="chartActions">
      <Select data={items} value={value} onChange={(next) => onValueChange(next || '全部')} w={130} size="xs" />
      <Select data={[{ label: '自动', value: 'auto' }, { label: 'KB/s', value: 'KB' }, { label: 'MB/s', value: 'MB' }, { label: 'GB/s', value: 'GB' }]} value={unit} onChange={(next) => onUnitChange((next || 'auto') as UnitMode)} w={96} size="xs" />
    </Group>
  )
}

function StatsRow({ items }: { items: string[] }) {
  return <Group gap="md" mb="sm">{items.map((item) => <Text key={item} size="xs" c="dimmed">{item}</Text>)}</Group>
}

function DetailContent({ metrics, detailType, selectedMounts, diskSummary, diskMountView, onDiskMountViewChange, onMountsChange }: { metrics: SystemMetricsSnapshot; detailType: DetailType | null; selectedMounts: string[]; diskSummary: { total: number; used: number; percent: number }; diskMountView: DiskMountView; onDiskMountViewChange: (value: DiskMountView) => void; onMountsChange: (value: string[]) => void }) {
  const topCPU = metrics.top?.cpu ?? []
  const topMemory = metrics.top?.memory ?? []
  const cpuCores = metrics.cpu.cores ?? []

  if (detailType === 'load') {
    return <LoadDetail metrics={metrics} topCPU={topCPU} />
  }
  if (detailType === 'cpu') {
    return <CpuDetail metrics={metrics} cpuCores={cpuCores} topCPU={topCPU} />
  }
  if (detailType === 'memory') {
    return <MemoryDetail metrics={metrics} topMemory={topMemory} />
  }
  if (detailType === 'disk') {
    return <DiskDetail metrics={metrics} selectedMounts={selectedMounts} diskSummary={diskSummary} mountView={diskMountView} onMountViewChange={onDiskMountViewChange} onMountsChange={onMountsChange} />
  }
  return null
}

function LoadDetail({ metrics, topCPU }: { metrics: SystemMetricsSnapshot; topCPU: SystemMetricsSnapshot['top']['cpu'] }) {
  const cores = metrics.cpu.info.logical_cores || 1
  const loadItems = [
    { label: '1 分钟', value: metrics.load.load1 },
    { label: '5 分钟', value: metrics.load.load5 },
    { label: '15 分钟', value: metrics.load.load15 },
  ]

  return (
    <Stack gap="md">
      <Box className="detailHero loadHero">
        <Group justify="space-between" align="flex-start" gap="md">
          <Box>
            <Text size="xs" tt="uppercase" fw={800} c="blue.7">Load Average</Text>
            <Text fw={800} fz={28}>{metrics.load.percent.toFixed(1)}%</Text>
            <Text size="sm" c="dimmed">基于 {cores} 个逻辑核心评估当前负载压力</Text>
          </Box>
          <Badge size="lg" variant="light" color={progressColor(metrics.load.percent)}>{loadStatus(metrics.load.percent)}</Badge>
        </Group>
        <Progress mt="md" value={clampPercent(metrics.load.percent)} color={progressColor(metrics.load.percent)} radius="xl" size="lg" />
      </Box>
      <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="md">
        {loadItems.map((item) => <LoadStatCard key={item.label} label={item.label} value={item.value} cores={cores} />)}
      </SimpleGrid>
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
        <InfoItem label="活动进程" value={`${metrics.load.running_processes} 个`} />
        <InfoItem label="总进程数" value={`${metrics.load.total_processes} 个`} />
      </SimpleGrid>
      <ProcessTable rows={topCPU} mode="cpu" />
    </Stack>
  )
}

function LoadStatCard({ label, value, cores }: { label: string; value: number; cores: number }) {
  const percent = cores > 0 ? (value / cores) * 100 : 0

  return (
    <Box className={`detailStatCard ${loadColor(value, cores)}`}>
      <Group justify="space-between" mb={4} gap={6}>
        <Text size="xs" fw={700}>{label}</Text>
        <Badge size="xs" variant="dot" color={loadColor(value, cores)}>{loadStatus(percent)}</Badge>
      </Group>
      <Text fw={800} fz={18}>{value.toFixed(2)}</Text>
      <Progress value={clampPercent(percent)} color={loadColor(value, cores)} radius="xl" size="xs" mt={4} />
    </Box>
  )
}

function MemoryDetail({ metrics, topMemory }: { metrics: SystemMetricsSnapshot; topMemory: SystemMetricsSnapshot['top']['memory'] }) {
  const cacheBytes = metrics.memory.buffers_bytes + metrics.memory.cached_bytes

  return (
    <Stack gap="md">
      <Box className="detailHero memoryHero">
        <Group justify="space-between" align="center" gap="md">
          <Box>
            <Text size="xs" tt="uppercase" fw={800} c="teal.7">Memory Usage</Text>
            <Text fw={800} fz={28}>{metrics.memory.percent.toFixed(1)}%</Text>
            <Text size="sm" c="dimmed">已用 {formatBytes(metrics.memory.used_bytes)} / 总计 {formatBytes(metrics.memory.total_bytes)}</Text>
          </Box>
          <RingProgress size={116} thickness={12} roundCaps sections={[{ value: clampPercent(metrics.memory.percent), color: progressColor(metrics.memory.percent) }]} label={<Text ta="center" fw={800}>{metrics.memory.percent.toFixed(0)}%</Text>} />
        </Group>
        <Progress mt="md" value={clampPercent(metrics.memory.percent)} color={progressColor(metrics.memory.percent)} radius="xl" size="lg" />
      </Box>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
        <InfoItem label="空闲内存" value={formatBytes(metrics.memory.free_bytes)} />
        <InfoItem label="已用" value={formatBytes(metrics.memory.used_bytes)} />
        <InfoItem label="总内存" value={formatBytes(metrics.memory.total_bytes)} />
        <InfoItem label="可分配内存" value={formatBytes(metrics.memory.available_bytes)} />
        <InfoItem label="共享" value={formatBytes(metrics.memory.shared_bytes)} />
        <InfoItem label="buff/cache" value={formatBytes(cacheBytes)} />
      </SimpleGrid>
      <ProcessTable rows={topMemory} mode="memory" />
    </Stack>
  )
}

function ProcessTable({ rows, mode }: { rows: SystemMetricsSnapshot['top']['cpu']; mode: 'cpu' | 'memory' }) {
  const safeRows = rows ?? []
  if (safeRows.length === 0) {
    return <Alert color="gray" title="暂无进程详情">详情 scope 刚切换时可能需要等待下一次 SSE 数据。</Alert>
  }
  return <ScrollArea><Table miw={mode === 'cpu' ? 680 : 820} className="processTable"><Table.Thead><Table.Tr><Table.Th>PID</Table.Th><Table.Th>进程</Table.Th><Table.Th>{mode === 'cpu' ? 'CPU' : '内存'}</Table.Th>{mode === 'memory' ? <Table.Th>RSS</Table.Th> : null}<Table.Th>命令</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{safeRows.map((row) => <Table.Tr key={`${row.pid}-${row.name}`}><Table.Td>{row.pid}</Table.Td><Table.Td>{row.name}</Table.Td><Table.Td>{mode === 'cpu' ? `${row.cpu_percent.toFixed(1)}%` : `${row.mem_percent.toFixed(1)}%`}</Table.Td>{mode === 'memory' ? <Table.Td className="nowrapCell">{formatBytes(row.rss_bytes)}</Table.Td> : null}<Table.Td className="processCommandCell" title="双击复制完整命令" onDoubleClick={(event) => copyCommand(event, row.command)}>{row.command}</Table.Td></Table.Tr>)}</Table.Tbody></Table></ScrollArea>
}

function CpuDetail({ metrics, cpuCores, topCPU }: { metrics: SystemMetricsSnapshot; cpuCores: SystemMetricsSnapshot['cpu']['cores']; topCPU: SystemMetricsSnapshot['top']['cpu'] }) {
  return (
    <Stack gap="md">
      <Box>
        <Text fw={700}>{metrics.cpu.info.model || '未知 CPU'} × {metrics.cpu.info.physical_cpus || 1}</Text>
        <Text size="sm" c="dimmed">{metrics.cpu.info.physical_cpus} 个物理 CPU，{metrics.cpu.info.physical_cores} 个物理核心，{metrics.cpu.info.logical_cores} 个逻辑核心</Text>
      </Box>
      <Box className="cpuDetailGrid">
        <Box className="cpuDetailPanel">
          <Text fw={700} mb="sm">核心使用率</Text>
          <ScrollArea className="cpuCoreScroll" h={360} offsetScrollbars>
            <SimpleGrid cols={2} spacing={6}>
              {cpuCores.map((core) => <Box key={core.name} className="coreItem cpuCoreItem"><Text size="xs">核心 {Number(core.name) + 1}</Text><Text size="xs" fw={700}>{core.percent.toFixed(1)}%</Text></Box>)}
            </SimpleGrid>
          </ScrollArea>
        </Box>
        <Box className="cpuDetailPanel">
          <Text fw={700} mb="sm">CPU 占用率 top5 的进程信息</Text>
          <ProcessTable rows={topCPU} mode="cpu" />
        </Box>
      </Box>
    </Stack>
  )
}

function DiskDetail({ metrics, selectedMounts, diskSummary, mountView, onMountViewChange, onMountsChange }: { metrics: SystemMetricsSnapshot; selectedMounts: string[]; diskSummary: { total: number; used: number; percent: number }; mountView: DiskMountView; onMountViewChange: (value: DiskMountView) => void; onMountsChange: (value: string[]) => void }) {
  const selectedSet = new Set(selectedMounts)
  const selectedCountedDisks = metrics.disks.filter((disk) => isCountedDisk(disk) && selectedSet.has(disk.mountpoint))
  const duplicateDisks = mountView === 'all' ? metrics.disks.filter((disk) => !isCountedDisk(disk)) : []
  const displayedDisks = [...selectedCountedDisks, ...duplicateDisks]
  const mountOptions = mountView === 'all' ? metrics.disks : metrics.disks.filter(isCountedDisk)

  return (
    <Box className="diskDetailGrid">
      <Box className="diskMountPanel">
        <Text fw={700} mb={8}>统计挂载点</Text>
        <Text size="xs" c="dimmed" mb="sm">只勾选参与汇总的挂载点，重复挂载点会保留展示但不计入容量。</Text>
        <Checkbox.Group value={selectedMounts} onChange={onMountsChange}>
          <SimpleGrid cols={{ base: 1, sm: 2, md: 1 }} spacing={8}>
            {mountOptions.map((disk) => <Checkbox key={`${disk.device}-${disk.mountpoint}-check`} value={disk.mountpoint} disabled={!isCountedDisk(disk)} label={<DiskMountLabel disk={disk} />} />)}
          </SimpleGrid>
        </Checkbox.Group>
      </Box>
      <Stack gap="md" className="diskDetailMain">
        <Group justify="space-between" align="center" gap="md">
          <Alert color="blue" className="diskSummaryAlert">当前选择加权使用率：{diskSummary.percent.toFixed(1)}%，已用 {formatBytes(diskSummary.used)} / {formatBytes(diskSummary.total)}</Alert>
          <SegmentedControl value={mountView} onChange={(value) => onMountViewChange(value as DiskMountView)} data={[{ label: '仅统计项', value: 'counted' }, { label: '全部挂载点', value: 'all' }]} />
        </Group>
        {displayedDisks.length === 0 ? <Alert color="gray" title="未选择统计项">请在左侧勾选参与汇总的挂载点，或切换到全部挂载点查看重复挂载。</Alert> : (
          <ScrollArea className="diskTableScroll" offsetScrollbars>
            <Table miw={960} className="diskDetailTable">
              <Table.Thead><Table.Tr><Table.Th className="diskStatusCell">状态</Table.Th><Table.Th>挂载点</Table.Th><Table.Th>文件系统</Table.Th><Table.Th>类型</Table.Th><Table.Th>容量</Table.Th><Table.Th>Inode</Table.Th></Table.Tr></Table.Thead>
              <Table.Tbody>{displayedDisks.map((disk) => <Table.Tr key={`${disk.device}-${disk.mountpoint}`}><Table.Td className="diskStatusCell"><DiskCountBadge disk={disk} /></Table.Td><Table.Td>{disk.mountpoint}</Table.Td><Table.Td>{disk.device}</Table.Td><Table.Td>{disk.fs_type}</Table.Td><Table.Td><DiskCapacity disk={disk} /></Table.Td><Table.Td>{disk.inodes.used} / {disk.inodes.total} ({disk.inodes.percent.toFixed(2)}%)</Table.Td></Table.Tr>)}</Table.Tbody>
            </Table>
          </ScrollArea>
        )}
      </Stack>
    </Box>
  )
}

function DiskMountLabel({ disk }: { disk: SystemMetricsSnapshot['disks'][number] }) {
  const text = `${disk.mountpoint} (${disk.device})`
  return <Group gap={6} wrap="nowrap" className="diskMountLabel"><Text size="sm" title={text} className="diskMountText">{text}</Text>{isCountedDisk(disk) ? null : <Tooltip label="不参与汇总"><Badge size="xs" variant="light" color="gray" className="diskMountBadge">不计入</Badge></Tooltip>}</Group>
}

function DiskCountBadge({ disk }: { disk: SystemMetricsSnapshot['disks'][number] }) {
  if (isCountedDisk(disk)) {
    return <Badge size="xs" variant="light" color="green">参与汇总</Badge>
  }
  const label = disk.duplicate_of ? `同 ${disk.duplicate_of}` : '重复挂载'
  return <Tooltip label={label}><Badge size="xs" variant="light" color="gray">不参与汇总</Badge></Tooltip>
}

function DiskCapacity({ disk }: { disk: SystemMetricsSnapshot['disks'][number] }) {
  return (
    <Box className="diskCapacity">
      <span>共</span><strong className="diskTotal diskCapacityValue">{formatBytes(disk.total_bytes)}</strong>
      <span>可用</span><strong className="diskAvail diskCapacityValue">{formatBytes(disk.avail_bytes)}</strong>
      <span>已用</span><strong className="diskUsed diskCapacityValue">{formatBytes(disk.used_bytes)}</strong>
    </Box>
  )
}

function InfoItem({ label, value }: { label: string; value: string }) {
  return <Box className="infoItem"><Text size="xs" c="dimmed">{label}</Text><Text fw={700}>{value}</Text></Box>
}

function nextHistory(list: RealtimePoint[], ts: number, a: number, b: number): RealtimePoint[] {
  const next = [...list, { ts, a, b }]
  return next.length > HISTORY_LIMIT ? next.slice(next.length - HISTORY_LIMIT) : next
}

function normalizeMetricTimestamp(ts: number): number {
  return ts > 1_000_000_000_000 ? ts : ts * 1000
}

function aggregateDisks(disks: SystemMetricsSnapshot['disks']): { total: number; used: number; percent: number } {
  // 多挂载点按容量加权，避免简单平均导致小分区过度影响总占用率。
  const total = disks.reduce((sum, disk) => sum + disk.total_bytes, 0)
  const used = disks.reduce((sum, disk) => sum + disk.used_bytes, 0)
  return { total, used, percent: total > 0 ? (used / total) * 100 : 0 }
}

function isCountedDisk(disk: SystemMetricsSnapshot['disks'][number]): boolean {
  return disk.counted !== false
}

function boolLabel(value: boolean | undefined): string {
  if (value === undefined) return '未知'
  return value ? '在线' : '离线'
}

function statusColor(status: string): string {
  if (status === 'connected') return 'green'
  if (status === 'error') return 'red'
  return 'yellow'
}

function progressColor(percent: number): string {
  if (percent >= 85) return 'red'
  if (percent >= 65) return 'yellow'
  return 'blue'
}

function loadStatus(percent: number): string {
  if (percent >= 85) return '过载'
  if (percent >= 65) return '偏高'
  return '正常'
}

function clampPercent(percent: number): number {
  return Math.max(0, Math.min(100, percent))
}

function loadColor(load: number, cores: number): string {
  if (cores <= 0) return 'blue'
  if (load >= cores) return 'red'
  if (load >= cores * 0.7) return 'yellow'
  return 'green'
}

function cpuValue(metrics: SystemMetricsSnapshot | null): string {
  if (!metrics) return '-'
  return metrics.cpu.info.model || `${metrics.cpu.info.logical_cores} 逻辑核心`
}

function detailTitle(detailType: DetailType | null): string {
  if (detailType === 'load') return '负载状态'
  if (detailType === 'cpu') return 'CPU 使用率'
  if (detailType === 'memory') return '内存使用率'
  if (detailType === 'disk') return '硬盘占用'
  return ''
}

function formatBytes(bytes?: number, mode: UnitMode = 'auto'): string {
  const value = bytes || 0
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = mode === 'auto' ? 0 : units.indexOf(mode)
  let next = value

  if (index < 0) index = 0
  if (mode === 'auto') {
    while (next >= 1024 && index < units.length - 1) {
      next /= 1024
      index += 1
    }
  } else if (index > 0) {
    next = value / Math.pow(1024, index)
  }

  return `${next.toFixed(index === 0 ? 0 : 2)} ${units[index]}`
}

function formatRate(bytes?: number, mode: UnitMode = 'auto'): string {
  return `${formatBytes(bytes, mode)}/s`
}

function chartUnit(mode: UnitMode): string | undefined {
  return mode === 'auto' ? undefined : `${mode}/s`
}

async function copyCommand(event: MouseEvent<HTMLElement>, value: string) {
  event.preventDefault()
  event.stopPropagation()

  const copied = await copyText(value)
  if (copied) {
    notifySuccess({ message: '完整命令已复制' })
    return
  }
  notifyError({ message: '复制失败，请手动复制' })
}

async function copyText(value: string): Promise<boolean> {
  if (!value) return false
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // Clipboard API 可能在非安全上下文或权限受限时失败，继续使用 textarea 兜底。
  }

  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', 'true')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  document.body.removeChild(textarea)
  return copied
}

function formatUptime(seconds?: number): string {
  const total = Math.floor(seconds || 0)
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}小时`
  if (hours > 0) return `${hours}小时 ${minutes}分钟`
  return `${minutes}分钟`
}

function formatDateTime(ts: number): string {
  const date = new Date(ts > 1_000_000_000_000 ? ts : ts * 1000)
  return date.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
