import type {
  TaskState,
  RunRecord,
  AIAnalysisRecord,
  ArtifactRef,
  ArtifactDetail,
  HealthResponse,
  ActionResponse,
  APIError,
} from './types'

// Generic fetch wrapper
async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch((): APIError => ({ error: res.statusText }))
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  return res.json()
}

// GET /healthz
export async function fetchHealth(): Promise<HealthResponse> {
  return request<HealthResponse>('/healthz')
}

// GET /tasks
export async function fetchTasks(): Promise<TaskState[]> {
  return request<TaskState[]>('/tasks')
}

// GET /tasks/{id}
export async function fetchTask(id: string): Promise<TaskState> {
  return request<TaskState>(`/tasks/${encodeURIComponent(id)}`)
}

// GET /tasks/{id}/runs
export async function fetchTaskRuns(id: string, limit?: number): Promise<RunRecord[]> {
  const params = new URLSearchParams()
  if (limit !== undefined) {
    params.set('limit', String(limit))
  }
  const qs = params.toString()
  return request<RunRecord[]>(`/tasks/${encodeURIComponent(id)}/runs${qs ? `?${qs}` : ''}`)
}

// GET /tasks/{id}/runs/{runID}
export async function fetchTaskRun(id: string, runID: string): Promise<RunRecord> {
  return request<RunRecord>(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}`)
}

// GET /tasks/{id}/runs/{runID}/ai
export async function fetchRunAIAnalysis(id: string, runID: string): Promise<AIAnalysisRecord> {
  return request<AIAnalysisRecord>(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/ai`)
}

// GET /tasks/{id}/ai
export async function fetchTaskAIAnalyses(id: string, limit?: number): Promise<AIAnalysisRecord[]> {
  const params = new URLSearchParams()
  if (limit !== undefined) {
    params.set('limit', String(limit))
  }
  const qs = params.toString()
  return request<AIAnalysisRecord[]>(`/tasks/${encodeURIComponent(id)}/ai${qs ? `?${qs}` : ''}`)
}

// GET /tasks/{id}/runs/{runID}/artifacts
export async function fetchRunArtifacts(id: string, runID: string): Promise<ArtifactRef[]> {
  return request<ArtifactRef[]>(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/artifacts`)
}

// GET /artifacts/{artifactID}
export async function fetchArtifactDetail(artifactID: string): Promise<ArtifactDetail> {
  return request<ArtifactDetail>(`/artifacts/${encodeURIComponent(artifactID)}`)
}

// POST /tasks/{id}/run
export async function triggerTaskRun(id: string): Promise<RunRecord> {
  return request<RunRecord>(`/tasks/${encodeURIComponent(id)}/run`, { method: 'POST' })
}

// POST /tasks/{id}/runs/{runID}/rerun
export async function retriggerTaskRun(id: string, runID: string): Promise<RunRecord> {
  return request<RunRecord>(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/rerun`, { method: 'POST' })
}

// POST /tasks/{id}/reload
export async function reloadTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/tasks/${encodeURIComponent(id)}/reload`, { method: 'POST' })
}

// POST /tasks/{id}/enable
export async function enableTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/tasks/${encodeURIComponent(id)}/enable`, { method: 'POST' })
}

// POST /tasks/{id}/disable
export async function disableTask(id: string): Promise<ActionResponse> {
  return request<ActionResponse>(`/tasks/${encodeURIComponent(id)}/disable`, { method: 'POST' })
}
