import { Anchor, Badge, Modal, Stack, Table, Text } from '@mantine/core'
import type { ACMEOrderItem } from '@/api/acme'

interface ACMEErrorDetailModalProps {
  order: ACMEOrderItem | null
  opened: boolean
  onClose: () => void
}

const ACME_ERROR_MAP: Record<string, string> = {
  dns_resolution_failed: 'DNS 解析失败',
  connection_timeout: '连接超时',
  challenge_failed: '域名验证失败',
  rate_limited: '请求频率超限',
  account_registration_failed: '账号注册失败',
  order_creation_failed: '订单创建失败',
  certificate_download_failed: '证书下载失败',
  validation_timeout: '验证超时',
  unknown: '未知错误',
}

export function ACMEErrorDetailModal({ order, opened, onClose }: ACMEErrorDetailModalProps) {
  const errorType = order?.error_type || 'unknown'
  return (
    <Modal opened={opened} onClose={onClose} title={ACME_ERROR_MAP[errorType] || '申请失败'} size="lg" centered>
      {order ? (
        <Stack gap="md">
          <Table withTableBorder>
            <Table.Tbody>
              <Table.Tr><Table.Th w={110}>验证 URL</Table.Th><Table.Td>{order.verification_url ? <Anchor href={order.verification_url} target="_blank" rel="noreferrer">点击查看</Anchor> : '-'}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>验证内容</Table.Th><Table.Td>{order.verification_content || '-'}</Table.Td></Table.Tr>
              <Table.Tr><Table.Th>错误代码</Table.Th><Table.Td><Text style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{order.error_detail || '-'}</Text></Table.Td></Table.Tr>
              <Table.Tr><Table.Th>验证结果</Table.Th><Table.Td><Badge color="red">验证失败</Badge></Table.Td></Table.Tr>
            </Table.Tbody>
          </Table>
        </Stack>
      ) : null}
    </Modal>
  )
}
