import { useMemo } from 'react'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts'
import type { RunRecord } from '../api/types'

// === Color constants ===
const LINE_COLOR = '#1677ff'

const STATUS_COLORS: Record<string, string> = {
  success: '#52c41a',
  failed: '#ff4d4f',
  timeout: '#faad14',
  running: '#1677ff',
}

const STATUS_LABELS: Record<string, string> = {
  success: '成功',
  failed: '失败',
  timeout: '超时',
  running: '运行中',
}

// === Formatting helpers ===
function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatTooltipTime(ts: number): string {
  const d = new Date(ts)
  const MM = String(d.getMonth() + 1).padStart(2, '0')
  const DD = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${MM}-${DD} ${hh}:${mm}`
}

// === Chart data point ===
interface ChartPoint {
  time: number
  duration: number
  status: string
  runId: string
  label: string
}

// === Props ===
interface DurationChartProps {
  runs: RunRecord[]
}

// === Custom tooltip ===
function DurationTooltip({
  active,
  payload,
}: {
  active?: boolean
  payload?: { payload: ChartPoint }[]
}) {
  if (!active || !payload || payload.length === 0) return null

  const data = payload[0].payload
  const statusColor = STATUS_COLORS[data.status] || LINE_COLOR
  const statusLabel = STATUS_LABELS[data.status] || data.status

  return (
    <div
      style={{
        background: '#fff',
        border: '1px solid #e8e8e8',
        borderRadius: 6,
        padding: '8px 12px',
        boxShadow: '0 2px 8px rgba(0,0,0,0.08)',
        fontSize: 13,
      }}
    >
      <div style={{ color: '#888', fontSize: 12, marginBottom: 4 }}>
        {formatTooltipTime(data.time)}
      </div>
      <div style={{ marginBottom: 2 }}>
        <span style={{ color: '#666' }}>耗时：</span>
        <span style={{ fontWeight: 600, color: '#333' }}>
          {formatDuration(data.duration)}
        </span>
      </div>
      <div style={{ marginBottom: 2 }}>
        <span style={{ color: '#666' }}>状态：</span>
        <span style={{ color: statusColor, fontWeight: 500 }}>
          {statusLabel}
        </span>
      </div>
      <div>
        <span
          style={{
            color: '#aaa',
            fontSize: 11,
            fontFamily:
              'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
          }}
        >
          {data.runId.substring(0, 12)}
        </span>
      </div>
    </div>
  )
}

// === Custom dot: color-coded by run status ===
function renderStatusDot(props: {
  cx?: number
  cy?: number
  payload?: { status: string }
}) {
  const { cx, cy, payload } = props
  if (cx == null || cy == null || !payload) return null

  const color = STATUS_COLORS[payload.status] || LINE_COLOR
  return <circle cx={cx} cy={cy} r={4} fill={color} stroke="none" />
}

// === Custom active dot ===
function renderStatusActiveDot(props: {
  cx?: number
  cy?: number
  payload?: { status: string }
}) {
  const { cx, cy, payload } = props
  if (cx == null || cy == null || !payload) return null

  const color = STATUS_COLORS[payload.status] || LINE_COLOR
  return (
    <circle cx={cx} cy={cy} r={6} fill={color} stroke="#fff" strokeWidth={2} />
  )
}

// === Custom legend: status color mapping ===
function StatusLegend({ statuses }: { statuses: string[] }) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        gap: 16,
        padding: '6px 0',
      }}
    >
      {statuses.map((s) => (
        <div
          key={s}
          style={{ display: 'flex', alignItems: 'center', gap: 5 }}
        >
          <span
            style={{
              width: 10,
              height: 10,
              borderRadius: '50%',
              backgroundColor: STATUS_COLORS[s] || LINE_COLOR,
              display: 'inline-block',
              flexShrink: 0,
            }}
          />
          <span style={{ fontSize: 12, color: '#666' }}>
            {STATUS_LABELS[s] || s}
          </span>
        </div>
      ))}
    </div>
  )
}

// === Main component ===
export default function DurationChart({ runs }: DurationChartProps) {
  // Determine whether all runs fall on the same calendar day
  const isSameDay = useMemo(() => {
    if (runs.length < 2) return true
    const dates = runs.map((r) => new Date(r.started_at).toDateString())
    return dates[0] === dates[dates.length - 1]
  }, [runs])

  // Collect unique statuses present in the data
  const statuses = useMemo(() => {
    const seen = new Set(runs.map((r) => r.run_status))
    return [...seen]
  }, [runs])

  // Build chart data (reversed → left-to-right chronological flow)
  const chartData = useMemo<ChartPoint[]>(() => {
    return [...runs].reverse().map((r) => {
      const d = new Date(r.started_at)
      return {
        time: d.getTime(),
        duration: r.duration_ms,
        status: r.run_status,
        runId: r.run_id,
        label: d.toISOString(),
      }
    })
  }, [runs])

  // Insufficient data guard
  if (runs.length < 2) {
    return (
      <div
        style={{
          textAlign: 'center',
          color: '#999',
          fontSize: 14,
          padding: '80px 0',
        }}
      >
        数据不足以生成趋势图
      </div>
    )
  }

  return (
    <ResponsiveContainer width="100%" height={300}>
      <LineChart
        data={chartData}
        margin={{ top: 8, right: 16, bottom: 8, left: 0 }}
      >
        <CartesianGrid strokeDasharray="3 3" vertical={false} />
        <XAxis
          dataKey="label"
          tickFormatter={(value: string) => {
            const d = new Date(value)
            const hh = String(d.getHours()).padStart(2, '0')
            const mm = String(d.getMinutes()).padStart(2, '0')
            if (isSameDay) return `${hh}:${mm}`
            const MM = String(d.getMonth() + 1).padStart(2, '0')
            const DD = String(d.getDate()).padStart(2, '0')
            return `${MM}-${DD} ${hh}:${mm}`
          }}
          tick={{ fontSize: 12, fill: '#888' }}
        />
        <YAxis
          dataKey="duration"
          tickFormatter={formatDuration}
          tick={{ fontSize: 12, fill: '#888' }}
          label={{
            value: '耗时',
            angle: -90,
            position: 'insideLeft',
            offset: 8,
            style: { textAnchor: 'middle', fontSize: 12, fill: '#888' },
          }}
        />
        <Tooltip content={<DurationTooltip />} />
        {statuses.length > 1 && (
          <Legend
            content={() => <StatusLegend statuses={statuses} />}
          />
        )}
        <Line
          type="monotone"
          dataKey="duration"
          stroke={LINE_COLOR}
          strokeWidth={2}
          dot={renderStatusDot}
          activeDot={renderStatusActiveDot}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}
