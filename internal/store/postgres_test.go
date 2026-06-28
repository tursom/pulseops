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
		RunID:       "run-1",
		TaskID:      "task-a",
		TaskKind:    "scenario_check",
		TriggerType: "manual",
		RunStatus:   "success",
		CheckStatus: "fail",
		StartedAt:   now,
		EndedAt:     now.Add(2 * time.Second),
		DurationMS:  2000,
		Summary:     map[string]any{"sample_count": 2},
		Payload:     []byte(`{"sample_seed":42}`),
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
			run_id, task_id, task_kind, trigger_type, run_status, check_status,
			started_at, ended_at, duration_ms, error_message, summary_json, payload,
			stdout, stderr, labels_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15::jsonb)`)).
		WithArgs(
			record.RunID, record.TaskID, record.TaskKind, record.TriggerType, record.RunStatus, record.CheckStatus,
			record.StartedAt, record.EndedAt, record.DurationMS, record.ErrorMessage, `{"sample_count":2}`, `{"sample_seed":42}`,
			record.Stdout, record.Stderr, `{"env":"test"}`,
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
			JOIN plugin_generation_refs r ON r.generation_id = g.generation_id
			WHERE g.active_versions_json ->> $1 = $2
			  AND (r.ref_count > 0 OR r.last_released_at IS NULL OR r.last_released_at > $3)
		)`)).
		WithArgs("external-driver", "1.0.0", sqlmock.AnyArg()).
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

func TestPostgresStoreListsRunsAndLoadsRunDetail(t *testing.T) {
	t.Parallel()

	st, mock := newMockStore(t)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json
		FROM runs
		WHERE task_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3`)).
		WithArgs("task-a", 5, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json",
		}).AddRow(
			"run-1", "task-a", "scenario_check", "manual", "success", "fail",
			now, now.Add(time.Second), 1000, "", []byte(`{"sample_count":1}`), []byte(`{"sample_seed":1}`),
			"", "", []byte(`{"env":"test"}`),
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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json
		FROM runs
		WHERE task_id = $1 AND run_id = $2`)).
		WithArgs("task-a", "run-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json",
		}).AddRow(
			"run-1", "task-a", "scenario_check", "manual", "success", "fail",
			now, now.Add(time.Second), 1000, "", []byte(`{"sample_count":1}`), []byte(`{"sample_seed":1}`),
			"", "", []byte(`{"env":"test"}`),
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
		       stdout, stderr, labels_json
		FROM runs
		WHERE task_id = $1 AND started_at >= $3
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $4`)).
		WithArgs("task-a", 50, sqlmock.AnyArg(), 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"run_id", "task_id", "task_kind", "trigger_type", "run_status", "check_status",
			"started_at", "ended_at", "duration_ms", "error_message", "summary_json", "payload",
			"stdout", "stderr", "labels_json",
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
