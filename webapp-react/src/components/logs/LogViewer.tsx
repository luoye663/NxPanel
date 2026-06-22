import { Box, Loader, ScrollArea, Text } from '@mantine/core'
import { useEffect, useRef } from 'react'

const MAX_RENDER_LINES = 1000

interface LogViewerProps {
  lines: string[]
  truncated?: boolean
  loading?: boolean
  emptyText?: string
  autoScroll?: boolean
  height?: number | string
}

export function LogViewer({ lines, truncated = false, loading = false, emptyText = '暂无日志内容', autoScroll = false, height = 600 }: LogViewerProps) {
  const viewportRef = useRef<HTMLDivElement>(null)
  // 后端仍按请求返回日志；这里仅限制 DOM 渲染尾部行数，避免超长日志拖慢浏览器。
  const visibleLines = lines.slice(-MAX_RENDER_LINES)
  const clientTruncated = lines.length > visibleLines.length

  useEffect(() => {
    if (!autoScroll || !viewportRef.current) return
    // 实时追踪时自动滚动到尾部，让最新日志始终可见。
    viewportRef.current.scrollTo({ top: viewportRef.current.scrollHeight, behavior: 'smooth' })
  }, [autoScroll, visibleLines.length])

  return (
    <Box className="logViewerShell">
      {truncated || clientTruncated ? (
        <Text size="xs" c="#fbbf24" bg="#111827" px={16} py={6} style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
          {truncated ? '日志内容过长，仅显示后端返回的最近部分。' : `前端仅渲染最近 ${MAX_RENDER_LINES} 行。`}
        </Text>
      ) : null}
      <ScrollArea h={height} className="logViewerScroll" viewportRef={viewportRef}>
        {loading ? (
          <Box py="xl" ta="center"><Loader size="sm" /></Box>
        ) : visibleLines.length > 0 ? (
          <pre className="logViewerPre">
            {visibleLines.map((line, index) => <code key={`${index}-${line}`}>{line}{'\n'}</code>)}
          </pre>
        ) : (
          <Text c="dimmed" ta="center" py="xl">{emptyText}</Text>
        )}
      </ScrollArea>
    </Box>
  )
}
