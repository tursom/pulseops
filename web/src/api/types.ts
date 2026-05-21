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

// Upload URL response (added by Go handler — artifact + presigned URL)
export interface ArtifactDetail extends ArtifactRef {
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
}

// === Health check response ===
export interface HealthResponse {
  status: string
}

// === Reload/enable/disable response ===
export interface ActionResponse {
  status: string
}
