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
  TaskView,
  DashboardSummary,
  RunListItem,
  TaskGraph,
  TaskDependency,
  BatchTaskResponse,
  SettingsResponse,
  TaskValidationResponse,
  PlatformConfigSummary,
} from './types'

// Generic fetch wrapper
async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    cache: 'no-store',
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch((): APIError => ({ error: res.statusText }))
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  return res.json()
}

// GET /api/healthz
export async function fetchHealth(): Promise<HealthResponse> {
  return request<HealthResponse>('/api/healthz')
}

export async function fetchPlatformConfig(): Promise<PlatformConfigSummary> {
  return request<PlatformConfigSummary>('/api/platform-config')
}

export async function updatePlatformConfig(config: PlatformConfigSummary): Promise<PlatformConfigSummary> {
  return request<PlatformConfigSummary>('/api/platform-config', {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

// GET /api/tasks
export async function fetchTasks(): Promise<TaskView[]> {
  return request<TaskView[]>('/api/tasks')
}

// GET /api/tasks/{id}
export async function fetchTask(id: string): Promise<TaskView> {
  return request<TaskView>(`/api/tasks/${encodeURIComponent(id)}`)
}

export async function fetchDashboardSummary(since = '24h'): Promise<DashboardSummary> {
  const params = new URLSearchParams({ since })
  return request<DashboardSummary>(`/api/dashboard/summary?${params.toString()}`)
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
  return request<PaginatedRuns>(`/api/tasks/${encodeURIComponent(id)}/runs${qs ? `?${qs}` : ''}`)
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
  return request<PaginatedRuns>(`/api/runs${qs ? `?${qs}` : ''}`)
}

// GET /api/tasks/{id}/runs/stats
export async function fetchTaskRunStats(id: string, since?: string): Promise<RunStat[]> {
  const params = new URLSearchParams()
  if (since) params.set('since', since)
  const qs = params.toString()
  return request<RunStat[]>(`/api/tasks/${encodeURIComponent(id)}/runs/stats${qs ? `?${qs}` : ''}`)
}

// GET /api/tasks/{id}/runs/{runID}
export async function fetchTaskRun(id: string, runID: string): Promise<RunRecord> {
  return request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}`)
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
  return request<AIAnalysisRecord[]>(`/api/tasks/${encodeURIComponent(id)}/ai${qs ? `?${qs}` : ''}`)
}

// GET /api/tasks/{id}/runs/{runID}/artifacts
export async function fetchRunArtifacts(id: string, runID: string): Promise<ArtifactRef[]> {
  return request<ArtifactRef[]>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/artifacts`)
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
  return request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/run`, { method: 'POST' })
}

export async function batchTaskAction(action: 'run' | 'enable' | 'disable' | 'reload', taskIds: string[]): Promise<BatchTaskResponse> {
  return request<BatchTaskResponse>('/api/tasks/batch', {
    method: 'POST',
    body: JSON.stringify({ action, task_ids: taskIds }),
  })
}

// POST /api/tasks/{id}/runs/{runID}/rerun
export async function retriggerTaskRun(id: string, runID: string): Promise<RunRecord> {
  return request<RunRecord>(`/api/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/rerun`, { method: 'POST' })
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
  return request<TaskDefinition[]>('/api/task-defs')
}

// GET /api/task-defs/{id}
export async function fetchTaskDefinition(id: string): Promise<TaskDefinition> {
  return request<TaskDefinition>(`/api/task-defs/${encodeURIComponent(id)}`)
}

// POST /api/task-defs
export async function createTaskDefinition(def: TaskDefinition): Promise<TaskDefinition> {
  return request<TaskDefinition>('/api/task-defs', {
    method: 'POST',
    body: JSON.stringify(def),
  })
}

// PUT /api/task-defs/{id}
export async function updateTaskDefinition(id: string, def: TaskDefinition): Promise<TaskDefinition> {
  return request<TaskDefinition>(`/api/task-defs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(def),
  })
}

// DELETE /api/task-defs/{id}
export async function deleteTaskDefinition(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/task-defs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function validateTaskDefinition(def: TaskDefinition): Promise<TaskValidationResponse> {
  return request<TaskValidationResponse>('/api/task-defs/validate', {
    method: 'POST',
    body: JSON.stringify(def),
  })
}

export async function dryRunTaskDefinition(def: TaskDefinition): Promise<TaskValidationResponse> {
  return request<TaskValidationResponse>('/api/task-defs/dry-run', {
    method: 'POST',
    body: JSON.stringify(def),
  })
}

export async function testRunTaskDefinition(def: TaskDefinition): Promise<RunRecord> {
  return request<RunRecord>('/api/task-defs/test-run', {
    method: 'POST',
    body: JSON.stringify(def),
  })
}

export async function fetchPipelines(): Promise<Pipeline[]> {
  return request<Pipeline[]>('/api/pipelines')
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
  return request<TaskDefinition[]>(`/api/pipelines/${encodeURIComponent(pipelineId)}/tasks`)
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
  return request<TaskGraph>(`/api/task-graph${qs ? `?${qs}` : ''}`)
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
  return request<SettingsResponse>('/api/settings')
}

export async function updateSettings(settings: GlobalSettings): Promise<SettingsResponse> {
  return request<SettingsResponse>('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}

export async function fetchTaskSample(taskId: string, source: string, jq?: string): Promise<SampleResponse> {
  const params = new URLSearchParams({ source })
  if (jq) params.set('jq', jq)
  return request<SampleResponse>(
    `/api/tasks/${encodeURIComponent(taskId)}/sample?${params.toString()}`
  )
}
