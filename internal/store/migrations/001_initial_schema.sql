-- 001: Initial schema — all core tables and indexes

CREATE TABLE IF NOT EXISTS runs (
    run_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    task_kind TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    run_status TEXT NOT NULL,
    check_status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload JSONB,
    stdout TEXT NOT NULL DEFAULT '',
    stderr TEXT NOT NULL DEFAULT '',
    labels_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_runs_task_started
    ON runs(task_id, started_at DESC);

CREATE TABLE IF NOT EXISTS findings (
    finding_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    sample_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    data_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_findings_run
    ON findings(run_id);

CREATE TABLE IF NOT EXISTS artifacts (
    artifact_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    storage_kind TEXT NOT NULL,
    uri TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    preview_text TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    expire_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_artifacts_run
    ON artifacts(run_id);

CREATE TABLE IF NOT EXISTS task_runtime_state (
    task_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    status TEXT NOT NULL,
    labels_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_run_at TIMESTAMPTZ NULL,
    next_run_at TIMESTAMPTZ NULL,
    last_run_status TEXT,
    last_check_status TEXT,
    last_error TEXT,
    last_duration_ms BIGINT NOT NULL DEFAULT 0,
    last_reload_error TEXT,
    last_sample_seed BIGINT NOT NULL DEFAULT 0,
    last_sample_count INTEGER NOT NULL DEFAULT 0,
    last_mismatch_count INTEGER NOT NULL DEFAULT 0,
    source_path TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS task_reload_failures (
    id BIGSERIAL PRIMARY KEY,
    task_id TEXT NOT NULL,
    source_path TEXT NOT NULL,
    error_message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS run_steps (
    step_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    step_status TEXT NOT NULL,
    started_at TIMESTAMPTZ NULL,
    ended_at TIMESTAMPTZ NULL,
    input_json JSONB NULL,
    output_json JSONB NULL
);

CREATE TABLE IF NOT EXISTS ai_analyses (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    analysis_type TEXT NOT NULL DEFAULT 'general',
    model TEXT NOT NULL,
    prompt TEXT NOT NULL,
    response TEXT NOT NULL,
    tokens_in INTEGER NOT NULL DEFAULT 0,
    tokens_out INTEGER NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'success',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_analyses_run
    ON ai_analyses(run_id);

CREATE INDEX IF NOT EXISTS idx_ai_analyses_task
    ON ai_analyses(task_id, analysis_type, created_at DESC);
