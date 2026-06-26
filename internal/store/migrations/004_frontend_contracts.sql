-- 004: Frontend-facing contracts and multi-upstream dependency foundation

CREATE TABLE IF NOT EXISTS task_dependencies (
    id                  TEXT PRIMARY KEY,
    upstream_task_id    TEXT NOT NULL,
    downstream_task_id  TEXT NOT NULL,
    condition           TEXT NOT NULL DEFAULT '',
    source_key          TEXT NOT NULL DEFAULT '',
    params_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_deps_pair_source
    ON task_dependencies(upstream_task_id, downstream_task_id, source_key);

CREATE INDEX IF NOT EXISTS idx_task_deps_downstream
    ON task_dependencies(downstream_task_id);

CREATE INDEX IF NOT EXISTS idx_task_deps_upstream
    ON task_dependencies(upstream_task_id);

CREATE INDEX IF NOT EXISTS idx_runs_started
    ON runs(started_at DESC);

CREATE INDEX IF NOT EXISTS idx_runs_status_started
    ON runs(run_status, check_status, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_runs_labels_gin
    ON runs USING GIN(labels_json);
