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

export async function fetchTaskSample(taskId: string, source: string, jq?: string): Promise<SampleResponse> {
  const params = new URLSearchParams({ source })
  if (jq) params.set('jq', jq)
  return request<SampleResponse>(
    `/api/tasks/${encodeURIComponent(taskId)}/sample?${params.toString()}`
  )
}
