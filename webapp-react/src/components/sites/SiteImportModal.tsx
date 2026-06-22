import { Alert, Badge, Button, Checkbox, Group, Loader, Modal, ScrollArea, Stack, Table, Text } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { importSite, type ImportScanItem } from '@/api/sites'
import { PathCell } from '@/components/common/PathCell'
import { showErrorModal } from '@/utils/errorModal'
import { notifySuccess, notifyWarning } from '@/utils/notify'

interface SiteImportModalProps {
  opened: boolean
  scanning: boolean
  items: ImportScanItem[]
  onClose: () => void
}

export function SiteImportModal({ opened, scanning, items, onClose }: SiteImportModalProps) {
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<string[]>([])
  const importMutation = useMutation({ mutationFn: importSite })
  const hasWarnings = items.some((item) => (item.warnings?.length || 0) > 0)

  useEffect(() => {
    if (opened) setSelected([])
  }, [opened, items])

  async function handleImport() {
    if (selected.length === 0) {
      notifyWarning({ message: '请选择要导入的站点' })
      return
    }
    let successCount = 0
    for (const sourceFile of selected) {
      try {
        await importMutation.mutateAsync(sourceFile)
        successCount++
      } catch (error) {
        showErrorModal(error, `导入失败：${sourceFile}`)
      }
    }
    if (successCount > 0) {
      notifySuccess({ message: `成功导入 ${successCount} 个站点` })
      await queryClient.invalidateQueries({ queryKey: ['sites'] })
      onClose()
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="导入旧站点" size="xl" closeOnClickOutside={false}>
      {scanning ? (
        <Group justify="center" py="xl"><Loader /><Text>正在扫描 Nginx 配置...</Text></Group>
      ) : (
        <Stack gap="md">
          {items.length === 0 ? (
            <Alert color="blue" title="未发现可导入的旧站点">所有 Nginx server block 均已被面板管理，或未检测到 server_name。</Alert>
          ) : (
            <>
              <Alert color={hasWarnings ? 'yellow' : 'blue'} title={hasWarnings ? '检测到白名单风险' : undefined}>
                导入后配置文件保持原样（手动模式），允许继续导入。
                {hasWarnings ? ' 部分旧站点的配置文件、根目录或日志路径未在 Agent 白名单内，导入后对应功能会不可用。请在 config.yaml 中补充白名单并重启 nxpanel-agent。' : ' 站点管理、日志、文件管理功能可直接使用。'}
              </Alert>
              <ScrollArea.Autosize mah={420}>
                <Table striped highlightOnHover withTableBorder className="siteImportTable">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th w={44} />
                      <Table.Th>配置文件</Table.Th>
                      <Table.Th>域名</Table.Th>
                      <Table.Th>端口</Table.Th>
                      <Table.Th>根目录</Table.Th>
                      <Table.Th>风险提示</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {items.map((item) => {
                      const checked = selected.includes(item.source_file)
                      return (
                        <Table.Tr key={item.source_file}>
                          <Table.Td><Checkbox aria-label="选择导入站点" checked={checked} onChange={(event) => { const nextChecked = event.currentTarget.checked; setSelected((current) => nextChecked ? [...current, item.source_file] : current.filter((value) => value !== item.source_file)) }} /></Table.Td>
                          <Table.Td><PathCell value={item.source_file} maxWidth={260} /></Table.Td>
                          <Table.Td><Group gap={4}>{item.server_names.map((name) => <Badge key={name} variant="light">{name}</Badge>)}</Group></Table.Td>
                          <Table.Td>{item.listen[0] || '-'}</Table.Td>
                          <Table.Td><PathCell value={item.root_path} maxWidth={220} /></Table.Td>
                          <Table.Td>
                            {(item.warnings?.length || 0) > 0 ? (
                              <Stack gap={4}>
                                {item.warnings?.map((warning) => <Text key={warning} size="xs" c="yellow.8">{warning}</Text>)}
                              </Stack>
                            ) : (
                              <Badge color="green" variant="light">白名单正常</Badge>
                            )}
                          </Table.Td>
                        </Table.Tr>
                      )
                    })}
                  </Table.Tbody>
                </Table>
              </ScrollArea.Autosize>
            </>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>关闭</Button>
            {items.length > 0 ? <Button loading={importMutation.isPending} disabled={selected.length === 0} onClick={handleImport}>导入选中站点（{selected.length}）</Button> : null}
          </Group>
        </Stack>
      )}
    </Modal>
  )
}
