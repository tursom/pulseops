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
  plugin_generation_id?: string
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
  plugin_config_versions?: Record<string, unknown>
  plugin_asset_versions?: Record<string, unknown>
  plugin_task_overrides?: Record<string, unknown>
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
  plugins: {
    enabled: boolean
    dir: string
    strict: boolean
    allow_process: boolean
    allow_http: boolean
    allow_grpc: boolean
    default_timeout: string
    max_output_bytes: number
    max_concurrent_calls: number
    generation_retention: string
    allowed_permissions: string[]
    env_allowlist: string[]
    status: string
    error?: string
  }
}

export interface PluginSchemaField {
  type: string
  required?: boolean
  description?: string
}

export interface PluginConfigOption {
  value: unknown
  label?: string
}

export interface PluginConfigValidation {
  min?: number
  max?: number
  step?: number
  min_len?: number
  max_len?: number
  pattern?: string
}

export interface PluginConfigCondition {
  field: string
  op: 'eq' | 'ne' | 'in' | 'not_in' | 'exists' | 'empty' | string
  value?: unknown
}

export interface PluginConfigUI {
  group?: string
  label?: string
  widget?: string
  order?: number
  placeholder?: string
  help?: string
  advanced?: boolean
  collapsed?: boolean
  visible_when?: PluginConfigCondition
}

export interface PluginConfigField {
  type: string
  class?: string
  required?: boolean
  default?: unknown
  overridable?: boolean
  description?: string
  options?: PluginConfigOption[]
  items?: PluginConfigField
  asset_kind?: string
  asset_scope?: 'plugin_shared' | 'capability_shared' | 'config_instance' | string
  accept?: string[]
  validation?: PluginConfigValidation
  ui?: PluginConfigUI
}

export interface PluginConfigClass {
  title?: string
  description?: string
  fields?: Record<string, PluginConfigField>
}

export interface PluginConfigSchema {
  title?: string
  description?: string
  validate_action?: string
  allow_plugin_config_ref?: boolean
  fields?: Record<string, PluginConfigField>
}

export interface PluginConfigSchemaResponse {
  plugin_id: string
  plugin_version?: string
  capability_id?: string
  capability_type?: string
  capability_name?: string
  config_classes?: Record<string, PluginConfigClass>
  config?: PluginConfigSchema
}

export interface PluginCapability {
  id: string
  type: string
  name: string
  title?: string
  description?: string
  plugin_id: string
  plugin_name: string
  plugin_version: string
  kind?: string
  protocol?: string
  runtime?: string
  entrypoint?: string
  endpoint?: string
  path?: string
  release_path?: string
  status: string
  enabled: boolean
  official: boolean
  bundled: boolean
  permissions?: string[]
  defaults?: Record<string, unknown>
  params?: Record<string, unknown>
  schema?: Record<string, PluginSchemaField>
  config_classes?: Record<string, PluginConfigClass>
  config?: PluginConfigSchema
}

export interface PluginManifest {
  schema_version: string
  id: string
  name: string
  version: string
  description?: string
  author?: string
  homepage?: string
  enabled: boolean
  permissions?: string[]
}

export interface PluginPackage {
  id: string
  name: string
  description?: string
  author?: string
  homepage?: string
  official: boolean
  bundled: boolean
  status: string
  last_error?: string
  created_at: string
  updated_at: string
}

export interface PluginRelease {
  plugin_id: string
  version: string
  schema_version: string
  manifest: PluginManifest
  path?: string
  status: string
  checksum?: string
  validation_error?: string
  official: boolean
  bundled: boolean
  created_at: string
  updated_at: string
  validated_at?: string
  activated_at?: string
}

export interface PluginView {
  package: PluginPackage
  active_version?: string
  release?: PluginRelease
  releases?: PluginRelease[]
  capabilities: PluginCapability[]
  permissions?: string[]
}

export interface PluginCatalog {
  generated_at: string
  plugin_dir: string
  status: string
  active_generation_id?: string
  stats: {
    total: number
    enabled: number
    disabled: number
    errors: number
    capabilities: number
  }
  plugins: PluginView[]
  errors?: string[]
}

export interface PluginConfigInstance {
  id: string
  plugin_id: string
  capability_id?: string
  capability_type?: string
  capability_name?: string
  scope: string
  title?: string
  status: string
  active_version?: number
  created_at: string
  updated_at: string
}

export interface PluginConfigVersion {
  instance_id: string
  version: number
  status: string
  values?: Record<string, unknown>
  validation_error?: string
  created_at: string
  updated_at: string
  validated_at?: string
  activated_at?: string
  retired_at?: string
}

export interface PluginConfigInstanceDetail {
  instance: PluginConfigInstance
  versions: PluginConfigVersion[]
  active?: PluginConfigVersion
}

export interface PluginConfigValidationResponse {
  valid: boolean
  errors?: string[]
  version: PluginConfigVersion
}

export interface PluginConfigEvent {
  id: number
  resource_type: string
  resource_id: string
  plugin_id?: string
  action: string
  status: string
  message?: string
  created_at: string
}

export interface PluginAsset {
  id: string
  plugin_id: string
  capability_id?: string
  config_instance_id?: string
  scope: 'plugin_shared' | 'capability_shared' | 'config_instance' | string
  kind: string
  title?: string
  status: string
  active_version?: number
  created_at: string
  updated_at: string
}

export interface PluginAssetVersion {
  asset_id: string
  version: number
  status: string
  filename?: string
  content_type?: string
  storage_uri?: string
  size_bytes?: number
  checksum?: string
  validation_error?: string
  created_at: string
  updated_at: string
  validated_at?: string
  activated_at?: string
  retired_at?: string
}

export interface PluginSecret {
  id: string
  plugin_id: string
  scope?: string
  title?: string
  masked: string
  status: string
  created_at: string
  updated_at: string
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
