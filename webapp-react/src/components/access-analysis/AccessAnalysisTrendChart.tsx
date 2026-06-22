import { useState, type MouseEvent } from 'react'
import { Box, Group, Text } from '@mantine/core'
import type { AccessAnalysisHourlyPoint } from '@/api/types'

const WIDTH = 640
const HEIGHT = 260
const PADDING_TOP = 16
const PADDING_RIGHT = 20
const PADDING_BOTTOM = 34
const PADDING_LEFT = 64

export function AccessAnalysisTrendChart({ points }: { points: AccessAnalysisHourlyPoint[] }) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const max = Math.max(1, ...points.flatMap((point) => [point.requests, point.unique_ips, point.status_4xx + point.status_5xx]))
  const pathReq = buildPath(points.map((p) => p.requests), max)
  const pathIP = buildPath(points.map((p) => p.unique_ips), max)
  const pathErr = buildPath(points.map((p) => p.status_4xx + p.status_5xx), max)
  const hovered = hoverIndex === null ? null : points[hoverIndex]
  const hoverX = hoverIndex === null ? 0 : xForIndex(hoverIndex, points.length)
  const current = points.length > 0 ? points[points.length - 1] : undefined

  function handleMouseMove(event: MouseEvent<SVGSVGElement>) {
    if (points.length === 0) return
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left) / rect.width
    const svgX = ratio * WIDTH
    const index = Math.round(((svgX - PADDING_LEFT) / (WIDTH - PADDING_LEFT - PADDING_RIGHT)) * Math.max(1, points.length - 1))
    setHoverIndex(Math.max(0, Math.min(points.length - 1, index)))
  }

  return (
    <Box className="realtimeChart">
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label="访问趋势" onMouseMove={handleMouseMove} onMouseLeave={() => setHoverIndex(null)}>
        <line x1={PADDING_LEFT} y1={HEIGHT - PADDING_BOTTOM} x2={WIDTH - PADDING_RIGHT} y2={HEIGHT - PADDING_BOTTOM} className="chartAxis" />
        <line x1={PADDING_LEFT} y1={PADDING_TOP} x2={PADDING_LEFT} y2={HEIGHT - PADDING_BOTTOM} className="chartAxis" />
        {buildYLabels(max).map((label) => <g key={label.value}><line x1={PADDING_LEFT} y1={label.y} x2={WIDTH - PADDING_RIGHT} y2={label.y} className="chartGridLine" /><text x={PADDING_LEFT - 8} y={label.y + 4} textAnchor="end" className="chartAxisLabel">{label.text}</text></g>)}
        {buildXLabels(points).map((label) => <text key={`${label.index}-${label.text}`} x={label.x} y={HEIGHT - 10} textAnchor={label.anchor} className="chartAxisLabel">{label.text}</text>)}
        <path d={pathReq} className="chartLine chartLineA" />
        <path d={pathIP} className="chartLine chartLineB" />
        <path d={pathErr} className="chartLine" stroke="#fa5252" />
        {hovered ? (
          <g>
            <line x1={hoverX} y1={PADDING_TOP} x2={hoverX} y2={HEIGHT - PADDING_BOTTOM} className="chartHoverLine" />
            <circle cx={hoverX} cy={yForValue(hovered.requests, max)} r={4} className="chartPointA" />
            <circle cx={hoverX} cy={yForValue(hovered.unique_ips, max)} r={4} className="chartPointB" />
            <circle cx={hoverX} cy={yForValue(hovered.status_4xx + hovered.status_5xx, max)} r={4} fill="#fa5252" stroke="#fff" strokeWidth={2} />
            <g transform={`translate(${Math.min(hoverX + 10, WIDTH - 190)}, ${PADDING_TOP + 8})`}>
              <rect width="178" height="78" rx="8" className="chartTooltipBox" />
              <text x="10" y="18" className="chartTooltipText">{formatHour(hovered.hour)}</text>
              <text x="10" y="38" className="chartTooltipText">访问量: {hovered.requests}</text>
              <text x="10" y="54" className="chartTooltipText">独立 IP: {hovered.unique_ips}</text>
              <text x="10" y="70" className="chartTooltipText">错误量: {hovered.status_4xx + hovered.status_5xx}</text>
            </g>
          </g>
        ) : null}
      </svg>
      <Group gap="md"><Text size="xs">访问量 {current?.requests ?? 0}</Text><Text size="xs">独立 IP {current?.unique_ips ?? 0}</Text><Text size="xs" c="red">错误量 {(current?.status_4xx ?? 0) + (current?.status_5xx ?? 0)}</Text></Group>
    </Box>
  )
}

function buildPath(values: number[], max: number): string {
  if (values.length === 0) return ''
  return values.map((value, index) => {
    const x = xForIndex(index, values.length)
    const y = yForValue(value, max)
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`
  }).join(' ')
}

function xForIndex(index: number, total: number): number {
  return PADDING_LEFT + (index / Math.max(1, total - 1)) * (WIDTH - PADDING_LEFT - PADDING_RIGHT)
}

function yForValue(value: number, maxValue: number): number {
  return HEIGHT - PADDING_BOTTOM - (value / maxValue) * (HEIGHT - PADDING_TOP - PADDING_BOTTOM)
}

function buildYLabels(maxValue: number) {
  return [0, maxValue / 2, maxValue].map((value) => ({ value, y: yForValue(value, maxValue), text: formatNumber(value) }))
}

function buildXLabels(points: AccessAnalysisHourlyPoint[]) {
  if (points.length === 0) return []
  const indexes = Array.from(new Set([0, Math.floor((points.length - 1) / 2), points.length - 1]))
  return indexes.map((index) => ({
    index,
    x: xForIndex(index, points.length),
    text: formatHour(points[index].hour),
    anchor: index === 0 ? 'start' : index === points.length - 1 ? 'end' : 'middle' as 'start' | 'middle' | 'end',
  }))
}

function formatHour(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatNumber(value: number): string {
  if (value >= 10000) return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)}万`
  return Math.round(value).toString()
}
