-- 002: Task definitions — DB-backed task configuration replacing TOML files

CREATE TABLE task_definitions (
    task_id          TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    kind             TEXT NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    interval         TEXT NOT NULL DEFAULT '',
    cron             TEXT NOT NULL DEFAULT '',
    timeout          TEXT NOT NULL DEFAULT '',
    labels_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    params_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    trigger          TEXT NOT NULL DEFAULT 'scheduled',
    watch_task_id    TEXT NOT NULL DEFAULT '',
    watch_condition  TEXT NOT NULL DEFAULT '',
    trace_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    alert_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_defs_kind ON task_definitions(kind);
CREATE INDEX idx_task_defs_watch ON task_definitions(watch_task_id) WHERE watch_task_id != '';
