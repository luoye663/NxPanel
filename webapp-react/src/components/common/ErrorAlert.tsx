import { Alert, Code, Stack, Text } from '@mantine/core'
import { ApiError } from '@/api/client'

const SENSITIVE_KEYS = ['token', 'secret', 'password', 'private_key', 'pem', 'key_pem']

function isSensitiveKey(key: string): boolean {
  const normalized = key.toLowerCase()
  return SENSITIVE_KEYS.some((sensitive) => normalized.includes(sensitive))
}

function sanitizeDetails(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeDetails(item))
  }

  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, item]) => [
        key,
        isSensitiveKey(key) ? '[已隐藏]' : sanitizeDetails(item),
      ])
    )
  }

  return value
}

interface ErrorAlertProps {
  error: unknown
  title?: string
}

function getWhitelistGuidance(apiError: ApiError | null, message: string): string[] {
  const details = apiError?.details as { allowed_config?: unknown } | undefined
  const allowedConfig = Array.isArray(details?.allowed_config) ? details.allowed_config.filter((value): value is string => typeof value === 'string') : []
  const hintedByMessage = apiError?.code === 'AGENT_DENIED' || message.includes('白名单') || message.includes('路径不在允许的目录范围内')
  if (!hintedByMessage && allowedConfig.length === 0) return []

  const guidance = ['请在 config.yaml 中补充对应白名单后重启 nxpanel-agent。']
  if (allowedConfig.length > 0) guidance.push(`可检查配置项：${allowedConfig.join('、')}`)
  return guidance
}

export function ErrorAlert({ error, title = '请求失败' }: ErrorAlertProps) {
  const apiError = error instanceof ApiError ? error : null
  const message = apiError?.message || (error instanceof Error ? error.message : '未知错误')
  const safeDetails = apiError?.details ? sanitizeDetails(apiError.details) : null
  const whitelistGuidance = getWhitelistGuidance(apiError, message)

  return (
    <Alert color="red" title={title}>
      <Stack gap="xs">
        <Text size="sm">{message}</Text>
        {whitelistGuidance.map((item) => <Text key={item} size="sm">{item}</Text>)}
        {apiError?.request_id ? <Text size="xs" c="dimmed">Request ID: {apiError.request_id}</Text> : null}
        {safeDetails ? (
          // 错误详情展示前先脱敏，避免把 token、secret、PEM 私钥渲染到页面。
          <Code block>{JSON.stringify(safeDetails, null, 2)}</Code>
        ) : null}
      </Stack>
    </Alert>
  )
}
