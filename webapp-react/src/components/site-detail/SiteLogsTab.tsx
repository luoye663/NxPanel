import { ActionIcon, Badge, Button, Group, Menu, Modal, SegmentedControl, Select, Table, Text, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDotsVertical, IconDownload, IconHistory, IconPlayerPlay, IconPlayerStop, IconRefresh, IconSearch, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { withGatePrefix } from '@/api/gate'
import { deleteRotatedLog, downloadLog, getLogs, getRotatedLogs, getRotatedLogTail, searchLogs, truncateLog, type RotatedLogItem } from '@/api/logs'
import type { SiteDetail } from '@/api/types'
import { ErrorAlert } from '@/components/common/ErrorAlert'
import { PathCell } from '@/components/common/PathCell'
import { SectionCard } from '@/components/common/SectionCard'
import { LogViewer } from '@/components/logs/LogViewer'
import { confirmDanger } from '@/utils/confirm'
import { notifySuccess } from '@/utils/notify'

interface SiteLogsTabProps {
  site: SiteDetail
}

type SiteLogType = 'access' | 'error'

const lineOptions = [50, 100, 200, 500, 1000].map((value) => ({ value: String(value), label: `${value} 行` }))

export function SiteLogsTab({ site }: SiteLogsTabProps) {
  const queryClient = useQueryClient()
  const [logType, setLogType] = useState<SiteLogType>('access')
  const [lines, setLines] = useState(200)
  const [keyword, setKeyword] = useState('')
  const [filteredLines, setFilteredLines] = useState<string[] | null>(null)
  const [historyOpened, setHistoryOpened] = useState(false)
  const [selectedRotated, setSelectedRotated] = useState<RotatedLogItem | null>(null)
  const [streaming, setStreaming] = useState(false)
  const [streamLines, setStreamLines] = useState<string[]>([])
  const logsQueryKey = ['site-detail', site.id, 'logs', logType, lines] as const
  const logsQuery = useQuery({ queryKey: logsQueryKey, queryFn: () => getLogs(site.id, { type: logType, lines }), enabled: !selectedRotated && !streaming })
  const rotatedQuery = useQuery({ queryKey: ['site-detail', site.id, 'logs', logType, 'rotated'], queryFn: () => getRotatedLogs(site.id, { type: logType }), enabled: historyOpened })
  const truncateMutation = useMutation({ mutationFn: () => truncateLog(site.id, { type: logType, confirm: true }) })
  const deleteRotatedMutation = useMutation({ mutationFn: (name: string) => deleteRotatedLog(site.id, { type: logType, name }) })
  const typeLabel = logType === 'access' ? '访问' : '错误'

  useEffect(() => {
    setFilteredLines(null)
    setSelectedRotated(null)
    setStreaming(false)
    setStreamLines([])
  }, [logType, site.id])

  useEffect(() => {
    if (!streaming || selectedRotated) return
    const source = new EventSource(withGatePrefix(`/sites/${site.id}/logs/stream?type=${logType}&from=tail`))
    source.addEventListener('line', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as { line: string }
      // 实时追踪只保留最近 1000 行，避免长时间打开页面造成前端内存持续增长。
      setStreamLines((current) => [...current, payload.line].slice(-1000))
    })
    source.addEventListener('error', () => source.close())
    return () => source.close()
  }, [logType, selectedRotated, site.id, streaming])

  function handleTruncate() {
    confirmDanger({
      title: '清空日志',
      message: `确认清空${typeLabel}日志？此操作不可恢复。`,
      confirmLabel: '确认清空',
      errorTitle: '清空日志失败',
      onConfirm: async () => {
        await truncateMutation.mutateAsync()
        notifySuccess({ message: `${typeLabel}日志已清空` })
        await queryClient.invalidateQueries({ queryKey: ['site-detail', site.id, 'logs'] })
      },
    })
  }

  async function handleSearch() {
    if (!keyword.trim()) {
      setFilteredLines(null)
      return
    }
    const result = await searchLogs(site.id, { type: logType, q: keyword.trim(), lines, rotated: selectedRotated?.name })
    setFilteredLines(result.lines)
  }

  async function handleDownload(rotatedName?: string) {
    const resp = await downloadLog(site.id, { type: logType, rotated: rotatedName || selectedRotated?.name })
    const url = window.URL.createObjectURL(resp.data)
    const link = document.createElement('a')
    link.href = url
    link.download = rotatedName || selectedRotated?.name || `${site.primary_domain}.${logType}.log`
    link.click()
    window.URL.revokeObjectURL(url)
  }

  async function openRotated(item: RotatedLogItem) {
    const result = await getRotatedLogTail(site.id, { type: logType, name: item.name, lines })
    setSelectedRotated(item)
    setFilteredLines(result.lines)
    setStreaming(false)
    setHistoryOpened(false)
  }

  function removeRotated(item: RotatedLogItem) {
    confirmDanger({
      title: '删除历史日志',
      message: `确认删除 ${item.name}？此操作不可恢复。`,
      confirmLabel: '确认删除',
      errorTitle: '删除历史日志失败',
      onConfirm: async () => {
        await deleteRotatedMutation.mutateAsync(item.name)
        notifySuccess({ message: '历史日志已删除' })
        await rotatedQuery.refetch()
      },
    })
  }

  const visibleLines = streaming ? streamLines : (filteredLines ?? logsQuery.data?.lines ?? [])

  return (
    <SectionCard className="siteLogsCard" p="md">
      <div className="siteLogsLayout">
        <Group justify="space-between" align="center" gap="sm" className="siteLogsTopbar">
          <SegmentedControl
            value={logType}
            data={[{ value: 'access', label: 'Access Log' }, { value: 'error', label: 'Error Log' }]}
            onChange={(value) => setLogType(value as SiteLogType)}
          />
          {logsQuery.data?.path ? (
            <Group gap="sm" align="center">
              <PathCell value={logsQuery.data.path} maxWidth={520} />
              {logsQuery.data.max_bytes ? <Text size="xs" c="dimmed">最大读取 {(logsQuery.data.max_bytes / 1024 / 1024).toFixed(1)} MB</Text> : null}
            </Group>
          ) : null}
          {streaming ? <Badge color="green" variant="light">实时追踪中</Badge> : <Text size="xs" c="dimmed">{selectedRotated ? '历史日志' : '当前日志'}</Text>}
        </Group>

        <Group gap="xs" className="siteLogsFilter">
          <TextInput flex={1} placeholder="搜索过滤，普通字符串匹配" value={keyword} onChange={(event) => setKeyword(event.currentTarget.value)} onKeyDown={(event) => { if (event.key === 'Enter') void handleSearch() }} />
          <Button variant="light" leftSection={<IconSearch size={16} />} onClick={handleSearch}>过滤</Button>
          {selectedRotated ? <Badge color="blue">历史：{selectedRotated.name}</Badge> : null}
          {selectedRotated ? <Button variant="subtle" onClick={() => { setSelectedRotated(null); setFilteredLines(null) }}>返回当前</Button> : null}
        </Group>

        {logsQuery.isError ? <ErrorAlert error={logsQuery.error} title="加载站点日志失败" /> : null}
        <div className="siteLogsViewer">
          <LogViewer lines={visibleLines} truncated={logsQuery.data?.truncated} loading={!streaming && (logsQuery.isLoading || logsQuery.isFetching)} autoScroll={streaming} height="100%" />
        </div>
        <Group justify="space-between" gap="sm" className="siteLogsBottomToolbar">
          <Select w={112} data={lineOptions} value={String(lines)} allowDeselect={false} onChange={(value) => value && setLines(Number(value))} />
          <Group gap="xs" justify="flex-end">
            <Button variant="light" leftSection={<IconRefresh size={16} />} disabled={!!selectedRotated || streaming} loading={!streaming && logsQuery.isFetching} onClick={() => logsQuery.refetch()}>刷新</Button>
            <Button variant={streaming ? 'filled' : 'light'} leftSection={streaming ? <IconPlayerStop size={16} /> : <IconPlayerPlay size={16} />} disabled={!!selectedRotated} onClick={() => setStreaming((value) => !value)}>{streaming ? '停止追踪' : '实时追踪'}</Button>
            <Menu position="top-end" withinPortal>
              <Menu.Target>
                <ActionIcon variant="light" aria-label="更多日志操作"><IconDotsVertical size={16} /></ActionIcon>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Item leftSection={<IconDownload size={16} />} onClick={() => handleDownload()}>下载日志</Menu.Item>
                <Menu.Item leftSection={<IconHistory size={16} />} onClick={() => setHistoryOpened(true)}>切割历史</Menu.Item>

                <Menu.Item color="red" leftSection={<IconTrash size={16} />} disabled={truncateMutation.isPending} onClick={handleTruncate}>清空日志</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </div>

      <Modal opened={historyOpened} onClose={() => setHistoryOpened(false)} title="切割历史日志" size="lg">
        <Table.ScrollContainer minWidth={640}>
          <Table striped highlightOnHover>
            <Table.Thead><Table.Tr><Table.Th>文件名</Table.Th><Table.Th>大小</Table.Th><Table.Th>修改时间</Table.Th><Table.Th>操作</Table.Th></Table.Tr></Table.Thead>
            <Table.Tbody>
              {(rotatedQuery.data?.items || []).map((item) => (
                <Table.Tr key={item.name}>
                  <Table.Td><Text size="sm" ff="monospace">{item.name}</Text></Table.Td>
                  <Table.Td>{(item.size / 1024).toFixed(1)} KB</Table.Td>
                  <Table.Td>{item.mod_time}</Table.Td>
                  <Table.Td>
                    <Group gap={4}>
                      <Button variant="light" onClick={() => openRotated(item)}>查看</Button>
                      <ActionIcon variant="subtle" onClick={() => handleDownload(item.name)}><IconDownload size={16} /></ActionIcon>
                      <ActionIcon color="red" variant="subtle" onClick={() => removeRotated(item)}><IconTrash size={16} /></ActionIcon>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Modal>
    </SectionCard>
  )
}
