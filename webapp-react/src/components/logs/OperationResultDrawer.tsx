import { Badge, Code, Drawer, Group, Loader, ScrollArea, Stack, Table, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { getOperation } from '@/api/operations'
import { MonoText } from '@/components/common/MonoText'
import { StatusBadge } from '@/components/common/StatusBadge'

interface OperationResultDrawerProps {
  operationId: string | null
  opened: boolean
  onClose: () => void
}

export function OperationResultDrawer({ operationId, opened, onClose }: OperationResultDrawerProps) {
  const detailQuery = useQuery({
    queryKey: ['operations', 'detail', operationId],
    queryFn: () => getOperation(operationId!),
    enabled: opened && Boolean(operationId),
  })
  const detail = detailQuery.data

  return (
    <Drawer opened={opened} onClose={onClose} title="操作详情" position="right" size="lg">
      {detailQuery.isLoading ? (
        <Group justify="center" py="xl"><Loader size="sm" /></Group>
      ) : detail ? (
        <Stack gap="sm">
          <Badge variant="light" color="gray">DETAIL</Badge>
          <Table withTableBorder withColumnBorders>
            <Table.Tbody>
              <Table.Tr><Table.Th w={120}>操作 ID</Table.Th><Table.Td><MonoText value={detail.id} maxWidth="100%" /></Table.Td></Table.Tr>
              <Table.Tr><Table.Th>操作类型</Table.Th><Table.Td>{detail.action}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>状态</Table.Th><Table.Td><StatusBadge kind="operation" value={detail.status} /></Table.Td></Table.Tr>
              {detail.error_code ? <Table.Tr><Table.Th>错误码</Table.Th><Table.Td><Badge color="red" variant="light">{detail.error_code}</Badge></Table.Td></Table.Tr> : null}
              {detail.error_message ? <Table.Tr><Table.Th>错误消息</Table.Th><Table.Td><Text c="red" size="sm">{detail.error_message}</Text></Table.Td></Table.Tr> : null}
            </Table.Tbody>
          </Table>
          {detail.stderr ? (
            <Stack gap="xs">
              <Text fw={600} size="sm">stderr 输出</Text>
              <Code block className="stderrBlock">{detail.stderr}</Code>
            </Stack>
          ) : null}
          {detail.backups?.length ? (
            <Stack gap="xs">
              <Text fw={600} size="sm">备份文件</Text>
              <ScrollArea>
                <Table withTableBorder miw={560}>
                  <Table.Thead><Table.Tr><Table.Th>原文件路径</Table.Th><Table.Th>备份路径</Table.Th></Table.Tr></Table.Thead>
                  <Table.Tbody>
                    {detail.backups.map((backup) => (
                      <Table.Tr key={`${backup.file_path}-${backup.backup_path}`}>
                        <Table.Td><MonoText value={backup.file_path} maxWidth={240} /></Table.Td>
                        <Table.Td><MonoText value={backup.backup_path} maxWidth={240} /></Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </ScrollArea>
            </Stack>
          ) : null}
        </Stack>
      ) : (
        <Text c="dimmed" ta="center" py="xl">未找到操作详情</Text>
      )}
    </Drawer>
  )
}
