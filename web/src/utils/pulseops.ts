import type { TaskDefinition, TaskState, RunRecord, AIAnalysisRecord } from '../api/types'

export const AUTO_REFRESH_MS = 15_000

export const KIND_LABELS: Record<string, string> = {
  http_check: 'HTTP 检查',
  tcp_check: 'TCP 检查',
  script_exec: '脚本执行',
  process_check: '进程检查',
  scenario_check: '场景巡检',
  ai_analyze: 'AI 分析',
  data_process: '数据处理',
}

export const KIND_COLORS: Record<string, string> = {
  http_check: 'blue',
  tcp_check: 'cyan',
  script_exec: 'orange',
  process_check: 'geekblue',
  scenario_check: 'purple',
  ai_analyze: 'magenta',
  data_process: 'gold',
}

export const RUN_STATUS_LABELS: Record<string, string> = {
  success: '成功',
  failed: '失败',
  timeout: '超时',
  running: '运行中',
}

export const CHECK_STATUS_LABELS: Record<string, string> = {
  pass: '通过',
  fail: '未通过',
  unknown: '未知',
}

export const TASK_STATUS_LABELS: Record<string, string> = {
  running: '运行中',
  loaded: '已加载',
  disabled: '已禁用',
  unloaded: '未加载',
  error: '异常',
}

export type TaskSeverity = 'critical' | 'warning' | 'normal' | 'disabled'

export interface TaskView extends TaskState {
  definition?: TaskDefinition
  config_status: 'valid' | 'load_error' | 'missing_runtime'
  anomaly_type?: string
  anomaly_reason?: string
  severity: TaskSeverity
}

export interface TaskSummary {
  total: number
  enabled: number
  failed: number
  checkFailed: number
  loadFailed: number
  stale: number
  disabled: number
}

export interface LabelAggregate {
  key: string
  value: string
  total: number
  abnormal: number
}

export function emptyTaskState(def: TaskDefinition): TaskState {
  return {
    task_id: def.task_id,
    name: def.name || def.task_id,
    kind: def.kind,
    enabled: def.enabled,
    status: 'unloaded',
    labels: def.labels || {},
    last_run_at: null,
    next_run_at: null,
    last_run_status: '',
    last_check_status: '',
    last_error: '',
    last_duration_ms: 0,
    last_reload_error: '',
    last_sample_seed: 0,
    last_sample_count: 0,
    last_mismatch_count: 0,
    source_path: '',
    updated_at: def.updated_at || '',
  }
}

export function mergeTaskViews(defs: TaskDefinition[], states: TaskState[]): TaskView[] {
  const stateMap = new Map(states.map((state) => [state.task_id, state]))
  const defMap = new Map(defs.map((def) => [def.task_id, def]))

  const views = defs.map((def) => toTaskView(stateMap.get(def.task_id) || emptyTaskState(def), def))

  for (const state of states) {
    if (!defMap.has(state.task_id)) {
      views.push(toTaskView(state))
    }
  }

  return views.sort((a, b) => {
    const severityOrder: Record<TaskSeverity, number> = { critical: 0, warning: 1, normal: 2, disabled: 3 }
    const diff = severityOrder[a.severity] - severityOrder[b.severity]
    if (diff !== 0) return diff
    return latestTime(b) - latestTime(a)
  })
}

export function toTaskView(state: TaskState, definition?: TaskDefinition): TaskView {
  const view: TaskView = {
    ...state,
    labels: state.labels || definition?.labels || {},
    definition,
    config_status: state.last_reload_error
      ? 'load_error'
      : state.status === 'unloaded'
        ? 'missing_runtime'
        : 'valid',
    severity: 'normal',
  }

  if (!view.enabled || view.status === 'disabled') {
    view.severity = 'disabled'
    return view
  }

  if (view.last_reload_error) {
    view.severity = 'critical'
    view.anomaly_type = '配置加载错误'
    view.anomaly_reason = view.last_reload_error
    return view
  }

  if (view.last_run_status === 'failed') {
    view.severity = 'critical'
    view.anomaly_type = '运行失败'
    view.anomaly_reason = view.last_error || '最近一次运行失败'
    return view
  }

  if (view.last_run_status === 'timeout') {
    view.severity = 'critical'
    view.anomaly_type = '运行超时'
    view.anomaly_reason = view.last_error || '最近一次运行超时'
    return view
  }

  if (view.last_check_status === 'fail') {
    view.severity = 'critical'
    view.anomaly_type = '检查未通过'
    view.anomaly_reason = view.last_error || '任务返回检查失败'
    return view
  }

  if (view.status === 'unloaded') {
    view.severity = 'warning'
    view.anomaly_type = '未加载'
    view.anomaly_reason = '任务定义存在，但运行态未加载'
    return view
  }

  if (isStaleTask(view)) {
    view.severity = 'warning'
    view.anomaly_type = '长时间未运行'
    view.anomaly_reason = '最近 24 小时内没有运行记录'
    return view
  }

  return view
}

export function summarizeTasks(tasks: TaskView[]): TaskSummary {
  return {
    total: tasks.length,
    enabled: tasks.filter((task) => task.enabled).length,
    failed: tasks.filter((task) => task.last_run_status === 'failed' || task.last_run_status === 'timeout').length,
    checkFailed: tasks.filter((task) => task.last_check_status === 'fail').length,
    loadFailed: tasks.filter((task) => Boolean(task.last_reload_error)).length,
    stale: tasks.filter((task) => task.enabled && isStaleTask(task)).length,
    disabled: tasks.filter((task) => !task.enabled || task.status === 'disabled').length,
  }
}

export function aggregateLabels(tasks: TaskView[], keys = ['env', 'service', 'kind']): LabelAggregate[] {
  const map = new Map<string, LabelAggregate>()
  for (const task of tasks) {
    for (const key of keys) {
      const value = key === 'kind' ? task.kind : task.labels?.[key]
      if (!value) continue
      const mapKey = `${key}:${value}`
      const entry = map.get(mapKey) || { key, value, total: 0, abnormal: 0 }
      entry.total += 1
      if (task.severity === 'critical' || task.severity === 'warning') entry.abnormal += 1
      map.set(mapKey, entry)
    }
  }
  return Array.from(map.values()).sort((a, b) => b.abnormal - a.abnormal || b.total - a.total)
}

export function isStaleTask(task: TaskState): boolean {
  if (!task.enabled || !task.last_run_at) return false
  const t = new Date(task.last_run_at).getTime()
  if (!Number.isFinite(t) || t <= 0) return false
  return Date.now() - t > 24 * 60 * 60 * 1000
}

export function latestTime(task: TaskState): number {
  return Math.max(
    parseTime(task.last_run_at),
    parseTime(task.updated_at),
    parseTime(task.next_run_at),
  )
}

export function parseTime(value: string | null | undefined): number {
  if (!value) return 0
  const time = new Date(value).getTime()
  return Number.isFinite(time) ? time : 0
}

export function formatTime(value: string | null | undefined): string {
  if (!value) return '—'
  const time = new Date(value)
  if (!Number.isFinite(time.getTime()) || time.getFullYear() < 2000) return '—'
  return time.toLocaleString()
}

export function formatRelativeTime(value: string | null | undefined): string {
  const time = parseTime(value)
  if (!time) return '—'
  const diff = Date.now() - time
  const abs = Math.abs(diff)
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour
  if (abs < minute) return diff >= 0 ? '刚刚' : '即将'
  if (abs < hour) return `${Math.round(abs / minute)} 分钟${diff >= 0 ? '前' : '后'}`
  if (abs < day) return `${Math.round(abs / hour)} 小时${diff >= 0 ? '前' : '后'}`
  return `${Math.round(abs / day)} 天${diff >= 0 ? '前' : '后'}`
}

export function formatDuration(ms: number | undefined): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`
}

export function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB`
  return `${(kb / 1024).toFixed(1)} MB`
}

export function shortID(id: string | undefined, length = 12): string {
  if (!id) return '—'
  return id.length > length ? `${id.slice(0, length)}...` : id
}

export function runIsAbnormal(run: RunRecord): boolean {
  return run.run_status === 'failed' || run.run_status === 'timeout' || run.check_status === 'fail' || Boolean(run.error_message)
}

export function runDiagnosis(run: RunRecord, ai?: AIAnalysisRecord | null): string {
  if (run.error_message) return run.error_message
  if (run.findings && run.findings.length > 0) return `${run.findings.length} 条检查发现`
  if (ai?.status === 'success' && ai.response) return firstLine(ai.response)
  if (run.check_status === 'fail') return '检查未通过'
  return '未发现错误信息'
}

export function firstLine(value: string, maxLength = 180): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (!compact) return '—'
  return compact.length > maxLength ? `${compact.slice(0, maxLength)}...` : compact
}

export function safeJson(value: unknown): string {
  if (value === undefined || value === null || value === '') return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function collectDownstream(defs: TaskDefinition[], taskId: string): TaskDefinition[] {
  return defs.filter((def) => def.trigger === 'on_run' && def.watch_task_id === taskId)
}

export function collectUpstream(defs: TaskDefinition[], task: TaskDefinition | undefined): TaskDefinition[] {
  if (!task?.watch_task_id) return []
  return defs.filter((def) => def.task_id === task.watch_task_id)
}

export function statusColorForTask(task: TaskView | TaskState): string {
  if (!task.enabled || task.status === 'disabled') return 'default'
  if (task.last_reload_error || task.last_run_status === 'failed' || task.last_check_status === 'fail') return 'red'
  if (task.last_run_status === 'timeout' || task.status === 'unloaded') return 'orange'
  if (task.last_run_status === 'success' || task.last_check_status === 'pass') return 'green'
  if (task.status === 'running') return 'blue'
  return 'default'
}

export function runStatusColor(status: string): string {
  if (status === 'success') return 'green'
  if (status === 'failed') return 'red'
  if (status === 'timeout') return 'orange'
  if (status === 'running') return 'blue'
  return 'default'
}

export function checkStatusColor(status: string): string {
  if (status === 'pass') return 'green'
  if (status === 'fail') return 'red'
  return 'default'
}
