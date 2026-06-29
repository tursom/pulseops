package store

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestPostgresStoreInsertRunPersistsRunFindingsAndArtifacts(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()
	record := RunRecord{
		RunID:                "run-1",
		TaskID:               "task-a",
		TaskKind:             "scenario_check",
		PluginGenerationID:   "plugin-gen-1",
		TriggerType:          "manual",
		RunStatus:            "success",
		CheckStatus:          "fail",
		StartedAt:            now,
		EndedAt:              now.Add(2 * time.Second),
		DurationMS:           2000,
		Summary:              map[string]any{"sample_count": 2},
		Payload:              []byte(`{"sample_seed":42}`),
		PluginConfigVersions: map[string]any{"cfg-grpc": 3},
		PluginAssetVersions:  map[string]any{"inventory-proto": 4},
		PluginTaskOverrides:  map[string]any{"inventory": map[string]any{"method": "GetInventory"}},
		Findings: []Finding{
			{
				FindingID: "finding-1",
				RunID:     "run-1",
				TaskID:    "task-a",
				SampleID:  "goods-1",
				Reason:    "price_mismatch",
				Data:      map[string]any{"expected": 100},
			},
		},
		ArtifactRefs: []ArtifactRef{
			{
				ArtifactID:  "artifact-1",
				Kind:        "payload",
				StorageKind: "s3",
				URI:         "s3://bucket/prod/task-a/run-1/payload.json",
				ContentType: "application/json",
				SizeBytes:   17,
				SHA256:      "abc",
				PreviewText: `{"sample_seed":42}`,
			},
		},
		Labels: map[string]string{"env": "test"},
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO runs (
			run_id, task_id, task_kind, plugin_generation_id, trigger_type, run_status, check_status,
			started_at, ended_at, duration_ms, error_message, summary_json, payload,
			stdout, stderr, labels_json, plugin_config_versions_json, plugin_asset_versions_json,
			plugin_task_overrides_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15, $16::jsonb, $17::jsonb, $18::jsonb, $19::jsonb)`)).
		WithArgs(
			record.RunID, record.TaskID, record.TaskKind, record.PluginGenerationID, record.TriggerType, record.RunStatus, record.CheckStatus,
			record.StartedAt, record.EndedAt, record.DurationMS, record.ErrorMessage, `{"sample_count":2}`, `{"sample_seed":42}`,
			record.Stdout, record.Stderr, `{"env":"test"}`, `{"cfg-grpc":3}`, `{"inventory-proto":4}`, `{"inventory":{"method":"GetInventory"}}`,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO findings (finding_id, run_id, task_id, sample_id, reason, data_json, created_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`)).
		WithArgs("finding-1", "run-1", "task-a", "goods-1", "price_mismatch", `{"expected":100}`, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO artifacts (
				artifact_id, run_id, task_id, kind, storage_kind, uri, content_type,
				size_bytes, sha256, preview_text, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`)).
		WithArgs("artifact-1", "run-1", "task-a", "payload", "s3", "s3://bucket/prod/task-a/run-1/payload.json", "application/json", int64(17), "abc", `{"sample_seed":42}`, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := st.InsertRun(context.Background(), record); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStorePluginGenerationRefs(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO plugin_generation_refs(generation_id, ref_count, updated_at)
		VALUES ($1, 1, NOW())
		ON CONFLICT(generation_id) DO UPDATE
		SET ref_count = plugin_generation_refs.ref_count + 1,
		    updated_at = NOW()`)).
		WithArgs("plugin-gen-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE plugin_generation_refs
		SET ref_count = GREATEST(ref_count - 1, 0),
		    last_released_at = CASE WHEN ref_count <= 1 THEN NOW() ELSE last_released_at END,
		    updated_at = NOW()
		WHERE generation_id = $1`)).
		WithArgs("plugin-gen-1").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := st.AcquirePluginGeneration(context.Background(), "plugin-gen-1"); err != nil {
		t.Fatalf("acquire generation: %v", err)
	}
	if err := st.ReleasePluginGeneration(context.Background(), "plugin-gen-1"); err != nil {
		t.Fatalf("release generation: %v", err)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStorePluginReleaseProtected(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS (
			SELECT 1
			FROM plugin_generations g
			WHERE g.active_versions_json ->> $1 = $2
		)`)).
		WithArgs("external-driver", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	protected, err := st.PluginReleaseProtected(context.Background(), "external-driver", "1.0.0", 10*time.Minute)
	if err != nil {
		t.Fatalf("release protected: %v", err)
	}
	if !protected {
		t.Fatal("expected release to be protected")
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreDeleteExpiredPluginGenerations(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM plugin_generations g
		USING plugin_generation_refs r
		WHERE r.generation_id = g.generation_id
		  AND g.generation_id <> $1
		  AND r.ref_count = 0
		  AND r.last_released_at IS NOT NULL
		  AND r.last_released_at <= $2`)).
		WithArgs("plugin-gen-active", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))

	removed, err := st.DeleteExpiredPluginGenerations(context.Background(), "plugin-gen-active", 10*time.Minute)
	if err != nil {
		t.Fatalf("delete expired generations: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 deleted generations, got %d", removed)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreCommitPluginGenerationUsesCASTransaction(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()
	commit := pluginmodel.GenerationCommit{
		PackageID:             "external-driver",
		PackageStatus:         "enabled",
		SetActiveVersion:      true,
		ExpectedActiveVersion: "1.0.0",
		ActiveVersion:         "1.1.0",
		ActiveReleaseVersion:  "1.1.0",
		DrainingVersion:       "1.0.0",
		Generation: pluginmodel.GenerationRecord{
			ID:             "plugin-gen-2",
			Status:         "active",
			ActiveVersions: map[string]string{"external-driver": "1.1.0"},
			Capabilities:   []pluginmodel.Capability{{ID: "external-driver:task_driver:external_check"}},
			CreatedAt:      now,
		},
		Event: pluginmodel.EventRecord{
			PluginID: "external-driver",
			Version:  "1.1.0",
			Action:   "activate",
			Status:   "ok",
		},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`WITH changed AS (
			INSERT INTO plugin_active_versions(plugin_id, version, generation_id, updated_at)
			VALUES ($1, $3, $4, NOW())
			ON CONFLICT(plugin_id) DO UPDATE SET
				version = EXCLUDED.version,
				generation_id = EXCLUDED.generation_id,
				updated_at = EXCLUDED.updated_at
			WHERE plugin_active_versions.version = $2
			RETURNING 1
		)
		SELECT COUNT(*) FROM changed`)).
		WithArgs("external-driver", "1.0.0", "1.1.0", "plugin-gen-2").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE plugin_packages
			SET status = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1`)).
		WithArgs("external-driver", "enabled", "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE plugin_releases
			SET status = 'draining', validation_error = '', updated_at = NOW()
			WHERE plugin_id = $1 AND version = $2`)).
		WithArgs("external-driver", "1.0.0").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE plugin_generation_refs r
			SET last_released_at = NOW(),
			    updated_at = NOW()
			FROM plugin_generations g
			WHERE g.generation_id = r.generation_id
			  AND g.active_versions_json ->> $1 = $2
			  AND g.generation_id <> $3
			  AND r.ref_count = 0`)).
		WithArgs("external-driver", "1.0.0", "plugin-gen-2").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE plugin_releases
			SET status = 'active', validation_error = '', activated_at = NOW(), updated_at = NOW()
			WHERE plugin_id = $1 AND version = $2`)).
		WithArgs("external-driver", "1.1.0").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO plugin_generations (
				generation_id, status, active_versions_json, capabilities_json, created_at, retired_at
			) VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6)
			ON CONFLICT(generation_id) DO NOTHING`)).
		WithArgs(
			"plugin-gen-2",
			"active",
			`{"external-driver":"1.1.0"}`,
			`[{"id":"external-driver:task_driver:external_check","type":"","name":"","plugin_id":"","plugin_name":"","plugin_version":"","status":"","enabled":false,"official":false,"bundled":false}]`,
			now,
			nil,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO plugin_generation_refs(generation_id, ref_count, last_released_at, updated_at)
		VALUES ($1, 0, NOW(), NOW())
		ON CONFLICT(generation_id) DO NOTHING`)).
		WithArgs("plugin-gen-2").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO plugin_events(plugin_id, version, action, status, message, generation_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`)).
		WithArgs("external-driver", "1.1.0", "activate", "ok", "", "plugin-gen-2", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := st.CommitPluginGeneration(context.Background(), commit); err != nil {
		t.Fatalf("commit plugin generation: %v", err)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStorePluginConfigVersionLifecycle(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	ctx := context.Background()

	instance := pluginmodel.ConfigInstanceRecord{
		ID:             "cfg-grpc-prod",
		PluginID:       "@pulseops/grpc-source",
		CapabilityID:   "@pulseops/grpc-source:data_source:grpc",
		CapabilityType: "data_source",
		CapabilityName: "grpc",
		Scope:          "capability",
		Title:          "gRPC prod",
		Status:         "draft",
	}
	mock.ExpectExec("INSERT INTO plugin_config_instances").
		WithArgs(instance.ID, instance.PluginID, instance.CapabilityID, instance.CapabilityType, instance.CapabilityName, instance.Scope, instance.Title, instance.Status, 0).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := st.UpsertPluginConfigInstance(ctx, instance); err != nil {
		t.Fatalf("upsert config instance: %v", err)
	}

	version := pluginmodel.ConfigVersionRecord{
		InstanceID: instance.ID,
		Version:    1,
		Status:     "validated",
		Values:     map[string]any{"endpoint": "inventory.service:9090"},
	}
	mock.ExpectExec("INSERT INTO plugin_config_versions").
		WithArgs(version.InstanceID, version.Version, version.Status, `{"endpoint":"inventory.service:9090"}`, "", nil, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := st.UpsertPluginConfigVersion(ctx, version); err != nil {
		t.Fatalf("upsert config version: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE plugin_config_versions\\s+SET status = 'retired'").
		WithArgs(instance.ID, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_config_versions\\s+SET status = 'active'").
		WithArgs(instance.ID, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_config_instances").
		WithArgs(instance.ID, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := st.ActivatePluginConfigVersion(ctx, instance.ID, 1); err != nil {
		t.Fatalf("activate config version: %v", err)
	}

	mock.ExpectExec("UPDATE plugin_config_instances").
		WithArgs(instance.ID, "disabled").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := st.UpdatePluginConfigInstanceStatus(ctx, instance.ID, "disabled"); err != nil {
		t.Fatalf("disable config instance: %v", err)
	}

	event := pluginmodel.ConfigEventRecord{
		ResourceType: "config_version",
		ResourceID:   "cfg-grpc-prod:1",
		PluginID:     "@pulseops/grpc-source",
		Action:       "activate",
		Status:       "success",
		Message:      "",
	}
	mock.ExpectExec("INSERT INTO plugin_config_events").
		WithArgs(event.ResourceType, event.ResourceID, event.PluginID, event.Action, event.Status, event.Message, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := st.InsertPluginConfigEvent(ctx, event); err != nil {
		t.Fatalf("insert config event: %v", err)
	}

	eventTime := time.Now().UTC()
	mock.ExpectQuery("SELECT id, resource_type, resource_id, plugin_id, action, status, message, created_at").
		WithArgs("@pulseops/grpc-source", "config_version", "cfg-grpc-prod:1", 50).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "resource_type", "resource_id", "plugin_id", "action", "status", "message", "created_at",
		}).AddRow(
			42, event.ResourceType, event.ResourceID, event.PluginID, event.Action, event.Status, event.Message, eventTime,
		))
	events, err := st.ListPluginConfigEvents(ctx, "@pulseops/grpc-source", "config_version", "cfg-grpc-prod:1", 50)
	if err != nil {
		t.Fatalf("list config events: %v", err)
	}
	if len(events) != 1 || events[0].ID != 42 || events[0].ResourceID != event.ResourceID {
		t.Fatalf("unexpected config events: %#v", events)
	}

	assertNoMockError(t, mock)
}

func TestPostgresStorePluginAssetAndSecret(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	ctx := context.Background()

	asset := pluginmodel.AssetRecord{
		ID:           "asset-inventory-proto",
		PluginID:     "@pulseops/grpc-source",
		CapabilityID: "@pulseops/grpc-source:data_source:grpc",
		Scope:        pluginmodel.AssetScopeCapabilityShared,
		Kind:         "proto_files",
		Title:        "Inventory proto",
		Status:       "draft",
	}
	mock.ExpectExec("INSERT INTO plugin_assets").
		WithArgs(asset.ID, asset.PluginID, asset.CapabilityID, asset.ConfigInstanceID, asset.Scope, asset.Kind, asset.Title, asset.Status, 0).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := st.UpsertPluginAsset(ctx, asset); err != nil {
		t.Fatalf("upsert asset: %v", err)
	}

	assetVersion := pluginmodel.AssetVersionRecord{
		AssetID:     asset.ID,
		Version:     1,
		Status:      "validated",
		Filename:    "inventory.proto",
		ContentType: "text/plain",
		StorageURI:  "db://plugin-assets/pulseops/inventory.proto",
		Content:     []byte(`syntax = "proto3";`),
		SizeBytes:   128,
		Checksum:    "sha256:abc",
	}
	mock.ExpectExec("INSERT INTO plugin_asset_versions").
		WithArgs(assetVersion.AssetID, assetVersion.Version, assetVersion.Status, assetVersion.Filename, assetVersion.ContentType, assetVersion.StorageURI, assetVersion.Content, assetVersion.SizeBytes, assetVersion.Checksum, "", nil, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := st.UpsertPluginAssetVersion(ctx, assetVersion); err != nil {
		t.Fatalf("upsert asset version: %v", err)
	}

	secret := pluginmodel.SecretRecord{
		ID:       "sec-auth",
		PluginID: "@pulseops/grpc-source",
		Scope:    "grpc-prod",
		Title:    "Authorization",
		Masked:   "********",
		Status:   "active",
	}
	value := pluginmodel.SecretValueRecord{
		SecretID:       secret.ID,
		Ciphertext:     "ciphertext",
		EncryptionMeta: map[string]any{"alg": "local-v1"},
	}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO plugin_secrets").
		WithArgs(secret.ID, secret.PluginID, secret.Scope, secret.Title, secret.Masked, secret.Status).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO plugin_secret_values").
		WithArgs(secret.ID, value.Ciphertext, `{"alg":"local-v1"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := st.UpsertPluginSecret(ctx, secret, value); err != nil {
		t.Fatalf("upsert secret: %v", err)
	}

	assertNoMockError(t, mock)
}

func TestPostgresStoreReadsPluginConfigRecords(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT id, plugin_id, capability_id, capability_type, capability_name").
		WithArgs("@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "plugin_id", "capability_id", "capability_type", "capability_name",
			"scope", "title", "status", "active_version", "created_at", "updated_at",
		}).AddRow(
			"cfg-grpc-prod", "@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc", "data_source", "grpc",
			"capability", "gRPC prod", "active", 2, now, now,
		))

	instances, err := st.ListPluginConfigInstances(context.Background(), "@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc")
	if err != nil {
		t.Fatalf("list config instances: %v", err)
	}
	if len(instances) != 1 || instances[0].ActiveVersion != 2 {
		t.Fatalf("unexpected config instances: %#v", instances)
	}

	mock.ExpectQuery("SELECT v.instance_id, v.version, v.status, v.values_json, v.validation_error").
		WithArgs("cfg-grpc-prod").
		WillReturnRows(sqlmock.NewRows([]string{
			"instance_id", "version", "status", "values_json", "validation_error",
			"created_at", "updated_at", "validated_at", "activated_at", "retired_at",
		}).AddRow(
			"cfg-grpc-prod", 2, "active", []byte(`{"endpoint":"inventory.service:9090"}`), "",
			now, now, now, now, nil,
		))

	active, err := st.GetActivePluginConfigVersion(context.Background(), "cfg-grpc-prod")
	if err != nil {
		t.Fatalf("get active config version: %v", err)
	}
	if active.Version != 2 || active.Values["endpoint"] != "inventory.service:9090" {
		t.Fatalf("unexpected active config version: %#v", active)
	}

	mock.ExpectQuery("SELECT instance_id, version, status, values_json, validation_error").
		WithArgs("cfg-grpc-prod").
		WillReturnRows(sqlmock.NewRows([]string{
			"instance_id", "version", "status", "values_json", "validation_error",
			"created_at", "updated_at", "validated_at", "activated_at", "retired_at",
		}).AddRow(
			"cfg-grpc-prod", 2, "active", []byte(`{"endpoint":"inventory.service:9090"}`), "",
			now, now, now, now, nil,
		).AddRow(
			"cfg-grpc-prod", 1, "retired", []byte(`{"endpoint":"old.service:9090"}`), "",
			now, now, now, now, now,
		))

	versions, err := st.ListPluginConfigVersions(context.Background(), "cfg-grpc-prod")
	if err != nil {
		t.Fatalf("list config versions: %v", err)
	}
	if len(versions) != 2 || versions[1].Status != "retired" {
		t.Fatalf("unexpected config versions: %#v", versions)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreReadsPluginAssetsAndSecrets(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT id, plugin_id, capability_id, config_instance_id, scope, kind, title, status, active_version").
		WithArgs("@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc", "", pluginmodel.AssetScopeCapabilityShared, "proto_files").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "plugin_id", "capability_id", "config_instance_id", "scope", "kind", "title", "status", "active_version", "created_at", "updated_at",
		}).AddRow(
			"asset-inventory-proto", "@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc",
			"", pluginmodel.AssetScopeCapabilityShared, "proto_files", "Inventory proto", "active", 3, now, now,
		))

	assets, err := st.ListPluginAssets(context.Background(), "@pulseops/grpc-source", "@pulseops/grpc-source:data_source:grpc", "", pluginmodel.AssetScopeCapabilityShared, "proto_files")
	if err != nil {
		t.Fatalf("list plugin assets: %v", err)
	}
	if len(assets) != 1 || assets[0].ActiveVersion != 3 {
		t.Fatalf("unexpected plugin assets: %#v", assets)
	}

	mock.ExpectQuery("SELECT v.asset_id, v.version, v.status, v.filename").
		WithArgs("asset-inventory-proto").
		WillReturnRows(sqlmock.NewRows([]string{
			"asset_id", "version", "status", "filename", "content_type", "storage_uri", "content", "size_bytes",
			"checksum", "validation_error", "created_at", "updated_at", "validated_at", "activated_at", "retired_at",
		}).AddRow(
			"asset-inventory-proto", 3, "active", "inventory.pb", "application/octet-stream",
			"db://plugin-assets/pulseops/inventory.pb", []byte("proto"), int64(512), "sha256:abc", "", now, now, now, now, nil,
		))

	activeAsset, err := st.GetActivePluginAssetVersion(context.Background(), "asset-inventory-proto")
	if err != nil {
		t.Fatalf("get active plugin asset version: %v", err)
	}
	if activeAsset.Version != 3 || activeAsset.StorageURI == "" || string(activeAsset.Content) != "proto" {
		t.Fatalf("unexpected active plugin asset version: %#v", activeAsset)
	}

	mock.ExpectQuery("SELECT id, plugin_id, scope, title, masked, status").
		WithArgs("@pulseops/grpc-source", "grpc-prod").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "plugin_id", "scope", "title", "masked", "status", "created_at", "updated_at",
		}).AddRow(
			"sec-auth", "@pulseops/grpc-source", "grpc-prod", "Authorization", "********", "active", now, now,
		))

	secrets, err := st.ListPluginSecrets(context.Background(), "@pulseops/grpc-source", "grpc-prod")
	if err != nil {
		t.Fatalf("list plugin secrets: %v", err)
	}
	if len(secrets) != 1 || secrets[0].Masked != "********" {
		t.Fatalf("unexpected plugin secrets: %#v", secrets)
	}

	mock.ExpectQuery("SELECT secret_id, ciphertext, encryption_meta_json, updated_at").
		WithArgs("sec-auth").
		WillReturnRows(sqlmock.NewRows([]string{
			"secret_id", "ciphertext", "encryption_meta_json", "updated_at",
		}).AddRow("sec-auth", "ciphertext", []byte(`{"alg":"local-v1"}`), now))

	value, err := st.GetPluginSecretValue(context.Background(), "sec-auth")
	if err != nil {
		t.Fatalf("get plugin secret value: %v", err)
	}
	if value.Ciphertext != "ciphertext" || value.EncryptionMeta["alg"] != "local-v1" {
		t.Fatalf("unexpected plugin secret value: %#v", value)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreListsRunsAndLoadsRunDetail(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
		       plugin_asset_versions_json, plugin_task_overrides_json
		FROM runs
		WHERE task_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3`)).
		WithArgs("task-a", 5, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json", "plugin_generation_id", "plugin_config_versions_json",
			"plugin_asset_versions_json", "plugin_task_overrides_json",
		}).AddRow(
			"run-1", "task-a", "scenario_check", "manual", "success", "fail",
			now, now.Add(time.Second), 1000, "", []byte(`{"sample_count":1}`), []byte(`{"sample_seed":1}`),
			"", "", []byte(`{"env":"test"}`), "plugin-gen-1", []byte(`{"cfg-grpc":3}`),
			[]byte(`{"inventory-proto":4}`), []byte(`{"inventory":{"method":"GetInventory"}}`),
		))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT artifact_id, kind, storage_kind, uri, content_type, size_bytes, sha256, preview_text
		FROM artifacts
		WHERE task_id = $1 AND run_id = $2
		ORDER BY created_at ASC`)).
		WithArgs("task-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"artifact_id", "kind", "storage_kind", "uri", "content_type", "size_bytes", "sha256", "preview_text",
		}))

	runs, err := st.ListRuns(context.Background(), "task-a", 5, 0, 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].RunID != "run-1" {
		t.Fatalf("unexpected runs: %#v", runs)
	}
	if runs[0].PluginGenerationID != "plugin-gen-1" || runs[0].PluginConfigVersions["cfg-grpc"] != float64(3) || runs[0].PluginAssetVersions["inventory-proto"] != float64(4) {
		t.Fatalf("unexpected plugin trace on listed run: %#v", runs[0])
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
		       plugin_asset_versions_json, plugin_task_overrides_json
		FROM runs
		WHERE task_id = $1 AND run_id = $2`)).
		WithArgs("task-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json", "plugin_generation_id", "plugin_config_versions_json",
			"plugin_asset_versions_json", "plugin_task_overrides_json",
		}).AddRow(
			"run-1", "task-a", "scenario_check", "manual", "success", "fail",
			now, now.Add(time.Second), 1000, "", []byte(`{"sample_count":1}`), []byte(`{"sample_seed":1}`),
			"", "", []byte(`{"env":"test"}`), "plugin-gen-1", []byte(`{"cfg-grpc":3}`),
			[]byte(`{"inventory-proto":4}`), []byte(`{"inventory":{"method":"GetInventory"}}`),
		))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT finding_id, run_id, task_id, sample_id, reason, data_json
		FROM findings
		WHERE run_id = $1
		ORDER BY created_at ASC`)).
		WithArgs("run-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"finding_id", "run_id", "task_id", "sample_id", "reason", "data_json",
		}).AddRow("finding-1", "run-1", "task-a", "goods-1", "price_mismatch", []byte(`{"expected":100}`)))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT artifact_id, kind, storage_kind, uri, content_type, size_bytes, sha256, preview_text
		FROM artifacts
		WHERE task_id = $1 AND run_id = $2
		ORDER BY created_at ASC`)).
		WithArgs("task-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"artifact_id", "kind", "storage_kind", "uri", "content_type", "size_bytes", "sha256", "preview_text",
		}).AddRow("artifact-1", "payload", "s3", "s3://bucket/object", "application/json", int64(10), "abc", "preview"))

	record, err := st.GetRun(context.Background(), "task-a", "run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if len(record.Findings) != 1 || record.Findings[0].SampleID != "goods-1" {
		t.Fatalf("unexpected findings: %#v", record.Findings)
	}
	if len(record.ArtifactRefs) != 1 || record.ArtifactRefs[0].ArtifactID != "artifact-1" {
		t.Fatalf("unexpected artifacts: %#v", record.ArtifactRefs)
	}
	if record.PluginGenerationID != "plugin-gen-1" || record.PluginConfigVersions["cfg-grpc"] != float64(3) || record.PluginAssetVersions["inventory-proto"] != float64(4) {
		t.Fatalf("unexpected plugin trace on detail: %#v", record)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreTaskStateAndReloadFailure(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	state := TaskState{
		TaskID:     "task-a",
		Name:       "Task A",
		Kind:       "http_check",
		Enabled:    true,
		Status:     "running",
		Labels:     map[string]string{"env": "test"},
		SourcePath: "/tmp/task-a.toml",
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO task_runtime_state (
			task_id, name, kind, enabled, status, labels_json, last_run_at, next_run_at,
			last_run_status, last_check_status, last_error, last_duration_ms, last_reload_error,
			last_sample_seed, last_sample_count, last_mismatch_count, source_path, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT(task_id) DO UPDATE SET
			name=excluded.name,
			kind=excluded.kind,
			enabled=excluded.enabled,
			status=excluded.status,
			labels_json=excluded.labels_json,
			last_run_at=excluded.last_run_at,
			next_run_at=excluded.next_run_at,
			last_run_status=excluded.last_run_status,
			last_check_status=excluded.last_check_status,
			last_error=excluded.last_error,
			last_duration_ms=excluded.last_duration_ms,
			last_reload_error=excluded.last_reload_error,
			last_sample_seed=excluded.last_sample_seed,
			last_sample_count=excluded.last_sample_count,
			last_mismatch_count=excluded.last_mismatch_count,
			source_path=excluded.source_path,
			updated_at=excluded.updated_at`)).
		WithArgs("task-a", "Task A", "http_check", true, "running", `{"env":"test"}`, nil, nil, "", "", "", int64(0), "", int64(0), 0, 0, "/tmp/task-a.toml", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO task_reload_failures(task_id, source_path, error_message, created_at)
		VALUES ($1, $2, $3, $4)`)).
		WithArgs("task-a", "/tmp/task-a.toml", "bad config", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM task_runtime_state WHERE task_id = $1`)).
		WithArgs("task-a").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := st.UpsertTaskState(context.Background(), state); err != nil {
		t.Fatalf("upsert task state: %v", err)
	}
	if err := st.InsertReloadFailure(context.Background(), "task-a", "/tmp/task-a.toml", "bad config"); err != nil {
		t.Fatalf("insert reload failure: %v", err)
	}
	if err := st.DeleteTaskState(context.Background(), "task-a"); err != nil {
		t.Fatalf("delete task state: %v", err)
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreListRunsWithTimeFilter(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
		       plugin_asset_versions_json, plugin_task_overrides_json
		FROM runs
		WHERE task_id = $1 AND started_at >= $3
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $4`)).
		WithArgs("task-a", 50, sqlmock.AnyArg(), 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json", "plugin_generation_id", "plugin_config_versions_json",
			"plugin_asset_versions_json", "plugin_task_overrides_json",
		}))

	runs, err := st.ListRuns(context.Background(), "task-a", 50, 0, 24*time.Hour)
	if err != nil {
		t.Fatalf("list runs with since: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runs))
	}
	assertNoMockError(t, mock)
}

func TestPostgresStoreUpsertTaskDependencyUpdatesByID(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()
	dep := config.TaskDependency{
		ID:               "dep-1",
		UpstreamTaskID:   "source-a",
		DownstreamTaskID: "task-a",
		Condition:        "run_status == success",
		SourceKey:        "source_a",
		Params:           map[string]any{"timeout_ms": 1200},
	}

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE task_dependencies
			SET upstream_task_id = $2,
			    downstream_task_id = $3,
			    condition = $4,
			    source_key = $5,
			    params_json = $6::jsonb,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at`)).
		WithArgs("dep-1", "source-a", "task-a", "run_status == success", "source_a", `{"timeout_ms":1200}`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "upstream_task_id", "downstream_task_id", "condition", "source_key", "params_json", "created_at", "updated_at",
		}).AddRow("dep-1", "source-a", "task-a", "run_status == success", "source_a", []byte(`{"timeout_ms":1200}`), now, now))

	saved, err := st.UpsertTaskDependency(context.Background(), dep)
	if err != nil {
		t.Fatalf("upsert dependency: %v", err)
	}
	if saved.SourceKey != "source_a" || saved.Params["timeout_ms"] != float64(1200) {
		t.Fatalf("unexpected saved dependency: %#v", saved)
	}
	assertNoMockError(t, mock)
}

func newMockStore(t *testing.T) (*PostgresStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &PostgresStore{db: db}, mock
}

func assertNoMockError(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
