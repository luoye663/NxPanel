import { Badge, SimpleGrid, Skeleton, Stack, Text, Tooltip } from '@mantine/core'
import type { SystemOverview } from '@/api/types'

function yesNo(value?: boolean): string {
  if (value === undefined) return '未知'
  return value ? '是' : '否'
}

function PlainPathText({ value }: { value?: string | null }) {
  if (!value) return <Text size="sm" c="dimmed">-</Text>

  return (
    <Tooltip label={value} multiline maw={520} openDelay={300}>
      <Text size="sm" ff="monospace" truncate="end" maw="100%">
        {value}
      </Text>
    </Tooltip>
  )
}

interface NginxOverviewCardProps {
  overview?: SystemOverview
  loading?: boolean
}

export function NginxOverviewCard({ overview, loading }: NginxOverviewCardProps) {
  return (
    <Stack gap="sm">
      <Text fw={600}>安装信息</Text>
      {loading ? <Skeleton height={96} radius="md" /> : (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="sm">
          <Stack gap={4}>
            <Text size="xs" c="dimmed">Nginx 已检测</Text>
            <Badge color={overview?.nginx.detected ? 'green' : 'yellow'} variant="light">{yesNo(overview?.nginx.detected)}</Badge>
          </Stack>

          <Stack gap={4}>
            <Text size="xs" c="dimmed">Include 已安装</Text>
            <Badge color={overview?.nginx.include_installed ? 'green' : 'yellow'} variant="light">{yesNo(overview?.nginx.include_installed)}</Badge>
          </Stack>
          <Stack gap={4}>
            <Text size="xs" c="dimmed">Nginx 路径</Text>
            <PlainPathText value={overview?.nginx.bin} />
          </Stack>
          <Stack gap={4}>
            <Text size="xs" c="dimmed">版本</Text>
            <Text size="sm">{overview?.nginx.version || '-'}</Text>
          </Stack>
          <Stack gap={4}>
            <Text size="xs" c="dimmed">主配置</Text>
            <PlainPathText value={overview?.nginx.conf_path} />
          </Stack>
        </SimpleGrid>
      )}
    </Stack>
  )
}
