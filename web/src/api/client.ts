import type {
  TaskState,
  TaskDefinition,
  Pipeline,
  RunRecord,
  AIAnalysisRecord,
  ArtifactRef,
  ArtifactDetail,
  HealthResponse,
  ActionResponse,
  APIError,
  GlobalSettings,
  SampleResponse,
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

// GET /api/tasks
export async function fetchTasks(): Promise<TaskState[]> {
  return request<TaskState[]>('/api/tasks')
}

// GET /api/tasks/{id}
export async function fetchTask(id: string): Promise<TaskState> {
  return request<TaskState>(`/api/tasks/${encodeURIComponent(id)}`)
}

// GET /api/tasks/{id}/runs
export async function fetchTaskRuns(id: string, limit?: number, since?: string): Promise<RunRecord[]> {
  const params = new URLSearchParams()
  if (limit !== undefined) {
    params.set('limit', String(limit))
  }
  if (since) {
    params.set('since', since)
  }
  const qs = params.toString()
  return request<RunRecord[]>(`/api/tasks/${encodeURIComponent(id)}/runs${qs ? `?${qs}` : ''}`)
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
export async function fetchArtifactDetail(artifactID: string): Promise<ArtifactDetail> {
  return request<ArtifactDetail>(`/api/artifacts/${encodeURIComponent(artifactID)}`)
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

export async function fetchSettings(): Promise<GlobalSettings> {
  return request<GlobalSettings>('/api/settings')
}

export async function updateSettings(settings: GlobalSettings): Promise<GlobalSettings> {
  return request<GlobalSettings>('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}

export async function fetchTaskSample(taskId: string, source: string): Promise<SampleResponse> {
  return request<SampleResponse>(
    `/api/tasks/${encodeURIComponent(taskId)}/sample?source=${encodeURIComponent(source)}`
  )
}
