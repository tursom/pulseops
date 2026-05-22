-- 003: Pipelines — multi-pipeline support for task organization

CREATE TABLE pipelines (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE task_definitions
    ADD COLUMN IF NOT EXISTS pipeline_id TEXT DEFAULT NULL;
