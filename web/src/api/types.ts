// === Task runtime state (managedTask state + DB persistence) ===
// Go: store.TaskState struct
export interface TaskState {
  task_id: string
  name: string
  kind: string
  enabled: boolean
  status: string
  labels: Record<string, string>
  last_run_at: string | null  // RFC3339 time or null
  next_run_at: string | null
  last_run_status: string
  last_check_status: string
  last_error: string
  last_duration_ms: number
  last_reload_error: string
  last_sample_seed: number
  last_sample_count: number
  last_mismatch_count: number
  source_path: string
  updated_at: string  // RFC3339 time
}

// === Run record (one execution of a task) ===
// Go: store.RunRecord struct
export interface RunRecord {
  run_id: string
  task_id: string
  task_kind: string
  trigger_type: string
  run_status: string   // "success" | "failed" | "timeout"
  check_status: string // "pass" | "fail" | "unknown"
  started_at: string
  ended_at: string
  duration_ms: number
  error_message: string
  summary?: Record<string, unknown>
  payload?: unknown
  artifact_refs?: ArtifactRef[]
  findings?: Finding[]
  stdout?: string
  stderr?: string
  labels?: Record<string, string>
}

export interface RunListItem {
  run_id: string
  task_id: string
  task_name?: string
  task_kind: string
  trigger_type: string
  run_status: string
  check_status: string
  started_at: string
  ended_at: string
  duration_ms: number
  error_message?: string
  summary?: Record<string, unknown>
  has_payload: boolean
  artifact_count: number
  finding_count: number
  labels?: Record<string, string>
}

export interface RunStat {
  run_id: string
  started_at: string
  duration_ms: number
  run_status: string
}

export interface PaginatedRuns {
  records: RunListItem[]
  total: number
}

// === Artifact reference (pointer to MinIO/S3 object) ===
// Go: store.ArtifactRef struct
export interface ArtifactRef {
  artifact_id: string
  kind: string
  storage_kind: string
  uri: string
  content_type: string
  size_bytes: number
  sha256: string
  preview_text: string
}

// Go handler returns artifact metadata wrapped with a presigned URL.
export interface ArtifactDetailResponse {
  artifact: ArtifactRef
  download_url?: string
}

// === Finding (issue detected during task execution) ===
// Go: store.Finding struct
export interface Finding {
  finding_id: string
  run_id: string
  task_id: string
  sample_id: string
  reason: string
  data: Record<string, unknown>
}

// === AI analysis record ===
// Go: store.AIAnalysisRecord struct
export interface AIAnalysisRecord {
  id: number
  run_id: string
  task_id: string
  analysis_type: string
  model: string
  prompt: string
  response: string
  tokens_in: number
  tokens_out: number
  duration_ms: number
  status: string
  error_message: string
  created_at: string
}

// === API error response ===
export interface APIError {
  error: string
  errors?: string[]
}

// === Health check response ===
export interface HealthResponse {
  status: string
}

// === Task definition (DB-backed configuration) ===
export interface TaskDefinition {
  task_id: string
  name: string
  kind: string
  enabled: boolean
  interval: string        // e.g. "30s", empty string if not set
  cron: string            // e.g. "0 */6 * * *"
  timeout: string
  labels: Record<string, string>
  params: Record<string, unknown>  // driver-specific, heterogeneous
  trigger: string         // "scheduled" | "manual" | "on_run"
  watch_task_id: string
  watch_condition: string
  pipeline_id?: string | null
  dependencies?: TaskDependency[]
  trace?: TracePolicy
  alert?: AlertPolicy
  created_at: string
  updated_at: string
}

export interface TaskDependency {
  id: string
  upstream_task_id: string
  downstream_task_id: string
  condition: string
  source_key?: string
  params?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

// === Trace policy (nested in TaskDefinition) ===
export interface TracePolicy {
  level: string
  retain_days: number
  mask_fields: string[]
}

// === Sink entry ===
export interface SinkEntry {
  name: string
  kind: 'postgres' | 'webhook'
  url?: string
  timeout?: string
}

// === Global settings ===
export interface GlobalSettings {
  sinks: SinkEntry[]
  max_payload_bytes: number
  default_retain_days: number
}

export interface SettingsResponse {
  settings: GlobalSettings
  applied: boolean
  warnings: string[]
}

export interface PlatformConfigSummary {
  mode: string
  applied: boolean
  warnings: string[]
  server: { addr: string }
  task: { config_dir: string }
  state: { backend: string }
  artifact_store: {
    kind: string
    provider: string
    bucket: string
    endpoint: string
    region: string
    base_path: string
    presign_ttl: string
    force_path_style: boolean
    use_ssl: boolean
    access_key?: string
    secret_key?: string
    status: string
    error?: string
  }
  ai: {
    enabled: boolean
    endpoint: string
    model: string
    timeout: string
    max_tokens: number
    temperature: number
    plugin_dir: string
    status: string
    error?: string
  }
}

// === Alert policy (nested in TaskDefinition) ===
export interface AlertPolicy {
  consecutive_failures: number
  channels: string[]
  recover_notify: boolean
}

// === Merged task detail (definition + runtime) ===
export interface TaskDetailResponse {
  task_id: string
  name: string
  kind: string
  enabled: boolean
  status: string
  definition: TaskDefinition
  runtime: TaskState
}

export interface TaskRuntimeView {
  status: string
  last_run_status?: string
  last_check_status?: string
  last_run_at?: string | null
  next_run_at?: string | null
  last_duration_ms?: number
  last_error?: string
  consecutive_failures: number
}

export interface TaskDependencyView {
  upstream_task_id?: string
  downstream_count: number
  upstream_count: number
  pipeline_id?: string
  watch_condition?: string
  dependency_status?: string
}

export interface TaskView extends TaskState {
  config_status: 'valid' | 'load_error' | 'missing_runtime'
  load_error?: string
  runtime: TaskRuntimeView
  dependency: TaskDependencyView
  definition?: TaskDefinition
  dependencies?: TaskDependency[]
}

export interface DashboardCounts {
  total: number
  enabled: number
  failed: number
  check_failed: number
  load_failed: number
  stale: number
  disabled: number
}

export interface DashboardHealth {
  status: string
  label: string
  detail: string
}

export interface LabelAggregate {
  key: string
  value: string
  total: number
  abnormal: number
}

export interface DashboardSummary {
  counts: DashboardCounts
  health: DashboardHealth
  anomalies: TaskView[]
  recent_runs: RunListItem[]
  label_groups: LabelAggregate[]
  generated_at: string
  refresh_after: string
}

export interface TaskGraphNode {
  task_id: string
  name: string
  kind: string
  enabled: boolean
  labels: Record<string, string>
  pipeline_id?: string
  config_status: string
  runtime: TaskRuntimeView
}

export interface TaskGraphEdge {
  id: string
  upstream_task_id: string
  downstream_task_id: string
  condition?: string
  source_key?: string
  params?: Record<string, unknown>
  valid: boolean
  error?: string
  legacy?: boolean
}

export interface TaskGraph {
  nodes: TaskGraphNode[]
  edges: TaskGraphEdge[]
}

export interface BatchTaskResult {
  task_id: string
  ok: boolean
  error?: string
  run?: RunRecord
}

export interface BatchTaskResponse {
  action: string
  results: BatchTaskResult[]
}

export interface TaskValidationResponse {
  valid: boolean
  errors: string[]
  normalized?: unknown
}

// === Reload/enable/disable response ===
export interface ActionResponse {
  status: string
}

export interface Pipeline {
  id: string
  name: string
  description: string
  created_at: string
  updated_at: string
}

export interface SampleResponse {
  available: boolean
  task_id?: string
  run_id?: string
  source?: string
  reason?: string
  message?: string
  data?: unknown
  display_data?: unknown
  jq_prefix?: string
  jq_result?: unknown
}
