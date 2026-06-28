-- 005: Plugin catalog, release state, and generation tracking

CREATE TABLE IF NOT EXISTS plugin_packages (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    author      TEXT NOT NULL DEFAULT '',
    homepage    TEXT NOT NULL DEFAULT '',
    official    BOOLEAN NOT NULL DEFAULT FALSE,
    bundled     BOOLEAN NOT NULL DEFAULT FALSE,
    status      TEXT NOT NULL DEFAULT 'disabled',
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plugin_releases (
    plugin_id        TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    version          TEXT NOT NULL,
    schema_version   TEXT NOT NULL,
    manifest_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    path             TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'staged',
    checksum         TEXT NOT NULL DEFAULT '',
    validation_error TEXT NOT NULL DEFAULT '',
    official         BOOLEAN NOT NULL DEFAULT FALSE,
    bundled          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    validated_at     TIMESTAMPTZ NULL,
    activated_at     TIMESTAMPTZ NULL,
    PRIMARY KEY(plugin_id, version)
);

CREATE INDEX IF NOT EXISTS idx_plugin_releases_status
    ON plugin_releases(status);

CREATE TABLE IF NOT EXISTS plugin_active_versions (
    plugin_id     TEXT PRIMARY KEY REFERENCES plugin_packages(id) ON DELETE CASCADE,
    version       TEXT NOT NULL,
    generation_id TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plugin_generations (
    generation_id        TEXT PRIMARY KEY,
    status               TEXT NOT NULL,
    active_versions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    capabilities_json    JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at           TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS plugin_generation_refs (
    generation_id    TEXT PRIMARY KEY REFERENCES plugin_generations(generation_id) ON DELETE CASCADE,
    ref_count        INTEGER NOT NULL DEFAULT 0,
    last_released_at TIMESTAMPTZ NULL,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plugin_events (
    id            BIGSERIAL PRIMARY KEY,
    plugin_id     TEXT NOT NULL DEFAULT '',
    version       TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    status        TEXT NOT NULL,
    message       TEXT NOT NULL DEFAULT '',
    generation_id TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugin_events_plugin_created
    ON plugin_events(plugin_id, created_at DESC);

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS plugin_generation_id TEXT NOT NULL DEFAULT '';
