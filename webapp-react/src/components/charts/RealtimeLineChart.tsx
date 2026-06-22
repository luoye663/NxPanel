import { useState, type MouseEvent } from 'react'
import { Box, Group, Text } from '@mantine/core'

export interface RealtimePoint {
  ts: number
  a: number
  b: number
}

interface RealtimeLineChartProps {
  points: RealtimePoint[]
  labelA: string
  labelB: string
  unit?: string
  windowSeconds?: number
  maxPoints?: number
  now?: number
}

const WIDTH = 640
const HEIGHT = 280
const PADDING_TOP = 14
const PADDING_RIGHT = 16
const PADDING_BOTTOM = 32
const PADDING_LEFT = 76
const DEFAULT_WINDOW_SECONDS = 180

export function RealtimeLineChart({ points, labelA, labelB, unit, windowSeconds = DEFAULT_WINDOW_SECONDS, maxPoints, now = Date.now() }: RealtimeLineChartProps) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const values = points.flatMap((point) => [point.a, point.b])
  const maxValue = Math.max(1, ...values) * 1.15
  const displayUnit = resolveDisplayUnit(maxValue, unit)
  const windowMs = Math.max(1, windowSeconds) * 1000
  const current = points.length > 0 ? points[points.length - 1] : undefined
  const latestTs = Math.max(normalizeTimestamp(now), current ? normalizeTimestamp(current.ts) : 0)
  const pathA = buildPath(points, 'a', maxValue, latestTs, windowMs)
  const pathB = buildPath(points, 'b', maxValue, latestTs, windowMs)
  const hovered = hoverIndex === null ? null : points[hoverIndex]
  const currentX = current ? xForTimestamp(current.ts, latestTs, windowMs) : 0
  const currentYA = current ? yForValue(current.a, maxValue) : 0
  const currentYB = current ? yForValue(current.b, maxValue) : 0
  const hoverX = hovered ? xForTimestamp(hovered.ts, latestTs, windowMs) : 0
  const hoverYA = hovered ? yForValue(hovered.a, maxValue) : 0
  const hoverYB = hovered ? yForValue(hovered.b, maxValue) : 0
  const xLabels = buildXLabels(latestTs, windowMs)
  const yLabels = buildYLabels(maxValue, displayUnit)
  const footerText = maxPoints && points.length < maxPoints ? `采集中 ${points.length}/${maxPoints} 个点` : `最近 ${formatDuration(windowMs)}`

  function handleMouseMove(event: MouseEvent<SVGSVGElement>) {
    if (points.length === 0) return

    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left) / rect.width
    const svgX = ratio * WIDTH
    setHoverIndex(nearestPointIndex(points, svgX, latestTs, windowMs))
  }

  return (
    <Box className="realtimeChart">
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label={`${labelA}与${labelB}实时趋势`} onMouseMove={handleMouseMove} onMouseLeave={() => setHoverIndex(null)}>
        <line x1={PADDING_LEFT} y1={HEIGHT - PADDING_BOTTOM} x2={WIDTH - PADDING_RIGHT} y2={HEIGHT - PADDING_BOTTOM} className="chartAxis" />
        <line x1={PADDING_LEFT} y1={PADDING_TOP} x2={PADDING_LEFT} y2={HEIGHT - PADDING_BOTTOM} className="chartAxis" />
        {yLabels.map((label) => <g key={label.value}><line x1={PADDING_LEFT} y1={label.y} x2={WIDTH - PADDING_RIGHT} y2={label.y} className="chartGridLine" /><text x={PADDING_LEFT - 8} y={label.y + 4} textAnchor="end" className="chartAxisLabel">{label.text}</text></g>)}
        {xLabels.map((label) => <text key={`${label.index}-${label.text}`} x={label.x} y={HEIGHT - 10} textAnchor={label.anchor} className="chartAxisLabel">{label.text}</text>)}
        <path d={pathA} className="chartLine chartLineA" />
        <path d={pathB} className="chartLine chartLineB" />
        {current ? (
          <g className="chartCurrentLayer">
            <circle cx={currentX} cy={currentYA} r={3.5} className="chartPointA" />
            <circle cx={currentX} cy={currentYB} r={3.5} className="chartPointB" />
          </g>
        ) : null}
        {hovered ? (
          <g className="chartHoverLayer">
            <line x1={hoverX} y1={PADDING_TOP} x2={hoverX} y2={HEIGHT - PADDING_BOTTOM} className="chartHoverLine" />
            <circle cx={hoverX} cy={hoverYA} r={4} className="chartPointA" />
            <circle cx={hoverX} cy={hoverYB} r={4} className="chartPointB" />
            <g transform={`translate(${Math.min(hoverX + 10, WIDTH - 190)}, ${PADDING_TOP + 8})`}>
              <rect width="178" height="62" rx="8" className="chartTooltipBox" />
              <text x="10" y="18" className="chartTooltipText">{formatTime(hovered.ts)}</text>
              <text x="10" y="38" className="chartTooltipText">{labelA}: {formatRate(hovered.a, displayUnit)}</text>
              <text x="10" y="54" className="chartTooltipText">{labelB}: {formatRate(hovered.b, displayUnit)}</text>
            </g>
          </g>
        ) : null}
      </svg>
      <Group justify="space-between" mt={6} gap="xs">
        <Group gap="md">
          <LegendDot className="chartLegendA" label={labelA} value={formatRate(current?.a, displayUnit)} />
          <LegendDot className="chartLegendB" label={labelB} value={formatRate(current?.b, displayUnit)} />
        </Group>
        <Text size="xs" c="dimmed">{footerText}</Text>
      </Group>
    </Box>
  )
}

function LegendDot({ className, label, value }: { className: string; label: string; value: string }) {
  return (
    <Group gap={6} wrap="nowrap">
      <span className={`chartLegendDot ${className}`} />
      <Text size="xs"><strong>{label}</strong> {value}</Text>
    </Group>
  )
}

function buildPath(points: RealtimePoint[], key: 'a' | 'b', maxValue: number, latestTs: number, windowMs: number): string {
  if (points.length === 0) return ''

  return points.map((point, index) => {
    const x = xForTimestamp(point.ts, latestTs, windowMs)
    const y = yForValue(point[key], maxValue)
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`
  }).join(' ')
}

function xForTimestamp(ts: number, latestTs: number, windowMs: number): number {
  const startTs = latestTs - windowMs
  const ratio = (normalizeTimestamp(ts) - startTs) / windowMs
  return PADDING_LEFT + clamp(ratio, 0, 1) * (WIDTH - PADDING_LEFT - PADDING_RIGHT)
}

function yForValue(value: number, maxValue: number): number {
  return HEIGHT - PADDING_BOTTOM - (value / maxValue) * (HEIGHT - PADDING_TOP - PADDING_BOTTOM)
}

function buildXLabels(latestTs: number, windowMs: number) {
  return [
    { index: 0, x: PADDING_LEFT, text: formatTime(latestTs - windowMs), anchor: 'start' as const },
    { index: 1, x: PADDING_LEFT + (WIDTH - PADDING_LEFT - PADDING_RIGHT) / 2, text: formatTime(latestTs - windowMs / 2), anchor: 'middle' as const },
    { index: 2, x: WIDTH - PADDING_RIGHT, text: '现在', anchor: 'end' as const },
  ]
}

function buildYLabels(maxValue: number, unit: string) {
  return [0, maxValue / 2, maxValue].map((value) => ({
    value,
    y: yForValue(value, maxValue),
    text: formatRate(value, unit),
  }))
}

function formatTime(ts: number): string {
  const date = new Date(normalizeTimestamp(ts))
  return date.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function normalizeTimestamp(ts: number): number {
  return ts > 1_000_000_000_000 ? ts : ts * 1000
}

function nearestPointIndex(points: RealtimePoint[], svgX: number, latestTs: number, windowMs: number): number {
  let nearestIndex = 0
  let nearestDistance = Number.POSITIVE_INFINITY

  points.forEach((point, index) => {
    const distance = Math.abs(xForTimestamp(point.ts, latestTs, windowMs) - svgX)
    if (distance < nearestDistance) {
      nearestDistance = distance
      nearestIndex = index
    }
  })

  return nearestIndex
}

function formatDuration(ms: number): string {
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds} 秒`
  const minutes = Math.round(seconds / 60)
  return `${minutes} 分钟`
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value))
}

function resolveDisplayUnit(maxValue: number, unit?: string): string {
  if (unit) return unit.replace('/s', '')
  if (maxValue >= 1024 ** 3) return 'GB'
  if (maxValue >= 1024 ** 2) return 'MB'
  if (maxValue >= 1024) return 'KB'
  return 'B'
}

function formatRate(value?: number, unit?: string): string {
  return `${formatBytes(value, unit)}/s`
}

function formatBytes(bytes?: number, unit?: string): string {
  const value = bytes || 0
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = unit ? units.indexOf(unit.replace('/s', '')) : 0
  let next = value

  if (index < 0) index = 0
  if (!unit) {
    while (next >= 1024 && index < units.length - 1) {
      next /= 1024
      index += 1
    }
  } else if (index > 0) {
    next = value / Math.pow(1024, index)
  }

  return `${next.toFixed(decimalPlaces(next, index))} ${units[index]}`
}

function decimalPlaces(value: number, unitIndex: number): number {
  if (unitIndex === 0 || value >= 100) return 0
  if (value >= 10) return 1
  return 2
}
