-- 006: Plugin configuration instances, assets, and secrets

CREATE TABLE IF NOT EXISTS plugin_config_instances (
    id              TEXT PRIMARY KEY,
    plugin_id       TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    capability_id   TEXT NOT NULL DEFAULT '',
    capability_type TEXT NOT NULL DEFAULT '',
    capability_name TEXT NOT NULL DEFAULT '',
    scope           TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'draft',
    active_version  INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugin_config_instances_plugin
    ON plugin_config_instances(plugin_id, capability_id, status);

CREATE TABLE IF NOT EXISTS plugin_config_versions (
    instance_id      TEXT NOT NULL REFERENCES plugin_config_instances(id) ON DELETE CASCADE,
    version          INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'draft',
    values_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    validation_error TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    validated_at     TIMESTAMPTZ NULL,
    activated_at     TIMESTAMPTZ NULL,
    retired_at       TIMESTAMPTZ NULL,
    PRIMARY KEY(instance_id, version)
);

CREATE INDEX IF NOT EXISTS idx_plugin_config_versions_status
    ON plugin_config_versions(status);

CREATE TABLE IF NOT EXISTS plugin_assets (
    id             TEXT PRIMARY KEY,
    plugin_id      TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    capability_id  TEXT NOT NULL DEFAULT '',
    config_instance_id TEXT NOT NULL DEFAULT '',
    scope          TEXT NOT NULL DEFAULT 'capability_shared',
    kind           TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'draft',
    active_version INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE plugin_assets
    ADD COLUMN IF NOT EXISTS config_instance_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'capability_shared';

CREATE INDEX IF NOT EXISTS idx_plugin_assets_plugin
    ON plugin_assets(plugin_id, capability_id, config_instance_id, scope, kind, status);

CREATE TABLE IF NOT EXISTS plugin_asset_versions (
    asset_id         TEXT NOT NULL REFERENCES plugin_assets(id) ON DELETE CASCADE,
    version          INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'draft',
    filename         TEXT NOT NULL DEFAULT '',
    content_type     TEXT NOT NULL DEFAULT '',
    storage_uri      TEXT NOT NULL DEFAULT '',
    content          BYTEA NOT NULL DEFAULT ''::bytea,
    size_bytes       BIGINT NOT NULL DEFAULT 0,
    checksum         TEXT NOT NULL DEFAULT '',
    validation_error TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    validated_at     TIMESTAMPTZ NULL,
    activated_at     TIMESTAMPTZ NULL,
    retired_at       TIMESTAMPTZ NULL,
    PRIMARY KEY(asset_id, version)
);

CREATE INDEX IF NOT EXISTS idx_plugin_asset_versions_status
    ON plugin_asset_versions(status);

ALTER TABLE plugin_asset_versions
    ADD COLUMN IF NOT EXISTS content BYTEA NOT NULL DEFAULT ''::bytea;

CREATE TABLE IF NOT EXISTS plugin_secrets (
    id         TEXT PRIMARY KEY,
    plugin_id  TEXT NOT NULL REFERENCES plugin_packages(id) ON DELETE CASCADE,
    scope      TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL DEFAULT '',
    masked     TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugin_secrets_plugin
    ON plugin_secrets(plugin_id, scope, status);

CREATE TABLE IF NOT EXISTS plugin_secret_values (
    secret_id            TEXT PRIMARY KEY REFERENCES plugin_secrets(id) ON DELETE CASCADE,
    ciphertext           TEXT NOT NULL,
    encryption_meta_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS plugin_config_events (
    id            BIGSERIAL PRIMARY KEY,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    plugin_id     TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    status        TEXT NOT NULL,
    message       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plugin_config_events_resource_created
    ON plugin_config_events(resource_type, resource_id, created_at DESC);

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS plugin_config_versions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS plugin_asset_versions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS plugin_task_overrides_json JSONB NOT NULL DEFAULT '{}'::jsonb;
