import { Tabs } from '@mantine/core'
import { PageShell } from '@/components/common/PageShell'
import { SectionCard } from '@/components/common/SectionCard'
import { LoginLogTab } from '@/components/logs/LoginLogTab'
import { OperationLogTab } from '@/components/logs/OperationLogTab'
import { ServiceLogTab } from '@/components/logs/ServiceLogTab'

export function LogsPage() {
  return (
    <PageShell>
      <SectionCard>
        <Tabs defaultValue="operation" keepMounted={false}>
          <Tabs.List>
            <Tabs.Tab value="operation">操作日志</Tabs.Tab>
            <Tabs.Tab value="login">登录日志</Tabs.Tab>
            <Tabs.Tab value="service">运行日志</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="operation"><OperationLogTab /></Tabs.Panel>
          <Tabs.Panel value="login"><LoginLogTab /></Tabs.Panel>
          <Tabs.Panel value="service"><ServiceLogTab /></Tabs.Panel>
        </Tabs>
      </SectionCard>
    </PageShell>
  )
}
