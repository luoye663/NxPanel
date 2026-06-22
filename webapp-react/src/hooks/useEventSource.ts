import { useEffect, useRef, useState } from 'react'

export type EventSourceStatus = 'connecting' | 'connected' | 'error' | 'closed'

interface UseEventSourceOptions {
  onMessage?: (event: MessageEvent<string>) => void
  onDone?: () => void
  onError?: (event: Event) => void
}

export function useEventSource(url: string | null, options: UseEventSourceOptions = {}) {
  const [status, setStatus] = useState<EventSourceStatus>(url ? 'connecting' : 'closed')
  const sourceRef = useRef<EventSource | null>(null)
  const onMessageRef = useRef(options.onMessage)
  const onDoneRef = useRef(options.onDone)
  const onErrorRef = useRef(options.onError)

  onMessageRef.current = options.onMessage
  onDoneRef.current = options.onDone
  onErrorRef.current = options.onError

  useEffect(() => {
    if (!url) {
      setStatus('closed')
      return undefined
    }

    setStatus('connecting')
    const source = new EventSource(url)
    sourceRef.current = source

    source.onopen = () => {
      setStatus('connected')
    }

    source.onmessage = (event) => {
      if (event.data === '[DONE]') {
        // ACME 日志流用 [DONE] 表示任务结束，收到后立即关闭连接并交给页面刷新订单状态。
        source.close()
        if (sourceRef.current === source) sourceRef.current = null
        setStatus('closed')
        onDoneRef.current?.()
        return
      }
      onMessageRef.current?.(event)
    }

    source.onerror = (event) => {
      setStatus('error')
      onErrorRef.current?.(event)
      // 原生 EventSource 会自动重连；这里不手动 new，避免重复连接和重复消费事件。
    }

    return () => {
      // URL/scope 变化或组件卸载时必须关闭旧连接，防止 Dashboard 累积多个 SSE 订阅。
      source.close()
      if (sourceRef.current === source) {
        sourceRef.current = null
      }
      setStatus('closed')
    }
  }, [url])

  function close() {
    sourceRef.current?.close()
    sourceRef.current = null
    setStatus('closed')
  }

  return { status, close }
}
