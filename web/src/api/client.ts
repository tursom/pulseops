import type {
  TaskDefinition,
  Pipeline,
  RunRecord,
  AIAnalysisRecord,
  ArtifactRef,
  ArtifactDetailResponse,
  HealthResponse,
  ActionResponse,
  APIError,
  GlobalSettings,
  SampleResponse,
  PaginatedRuns,
  RunStat,
  AlertPolicy,
  TaskView,
  DashboardSummary,
  RunListItem,
  TaskGraph,
  TaskGraphEdge,
  TaskGraphNode,
  TaskDependency,
  BatchTaskResponse,
  BatchTaskResult,
  SettingsResponse,
  TaskValidationResponse,
  PlatformConfigSummary,
  TracePolicy,
  PluginCatalog,
  PluginCapability,
  PluginAsset,
  PluginAssetVersion,
  PluginConfigEvent,
  PluginConfigInstance,
  PluginConfigInstanceDetail,
  PluginConfigSchemaResponse,
  PluginConfigValidationResponse,
  PluginConfigVersion,
  PluginRelease,
  PluginSecret,
  PluginView,
} from './types'

export class PulseOpsAPIError extends Error {
  readonly errors: string[]
  readonly body: unknown

  constructor(message: string, errors: string[] = [], body?: unknown) {
    super(message)
    this.name = 'PulseOpsAPIError'
    this.errors = errors
    this.body = body
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function listOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : []
}

function stringRecordOrEmpty(value: unknown): Record<string, string> {
  return isRecord(value) ? value as Record<string, string> : {}
}

function recordOrEmpty(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function normalizeTracePolicy(trace: TracePolicy | null | undefined): TracePolicy | undefined {
  if (!trace) return undefined
  return {
    ...trace,
    mask_fields: listOrEmpty(trace.mask_fields),
  }
}

function normalizeAlertPolicy(alert: AlertPolicy | null | undefined): AlertPolicy | undefined {
  if (!alert) return undefined
  return {
    ...alert,
    channels: listOrEmpty(alert.channels),
  }
}

function normalizeTaskDefinition(def: TaskDefinition): TaskDefinition {
  return {
    ...def,
    labels: stringRecordOrEmpty(def.labels),
    params: recordOrEmpty(def.params),
    dependencies: listOrEmpty(def.dependencies),
    trace: normalizeTracePolicy(def.trace),
    alert: normalizeAlertPolicy(def.alert),
  }
}

function normalizeTaskView(task: TaskView): TaskView {
  return {
    ...task,
    labels: stringRecordOrEmpty(task.labels),
    dependencies: listOrEmpty(task.dependencies),
    definition: task.definition ? normalizeTaskDefinition(task.definition) : undefined,
  }
}

function normalizeRunRecord(run: RunRecord): RunRecord {
  return {
    ...run,
    labels: stringRecordOrEmpty(run.labels),
    artifact_refs: listOrEmpty(run.artifact_refs),
    plugin_config_versions: recordOrEmpty(run.plugin_config_versions),
    plugin_asset_versions: recordOrEmpty(run.plugin_asset_versions),
    plugin_task_overrides: recordOrEmpty(run.plugin_task_overrides),
    findings: listOrEmpty(run.findings),
  }
}

function normalizeRunListItem(run: RunListItem): RunListItem {
  return {
    ...run,
    labels: stringRecordOrEmpty(run.labels),
  }
}

function normalizeTaskGraphNode(node: TaskGraphNode): TaskGraphNode {
  return {
    ...node,
    labels: stringRecordOrEmpty(node.labels),
  }
}

function normalizeTaskGraphEdge(edge: TaskGraphEdge): TaskGraphEdge {
  return {
    ...edge,
    params: edge.params == null ? undefined : recordOrEmpty(edge.params),
  }
}

function normalizeBatchTaskResult(result: BatchTaskResult): BatchTaskResult {
  return {
    ...result,
    run: result.run ? normalizeRunRecord(result.run) : undefined,
  }
}

function normalizePaginatedRuns(data: PaginatedRuns | null): PaginatedRuns {
  return {
    records: listOrEmpty(data?.records).map(normalizeRunListItem),
    total: typeof data?.total === 'number' ? data.total : 0,
  }
}

function normalizeValidationResponse(data: TaskValidationResponse | null): TaskValidationResponse {
  return {
    valid: Boolean(data?.valid),
    ...data,
    errors: listOrEmpty(data?.errors),
  }
}

function normalizeSettingsResponse(data: SettingsResponse | null): SettingsResponse {
  const settings = data?.settings || { sinks: [], max_payload_bytes: 0, default_retain_days: 0 }
  return {
    ...data,
    applied: Boolean(data?.applied),
    settings: {
      ...settings,
      sinks: listOrEmpty(settings.sinks),
    },
    warnings: listOrEmpty(data?.warnings),
  }
}

function normalizePlatformConfig(data: PlatformConfigSummary): PlatformConfigSummary {
  return {
    ...data,
    warnings: listOrEmpty(data.warnings),
    plugins: {
      ...(data.plugins || {
        enabled: false,
        dir: '',
        strict: false,
        allow_process: false,
        allow_http: false,
        allow_grpc: false,
        default_timeout: '',
        max_output_bytes: 0,
        max_concurrent_calls: 0,
        generation_retention: '',
        allowed_permissions: [],
        env_allowlist: [],
        status: '',
      }),
      allowed_permissions: listOrEmpty(data.plugins?.allowed_permissions),
      env_allowlist: listOrEmpty(data.plugins?.env_allowlist),
    },
  }
}

function normalizePluginView(plugin: PluginView): PluginView {
  return {
    ...plugin,
    capabilities: listOrEmpty(plugin.capabilities),
    permissions: listOrEmpty(plugin.permissions),
    releases: listOrEmpty(plugin.releases),
  }
}

function normalizePluginConfigVersion(version: PluginConfigVersion): PluginConfigVersion {
  return {
    ...version,
    values: recordOrEmpty(version.values),
  }
}

function normalizePluginConfigDetail(data: PluginConfigInstanceDetail): PluginConfigInstanceDetail {
  return {
    ...data,
    versions: listOrEmpty(data.versions).map(normalizePluginConfigVersion),
    active: data.active ? normalizePluginConfigVersion(data.active) : undefined,
  }
}

function normalizePluginCatalog(data: PluginCatalog | null): PluginCatalog {
  const stats = data?.stats || { total: 0, enabled: 0, disabled: 0, errors: 0, capabilities: 0 }
  return {
    ...data,
    generated_at: data?.generated_at || '',
    plugin_dir: data?.plugin_dir || '',
    status: data?.status || 'unknown',
    stats,
    plugins: listOrEmpty(data?.plugins).map(normalizePluginView),
    errors: listOrEmpty(data?.errors),
  }
}

// Generic fetch wrapper
async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    cache: 'no-store',
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch((): APIError => ({ error: res.statusText }))
    const errorBody = isRecord(body) ? body : {}
    const errors = listOrEmpty(errorBody.errors as string[] | null | undefined)
    const errorMessage = typeof errorBody.error === 'string' ? errorBody.error : ''
    throw new PulseOpsAPIError(errorMessage || errors.join('；') || `HTTP ${res.status}`, errors, body)
  }
  return res.json()
}

// GET /api/healthz
export async function fetchHealth(): Promise<HealthResponse> {
  return request<HealthResponse>('/api/healthz')
}

export async function fetchPlatformConfig(): Promise<PlatformConfigSummary> {
  const data = await request<PlatformConfigSummary>('/api/platform-config')
  return normalizePlatformConfig(data)
}

export async function updatePlatformConfig(config: PlatformConfigSummary): Promise<PlatformConfigSummary> {
  const data = await request<PlatformConfigSummary>('/api/platform-config', {
    method: 'PUT',
    body: JSON.stringify(config),
  })
  return normalizePlatformConfig(data)
}

// GET /api/tasks
export async function fetchTasks(): Promise<TaskView[]> {
  const data = await request<TaskView[] | null>('/api/tasks')
  return listOrEmpty(data).map(normalizeTaskView)
}

// GET /api/tasks/{id}
export async function fetchTask(id: string): Promise<TaskView> {
  const data = await request<TaskView>(`/api/tasks/${encodeURIComponent(id)}`)
  return normalizeTaskView(data)
}

export async function fetchDashboardSummary(since = '24h'): Promise<DashboardSummary> {
  const params = new URLSearchParams({ since })
  const data = await request<DashboardSummary | null>(`/api/dashboard/summary?${params.toString()}`)
  const counts = data?.counts || { total: 0, enabled: 0, failed: 0, check_failed: 0, load_failed: 0, stale: 0, disabled: 0 }
  const health = data?.health || { status: 'unknown', label: '未知', detail: '后端未返回工作台健康信息' }
  return {
    ...data,
    counts,
    health,
    anomalies: listOrEmpty(data?.anomalies).map(normalizeTaskView),
    recent_runs: listOrEmpty(data?.recent_runs).map(normalizeRunListItem),
    label_groups: listOrEmpty(data?.label_groups),
    generated_at: data?.generated_at || '',
    refresh_after: data?.refresh_after || '',
  }
}

// GET /api/tasks/{id}/runs
export async function fetchTaskRuns(id: string, limit?: number, since?: string): Promise<RunListItem[]> {
  const data = await fetchTaskRunsPaginated(id, limit, 0, since)
  return data.records
}

// GET /api/tasks/{id}/runs (paginated)
export async function fetchTaskRunsPaginated(
  id: string,
  limit?: number,
  offset?: number,
  since?: string,
): Promise<PaginatedRuns> {
  const params = new URLSearchParams()
  if (limit !== undefined) params.set('limit', String(limit))
  if (offset !== undefined && offset > 0) params.set('offset', String(offset))
  if (since) params.set('since', since)
  const qs = params.toString()
  const data = await request<PaginatedRuns | null>(`/api/tasks/${encodeURIComponent(id)}/runs${qs ? `?${qs}` : ''}`)
  return normalizePaginatedRuns(data)
}

export async function fetchRuns(paramsInput: {
  task_id?: string
  kind?: string
  run_status?: string
  check_status?: string
  since?: string
  limit?: number
  offset?: number
  labels?: Record<string, string>
} = {}): Promise<PaginatedRuns> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(paramsInput)) {
    if (key === 'labels' || value === undefined || value === '') continue
    params.set(key, String(value))
  }
  for (const [key, value] of Object.entries(paramsInput.labels || {})) {
    if (value) params.set(`label.${key}`, value)
  }
  const qs = params.toString()
  const data = await request<PaginatedRuns | null>(`/api/runs${qs ? `?${qs}` : ''}`)
  return normalizePaginatedRuns(data)
}

// GET /api/tasks/{id}/runs/stats
export async function fetchTaskRunStats(id: string, since?: string): Promise<RunStat[]> {
  const params = new URLSearchParams()
  if (since) params.set('since', since)
  const qs = params.toString()
  const data = await request<RunStat[] | null>(`/api/tasks/${encodeURIComponent(id)}/runs/stats${qs ? `?${qs}` : ''}`)
  return listOrEmpty(data)
}

// GET /api/tasks/{id}/runs/{runID}
export async function fetchTaskRun(id: string, runID: string): Promise<RunRecord> {
  const data = await request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}`)
  return normalizeRunRecord(data)
}

// GET /api/tasks/{id}/runs/{runID}/ai
export async function fetchRunAIAnalysis(id: string, runID: string): Promise<AIAnalysisRecord> {
  return request<AIAnalysisRecord>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/ai`)
}

// GET /api/tasks/{id}/ai
export async function fetchTaskAIAnalyses(id: string, limit?: number): Promise<AIAnalysisRecord[]> {
  const params = new URLSearchParams()
  if (limit !== undefined) {
    params.set('limit', String(limit))
  }
  const qs = params.toString()
  const data = await request<AIAnalysisRecord[] | null>(`/api/tasks/${encodeURIComponent(id)}/ai${qs ? `?${qs}` : ''}`)
  return listOrEmpty(data)
}

// GET /api/tasks/{id}/runs/{runID}/artifacts
export async function fetchRunArtifacts(id: string, runID: string): Promise<ArtifactRef[]> {
  const data = await request<ArtifactRef[] | null>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/artifacts`)
  return listOrEmpty(data)
}

// GET /api/artifacts/{artifactID}
export async function fetchArtifactDetail(artifactID: string): Promise<ArtifactDetailResponse> {
  return request<ArtifactDetailResponse>(`/api/artifacts/${encodeURIComponent(artifactID)}`)
}

// GET /api/artifacts/{artifactID}/content
export async function fetchArtifactContent(artifactID: string): Promise<string> {
  const res = await fetch(`/api/artifacts/${encodeURIComponent(artifactID)}/content`, {
    headers: { 'Content-Type': 'application/json' },
    cache: 'no-store',
  })
  if (!res.ok) {
    const body = await res.json().catch((): { error: string } => ({ error: res.statusText }))
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  return res.text()
}

// POST /api/tasks/{id}/run
export async function triggerTaskRun(id: string): Promise<RunRecord> {
  const data = await request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/run`, { method: 'POST' })
  return normalizeRunRecord(data)
}

export async function batchTaskAction(action: 'run' | 'enable' | 'disable' | 'reload', taskIds: string[]): Promise<BatchTaskResponse> {
  const data = await request<BatchTaskResponse | null>('/api/tasks/batch', {
    method: 'POST',
    body: JSON.stringify({ action, task_ids: taskIds }),
  })
  return {
    ...data,
    action: data?.action || action,
    results: listOrEmpty(data?.results).map(normalizeBatchTaskResult),
  }
}

// POST /api/tasks/{id}/runs/{runID}/rerun
export async function retriggerTaskRun(id: string, runID: string): Promise<RunRecord> {
  const data = await request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/rerun`, { method: 'POST' })
  return normalizeRunRecord(data)
}

// POST /api/tasks/{id}/reload
export async function reloadTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/api/tasks/${encodeURIComponent(id)}/reload`, { method: 'POST' })
}

// POST /api/tasks/{id}/enable
export async function enableTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/api/tasks/${encodeURIComponent(id)}/enable`, { method: 'POST' })
}

// POST /api/tasks/{id}/disable
export async function disableTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/api/tasks/${encodeURIComponent(id)}/disable`, { method: 'POST' })
}

// GET /api/task-defs
export async function fetchTaskDefinitions(): Promise<TaskDefinition[]> {
  const data = await request<TaskDefinition[] | null>('/api/task-defs')
  return listOrEmpty(data).map(normalizeTaskDefinition)
}

// GET /api/task-defs/{id}
export async function fetchTaskDefinition(id: string): Promise<TaskDefinition> {
  const data = await request<TaskDefinition>(`/api/task-defs/${encodeURIComponent(id)}`)
  return normalizeTaskDefinition(data)
}

// POST /api/task-defs
export async function createTaskDefinition(def: TaskDefinition): Promise<TaskDefinition> {
  const data = await request<TaskDefinition>('/api/task-defs', {
    method: 'POST',
    body: JSON.stringify(def),
  })
  return normalizeTaskDefinition(data)
}

// PUT /api/task-defs/{id}
export async function updateTaskDefinition(id: string, def: TaskDefinition): Promise<TaskDefinition> {
  const data = await request<TaskDefinition>(`/api/task-defs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(def),
  })
  return normalizeTaskDefinition(data)
}

// DELETE /api/task-defs/{id}
export async function deleteTaskDefinition(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/task-defs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function validateTaskDefinition(def: TaskDefinition): Promise<TaskValidationResponse> {
  const data = await request<TaskValidationResponse>('/api/task-defs/validate', {
    method: 'POST',
    body: JSON.stringify(def),
  })
  return normalizeValidationResponse(data)
}

export async function dryRunTaskDefinition(def: TaskDefinition): Promise<TaskValidationResponse> {
  const data = await request<TaskValidationResponse>('/api/task-defs/dry-run', {
    method: 'POST',
    body: JSON.stringify(def),
  })
  return normalizeValidationResponse(data)
}

export async function testRunTaskDefinition(def: TaskDefinition): Promise<RunRecord> {
  const data = await request<RunRecord>('/api/task-defs/test-run', {
    method: 'POST',
    body: JSON.stringify(def),
  })
  return normalizeRunRecord(data)
}

export async function fetchPipelines(): Promise<Pipeline[]> {
  const data = await request<Pipeline[] | null>('/api/pipelines')
  return listOrEmpty(data)
}

export async function fetchPipeline(id: string): Promise<Pipeline> {
  return request<Pipeline>(`/api/pipelines/${encodeURIComponent(id)}`)
}

export async function createPipeline(p: Pick<Pipeline, 'id' | 'name' | 'description'>): Promise<Pipeline> {
  return request<Pipeline>('/api/pipelines', {
    method: 'POST',
    body: JSON.stringify(p),
  })
}

export async function updatePipeline(id: string, p: Pick<Pipeline, 'name' | 'description'>): Promise<Pipeline> {
  return request<Pipeline>(`/api/pipelines/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(p),
  })
}

export async function deletePipeline(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/pipelines/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function fetchPipelineTasks(pipelineId: string): Promise<TaskDefinition[]> {
  const data = await request<TaskDefinition[] | null>(`/api/pipelines/${encodeURIComponent(pipelineId)}/tasks`)
  return listOrEmpty(data).map(normalizeTaskDefinition)
}

export async function assignTaskToPipeline(pipelineId: string, taskId: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/pipelines/${encodeURIComponent(pipelineId)}/tasks/${encodeURIComponent(taskId)}`, {
    method: 'PUT',
  })
}

export async function unassignTaskFromPipeline(pipelineId: string, taskId: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/pipelines/${encodeURIComponent(pipelineId)}/tasks/${encodeURIComponent(taskId)}`, {
    method: 'DELETE',
  })
}

export async function fetchTaskGraph(pipelineId?: string): Promise<TaskGraph> {
  const params = new URLSearchParams()
  if (pipelineId) params.set('pipeline_id', pipelineId)
  const qs = params.toString()
  const data = await request<TaskGraph | null>(`/api/task-graph${qs ? `?${qs}` : ''}`)
  return {
    ...data,
    nodes: listOrEmpty(data?.nodes).map(normalizeTaskGraphNode),
    edges: listOrEmpty(data?.edges).map(normalizeTaskGraphEdge),
  }
}

export async function upsertTaskDependency(dep: Partial<TaskDependency> & Pick<TaskDependency, 'upstream_task_id' | 'downstream_task_id'>): Promise<TaskDependency> {
  return request<TaskDependency>('/api/task-dependencies', {
    method: 'POST',
    body: JSON.stringify(dep),
  })
}

export async function deleteTaskDependency(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/task-dependencies/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function fetchSettings(): Promise<SettingsResponse> {
  const data = await request<SettingsResponse>('/api/settings')
  return normalizeSettingsResponse(data)
}

export async function updateSettings(settings: GlobalSettings): Promise<SettingsResponse> {
  const data = await request<SettingsResponse>('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
  return normalizeSettingsResponse(data)
}

export async function fetchPluginCatalog(): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>('/api/plugins')
  return normalizePluginCatalog(data)
}

export async function fetchPlugin(id: string): Promise<PluginView> {
  const data = await request<PluginView>(`/api/plugins/${encodeURIComponent(id)}`)
  return normalizePluginView(data)
}

export async function fetchPluginReleases(id: string): Promise<PluginRelease[]> {
  const data = await request<PluginRelease[] | null>(`/api/plugins/${encodeURIComponent(id)}/releases`)
  return listOrEmpty(data)
}

export async function fetchPluginCapabilities(type?: string, kind?: string): Promise<PluginCapability[]> {
  const params = new URLSearchParams()
  if (type) params.set('type', type)
  if (kind) params.set('kind', kind)
  const qs = params.toString()
  const data = await request<PluginCapability[] | null>(`/api/plugin-capabilities${qs ? `?${qs}` : ''}`)
  return listOrEmpty(data)
}

export async function fetchPluginConfigSchema(id: string): Promise<PluginConfigSchemaResponse> {
  return request<PluginConfigSchemaResponse>(`/api/plugins/${encodeURIComponent(id)}/config-schema`)
}

export async function fetchCapabilityConfigSchema(capabilityId: string): Promise<PluginConfigSchemaResponse> {
  return request<PluginConfigSchemaResponse>(`/api/plugin-capabilities/${encodeURIComponent(capabilityId)}/config-schema`)
}

export async function fetchPluginConfigs(params: { plugin_id?: string; capability_id?: string }): Promise<PluginConfigInstance[]> {
  const qs = new URLSearchParams()
  if (params.plugin_id) qs.set('plugin_id', params.plugin_id)
  if (params.capability_id) qs.set('capability_id', params.capability_id)
  const data = await request<PluginConfigInstance[] | null>(`/api/plugin-configs${qs.toString() ? `?${qs.toString()}` : ''}`)
  return listOrEmpty(data)
}

export async function fetchPluginConfigEvents(params: {
  plugin_id?: string
  resource_type?: string
  resource_id?: string
  limit?: number
}): Promise<PluginConfigEvent[]> {
  const qs = new URLSearchParams()
  if (params.plugin_id) qs.set('plugin_id', params.plugin_id)
  if (params.resource_type) qs.set('resource_type', params.resource_type)
  if (params.resource_id) qs.set('resource_id', params.resource_id)
  if (params.limit) qs.set('limit', String(params.limit))
  const data = await request<PluginConfigEvent[] | null>(`/api/plugin-config-events${qs.toString() ? `?${qs.toString()}` : ''}`)
  return listOrEmpty(data)
}

export async function createPluginConfig(input: {
  id: string
  plugin_id: string
  capability_id?: string
  scope?: string
  title?: string
}): Promise<PluginConfigInstance> {
  return request<PluginConfigInstance>('/api/plugin-configs', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function fetchPluginConfig(instanceId: string): Promise<PluginConfigInstanceDetail> {
  const data = await request<PluginConfigInstanceDetail>(`/api/plugin-configs/${encodeURIComponent(instanceId)}`)
  return normalizePluginConfigDetail(data)
}

export async function createPluginConfigVersion(instanceId: string, values?: Record<string, unknown>): Promise<PluginConfigVersion> {
  const data = await request<PluginConfigVersion>(`/api/plugin-configs/${encodeURIComponent(instanceId)}/versions`, {
    method: 'POST',
    body: JSON.stringify({ values: values || {} }),
  })
  return normalizePluginConfigVersion(data)
}

export async function updatePluginConfigVersion(instanceId: string, version: number, values: Record<string, unknown>): Promise<PluginConfigVersion> {
  const data = await request<PluginConfigVersion>(`/api/plugin-configs/${encodeURIComponent(instanceId)}/versions/${version}`, {
    method: 'PUT',
    body: JSON.stringify({ values }),
  })
  return normalizePluginConfigVersion(data)
}

export async function validatePluginConfigVersion(instanceId: string, version: number): Promise<PluginConfigValidationResponse> {
  const data = await request<PluginConfigValidationResponse>(`/api/plugin-configs/${encodeURIComponent(instanceId)}/versions/${version}/validate`, { method: 'POST' })
  return {
    ...data,
    errors: listOrEmpty(data.errors),
    version: normalizePluginConfigVersion(data.version),
  }
}

export async function activatePluginConfigVersion(instanceId: string, version: number): Promise<PluginConfigInstanceDetail> {
  const data = await request<PluginConfigInstanceDetail>(`/api/plugin-configs/${encodeURIComponent(instanceId)}/versions/${version}/activate`, { method: 'POST' })
  return normalizePluginConfigDetail(data)
}

export async function disablePluginConfig(instanceId: string): Promise<PluginConfigInstanceDetail> {
  const data = await request<PluginConfigInstanceDetail>(`/api/plugin-configs/${encodeURIComponent(instanceId)}/disable`, { method: 'POST' })
  return normalizePluginConfigDetail(data)
}

export async function fetchPluginAssets(params: { plugin_id?: string; capability_id?: string; config_instance_id?: string; scope?: string; kind?: string }): Promise<PluginAsset[]> {
  const qs = new URLSearchParams()
  if (params.plugin_id) qs.set('plugin_id', params.plugin_id)
  if (params.capability_id) qs.set('capability_id', params.capability_id)
  if (params.config_instance_id) qs.set('config_instance_id', params.config_instance_id)
  if (params.scope) qs.set('scope', params.scope)
  if (params.kind) qs.set('kind', params.kind)
  const data = await request<PluginAsset[] | null>(`/api/plugin-assets${qs.toString() ? `?${qs.toString()}` : ''}`)
  return listOrEmpty(data)
}

export async function createPluginAsset(input: {
  id: string
  plugin_id: string
  capability_id?: string
  config_instance_id?: string
  scope: string
  kind: string
  title?: string
}): Promise<PluginAsset> {
  return request<PluginAsset>('/api/plugin-assets', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function uploadPluginAssetVersion(assetId: string, file: File): Promise<PluginAssetVersion> {
  const body = new FormData()
  body.set('file', file)
  const res = await fetch(`/api/plugin-assets/${encodeURIComponent(assetId)}/versions`, {
    method: 'POST',
    body,
    cache: 'no-store',
  })
  if (!res.ok) {
    const errorBody = await res.json().catch((): APIError => ({ error: res.statusText }))
    const bodyRecord = isRecord(errorBody) ? errorBody : {}
    const errorMessage = typeof bodyRecord.error === 'string' ? bodyRecord.error : ''
    throw new PulseOpsAPIError(errorMessage || `HTTP ${res.status}`, listOrEmpty(bodyRecord.errors as string[] | null | undefined), errorBody)
  }
  return res.json()
}

export async function validatePluginAssetVersion(assetId: string, version: number): Promise<PluginAssetVersion> {
  return request<PluginAssetVersion>(`/api/plugin-assets/${encodeURIComponent(assetId)}/versions/${version}/validate`, { method: 'POST' })
}

export async function activatePluginAssetVersion(assetId: string, version: number): Promise<PluginAsset> {
  return request<PluginAsset>(`/api/plugin-assets/${encodeURIComponent(assetId)}/versions/${version}/activate`, { method: 'POST' })
}

export async function fetchPluginSecrets(params: { plugin_id?: string; scope?: string }): Promise<PluginSecret[]> {
  const qs = new URLSearchParams()
  if (params.plugin_id) qs.set('plugin_id', params.plugin_id)
  if (params.scope) qs.set('scope', params.scope)
  const data = await request<PluginSecret[] | null>(`/api/plugin-secrets${qs.toString() ? `?${qs.toString()}` : ''}`)
  return listOrEmpty(data)
}

export async function upsertPluginSecret(input: {
  id: string
  plugin_id: string
  scope?: string
  title?: string
  value: string
}): Promise<PluginSecret> {
  return request<PluginSecret>('/api/plugin-secrets', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function reloadPlugins(): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>('/api/plugins/reload', { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function gcPlugins(): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>('/api/plugins/gc', { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function importPluginArchive(file: Blob): Promise<PluginRelease> {
  return request<PluginRelease>('/api/plugins/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/gzip' },
    body: file,
  })
}

export async function exportPluginRelease(id: string, version: string): Promise<Blob> {
  const res = await fetch(`/api/plugins/${encodeURIComponent(id)}/releases/${encodeURIComponent(version)}/export`, {
    cache: 'no-store',
  })
  if (!res.ok) {
    const body = await res.json().catch((): APIError => ({ error: res.statusText }))
    const errorBody = isRecord(body) ? body : {}
    const errorMessage = typeof errorBody.error === 'string' ? errorBody.error : ''
    throw new PulseOpsAPIError(errorMessage || `HTTP ${res.status}`, listOrEmpty(errorBody.errors as string[] | null | undefined), body)
  }
  return res.blob()
}

export async function validatePluginRelease(id: string, version: string): Promise<PluginRelease> {
  return request<PluginRelease>(`/api/plugins/${encodeURIComponent(id)}/releases/${encodeURIComponent(version)}/validate`, { method: 'POST' })
}

export async function activatePluginRelease(id: string, version: string): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>(`/api/plugins/${encodeURIComponent(id)}/releases/${encodeURIComponent(version)}/activate`, { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function disablePlugin(id: string): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>(`/api/plugins/${encodeURIComponent(id)}/disable`, { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function enablePlugin(id: string): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>(`/api/plugins/${encodeURIComponent(id)}/enable`, { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function rollbackPlugin(id: string): Promise<PluginCatalog> {
  const data = await request<PluginCatalog | null>(`/api/plugins/${encodeURIComponent(id)}/rollback`, { method: 'POST' })
  return normalizePluginCatalog(data)
}

export async function fetchTaskSample(taskId: string, source: string, jq?: string): Promise<SampleResponse> {
  const params = new URLSearchParams({ source })
  if (jq) params.set('jq', jq)
  return request<SampleResponse>(
    `/api/tasks/${encodeURIComponent(taskId)}/sample?${params.toString()}`
  )
}
