import { ActionIcon, Badge, Button, Group, Modal, ScrollArea, Stack, Text, Tooltip } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { IconAlertTriangle, IconShieldCheck } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { getAccessConfigDiagnostics, type AccessConfigDiagnostics } from '@/api/accessPolicy'

interface Props {
  siteId: string
  refreshKey: number
}

export function AccessPolicyDiagnostics({ siteId, refreshKey }: Props) {
  const [report, setReport] = useState<AccessConfigDiagnostics | null>(null)
  const [opened, setOpened] = useState(false)
  const [loading, setLoading] = useState(false)
  const [retry, setRetry] = useState(0)
  const mobile = useMediaQuery('(max-width: 48rem)')

  useEffect(() => {
    const controller = new AbortController()
    const notificationId = `access-policy-diagnostics-${siteId}`
    setReport(null)
    setLoading(true)
    // Deferring the request avoids duplicate notifications/requests during
    // StrictMode's setup/cleanup probe. Aborted results never replace newer ones.
    void Promise.resolve().then(async () => {
      if (controller.signal.aborted) return
      let result: AccessConfigDiagnostics
      try {
        result = await getAccessConfigDiagnostics(siteId, controller.signal)
      } catch {
        result = { complete: false, truncated: false, warnings: [{ message: '无法获取配置诊断，请稍后重新检查。诊断失败不代表策略保存失败，也不阻止保存。' }] }
      }
      if (controller.signal.aborted) return
      setReport(result)
      setLoading(false)
      if (result.warnings.length || !result.complete || result.truncated) {
        notifications.show({
          id: notificationId,
          color: 'yellow',
          title: result.complete ? `发现 ${result.warnings.length}${result.truncated ? '+' : ''} 项配置风险` : '配置诊断未完成',
          autoClose: 8000,
          message: <Stack gap="xs"><Text size="sm">自定义配置独立生效，本提示不阻止保存，不改变策略的应用状态。</Text><Button variant="subtle" size="xs" onClick={() => { notifications.hide(notificationId); setOpened(true) }}>查看详情</Button></Stack>,
        })
      }
    })
    return () => {
      controller.abort()
      notifications.hide(notificationId)
    }
  }, [siteId, refreshKey, retry])

  useEffect(() => {
    if (opened) notifications.hide(`access-policy-diagnostics-${siteId}`)
  }, [opened, report, siteId])

  const hasWarnings = !!report && (report.warnings.length > 0 || !report.complete || report.truncated)
  const label = loading ? '正在检查配置' : hasWarnings ? '查看配置警告' : '查看配置诊断'

  return <>
    <Tooltip label={label}><ActionIcon variant="subtle" color={hasWarnings ? 'yellow' : 'blue'} size="lg" loading={loading} aria-label={label} onClick={() => setOpened(true)}>{hasWarnings ? <IconAlertTriangle size={20} /> : <IconShieldCheck size={20} />}</ActionIcon></Tooltip>
    <Modal opened={opened} onClose={() => setOpened(false)} title="配置诊断" size="lg" fullScreen={mobile}>
      <Stack>
        <Text>统一策略仅编排面板管理的规则。自定义配置及上游应用仍独立生效；诊断不会修改配置。</Text>
        {report && !report.complete ? <Text c="orange.8">检查不完整，未列出的问题不代表不存在。路径权限和 nginx -t 等写入检查仍会正常执行。</Text> : null}
        {report?.complete && !report.warnings.length ? <Text>本次检查未发现已知冲突指令。这不代表已验证整个请求处理链。</Text> : null}
        <ScrollArea.Autosize mah={mobile ? '60vh' : 400} type="auto" offsetScrollbars>
          <Stack gap="lg">
            {report?.warnings.map((warning, index) => <Stack gap={4} key={`${warning.path}-${warning.line}-${index}`}>
              {warning.path ? <Text ff="monospace" size="sm" style={{ overflowWrap: 'anywhere' }}>{warning.path}</Text> : null}
              <Group gap="xs">{warning.line ? <Text size="sm">第 {warning.line} 行</Text> : null}{warning.directive ? <Badge variant="light" color="orange" c="orange.8" tt="none">{warning.directive}</Badge> : null}</Group>
              <Text style={{ overflowWrap: 'anywhere' }}>{warning.message}</Text>
            </Stack>)}
          </Stack>
        </ScrollArea.Autosize>
        {report?.truncated ? <Text c="orange.8">警告过多，仅展示前 100 项。</Text> : null}
        <Group justify="space-between"><Button variant="default" loading={loading} onClick={() => setRetry((value) => value + 1)}>重新检查</Button><Button onClick={() => setOpened(false)}>关闭</Button></Group>
      </Stack>
    </Modal>
  </>
}
