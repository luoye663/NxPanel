import { Alert, Center, Loader, Stack, Text } from '@mantine/core'
import { useColorScheme } from '@mantine/hooks'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { callPluginRPC } from '@/api/plugins'
import { gateSecret } from '@/api/gate'
import { notifyError, notifySuccess } from '@/utils/notify'

interface PluginSandboxProps {
  pluginId: string
  entry: string
  allowedRPCMethods?: string[]
  context?: Record<string, unknown>
  title: string
}
interface BridgeRequest {
  id: string
  type: 'rpc' | 'notify' | 'confirm' | 'navigate' | 'theme' | 'resize'
  payload?: unknown
}

interface BridgeResponse {
  id: string
  ok: boolean
  result?: unknown
  error?: { code: string; message: string }
}

const MIN_HEIGHT = 360
const MAX_HEIGHT = 2400

function safeEntryPath(entry: string): string {
  return entry.split('/').filter((part) => part && part !== '.' && part !== '..').map(encodeURIComponent).join('/')
}

export function PluginSandbox({ pluginId, entry, allowedRPCMethods = [], context, title }: PluginSandboxProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const channelRef = useRef<MessageChannel | null>(null)
  const navigate = useNavigate()
  const colorScheme = useColorScheme()
  const nonce = useMemo(() => crypto.randomUUID(), [pluginId, entry])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [height, setHeight] = useState(MIN_HEIGHT)
  const allowedMethods = useMemo(() => new Set(allowedRPCMethods), [allowedRPCMethods])
  const src = `/api/v1/${encodeURIComponent(gateSecret)}/plugins/${encodeURIComponent(pluginId)}/ui/${safeEntryPath(entry)}`

  const respond = useCallback((port: MessagePort, response: BridgeResponse) => port.postMessage(response), [])

  const handleRequest = useCallback(async (port: MessagePort, request: BridgeRequest) => {
    if (!request || typeof request.id !== 'string' || typeof request.type !== 'string') return
    try {
      if (request.type === 'rpc') {
        const payload = request.payload as { method?: unknown; payload?: unknown }
        const method = typeof payload?.method === 'string' ? payload.method : ''
        if (!method || !allowedMethods.has(method)) throw new Error('该插件未获准调用此方法')
        const result = await callPluginRPC(pluginId, { method, payload: payload.payload, context })
        respond(port, { id: request.id, ok: true, result })
        return
      }
      if (request.type === 'notify') {
        const payload = request.payload as { level?: string; message?: unknown }
        const message = typeof payload?.message === 'string' ? payload.message.slice(0, 500) : ''
        if (!message) throw new Error('通知内容为空')
        if (payload.level === 'error') notifyError({ message })
        else notifySuccess({ message })
        respond(port, { id: request.id, ok: true })
        return
      }
      if (request.type === 'confirm') {
        const payload = request.payload as { message?: unknown }
        const message = typeof payload?.message === 'string' ? payload.message.slice(0, 500) : '确认执行此操作？'
        respond(port, { id: request.id, ok: true, result: window.confirm(message) })
        return
      }
      if (request.type === 'navigate') {
        const payload = request.payload as { path?: unknown }
        const path = typeof payload?.path === 'string' ? payload.path : ''
        if (!path.startsWith('/') || path.startsWith('//') || path.includes('://')) throw new Error('仅允许跳转到面板内部页面')
        navigate(path)
        respond(port, { id: request.id, ok: true })
        return
      }
      if (request.type === 'theme') {
        respond(port, { id: request.id, ok: true, result: { colorScheme } })
        return
      }
      if (request.type === 'resize') {
        const requested = Number((request.payload as { height?: unknown })?.height)
        if (!Number.isFinite(requested)) throw new Error('无效的页面高度')
        setHeight(Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, Math.round(requested))))
        respond(port, { id: request.id, ok: true })
        return
      }
      throw new Error('不支持的 Bridge 方法')
    } catch (error) {
      respond(port, {
        id: request.id,
        ok: false,
        error: { code: 'BRIDGE_REQUEST_FAILED', message: error instanceof Error ? error.message : '请求失败' },
      })
    }
  }, [allowedMethods, colorScheme, context, navigate, pluginId, respond])

  useEffect(() => () => channelRef.current?.port1.close(), [])

  function connect() {
    const frameWindow = iframeRef.current?.contentWindow
    if (!frameWindow) return
    channelRef.current?.port1.close()
    const channel = new MessageChannel()
    channelRef.current = channel
    channel.port1.onmessage = (event: MessageEvent<BridgeRequest>) => void handleRequest(channel.port1, event.data)
    channel.port1.start()
    frameWindow.postMessage({ type: 'nxpanel:connect', version: 1, plugin_id: pluginId, nonce, context, color_scheme: colorScheme }, '*', [channel.port2])
    setLoading(false)
  }

  return (
    <Stack gap="sm" className="pluginSandboxRoot">
      {failed ? (
        <Alert color="red" title="插件页面加载失败">
          插件资源未能加载。请确认插件已启用且当前版本健康，然后刷新页面重试。
        </Alert>
      ) : null}
      <div className="pluginSandboxFrameWrap" style={{ minHeight: height }}>
        {loading && !failed ? (
          <Center className="pluginSandboxLoading">
            <Stack align="center" gap="xs"><Loader size="sm" /><Text c="dimmed">正在启动隔离插件页面...</Text></Stack>
          </Center>
        ) : null}
        <iframe
          ref={iframeRef}
          className="pluginSandboxFrame"
          src={src}
          title={title}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          style={{ height }}
          onLoad={connect}
          onError={() => { setLoading(false); setFailed(true) }}
        />
      </div>
    </Stack>
  )
}
