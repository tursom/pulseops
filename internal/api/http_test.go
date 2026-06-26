package api

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

func TestArtifactEndpoints(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		artifactsByRun: map[string][]store.ArtifactRef{
			"task-a/run-1": {{
				ArtifactID:  "artifact-1",
				Kind:        "payload",
				StorageKind: "s3",
				URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
				ContentType: "application/json",
				SizeBytes:   10,
				SHA256:      "abc",
				PreviewText: "{}",
			}},
		},
		artifactsByID: map[string]store.ArtifactRef{
			"artifact-1": {
				ArtifactID:  "artifact-1",
				Kind:        "payload",
				StorageKind: "s3",
				URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
				ContentType: "application/json",
				SizeBytes:   10,
				SHA256:      "abc",
				PreviewText: "{}",
			},
		},
	}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"artifact_id":"artifact-1"`) {
		t.Fatalf("unexpected artifacts response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/artifacts/artifact-1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"download_url":"https://download.local/object"`) {
		t.Fatalf("unexpected artifact detail response: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRunDetailHydratesPayloadFromArtifact(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		runs: map[string]store.RunRecord{
			"task-a/run-1": {
				RunID:       "run-1",
				TaskID:      "task-a",
				TaskKind:    "http_check",
				TriggerType: "manual",
				RunStatus:   "success",
				CheckStatus: "pass",
				StartedAt:   time.Now(),
				EndedAt:     time.Now(),
				ArtifactRefs: []store.ArtifactRef{{
					ArtifactID:  "artifact-1",
					Kind:        "payload",
					StorageKind: "s3",
					URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
					ContentType: "application/json",
				}},
			},
		},
	}, &fakeArtifactStore{
		bodies: map[string]string{
			"prod/task-a/run-1/payload.json": `{"body":{"status":"ok"}}`,
		},
	}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"payload":{"body":{"status":"ok"}}`) {
		t.Fatalf("expected hydrated payload object, got %s", rec.Body.String())
	}
}

func TestRunDetailKeepsInlinePayload(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		runs: map[string]store.RunRecord{
			"task-a/run-1": {
				RunID:       "run-1",
				TaskID:      "task-a",
				TaskKind:    "http_check",
				TriggerType: "manual",
				RunStatus:   "success",
				CheckStatus: "pass",
				StartedAt:   time.Now(),
				EndedAt:     time.Now(),
				Payload:     []byte(`{"inline":true}`),
				ArtifactRefs: []store.ArtifactRef{{
					ArtifactID:  "artifact-1",
					Kind:        "payload",
					StorageKind: "s3",
					URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
					ContentType: "application/json",
				}},
			},
		},
	}, &fakeArtifactStore{
		bodies: map[string]string{
			"prod/task-a/run-1/payload.json": `{"inline":false}`,
		},
	}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"payload":{"inline":true}`) {
		t.Fatalf("expected inline payload to be preserved, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"inline":false`) {
		t.Fatalf("artifact payload should not override inline payload: %s", rec.Body.String())
	}
}

func TestTaskViewIncludesDefinitionAndDependencies(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{{
			TaskID:  "task-a",
			Name:    "Task A",
			Kind:    "http_check",
			Enabled: true,
			Labels:  map[string]string{"env": "test"},
		}},
		dependencies: []config.TaskDependency{{
			ID:               "dep-1",
			UpstreamTaskID:   "source-a",
			DownstreamTaskID: "task-a",
			Condition:        "run_status == success",
		}},
	}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"config_status":"valid"`) || !strings.Contains(body, `"upstream_count":1`) {
		t.Fatalf("expected task view contract, got %s", body)
	}
	if !strings.Contains(body, `"definition"`) || !strings.Contains(body, `"dependencies"`) {
		t.Fatalf("expected definition and dependencies, got %s", body)
	}
}

func TestDashboardSummaryReturnsAggregates(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{{
			TaskID:  "task-a",
			Name:    "Task A",
			Kind:    "http_check",
			Enabled: true,
			Labels:  map[string]string{"env": "test"},
		}},
		runItems: []store.RunListItem{{
			RunID:       "run-1",
			TaskID:      "task-a",
			TaskName:    "Task A",
			TaskKind:    "http_check",
			RunStatus:   "success",
			CheckStatus: "pass",
			StartedAt:   time.Now(),
			EndedAt:     time.Now(),
		}},
	}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"counts"`) || !strings.Contains(body, `"recent_runs"`) || !strings.Contains(body, `"label_groups"`) {
		t.Fatalf("expected dashboard summary contract, got %s", body)
	}
}

func TestDashboardSummaryReturnsEmptyLists(t *testing.T) {
	t.Parallel()

	handler := Routes("", &emptyTaskManager{}, &fakeRepository{}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, field := range []string{`"anomalies":[]`, `"recent_runs":[]`, `"label_groups":[]`} {
		if !strings.Contains(body, field) {
			t.Fatalf("expected dashboard summary field %s to be an empty list, got %s", field, body)
		}
	}
}

func TestTaskGraphReturnsDependencyEdges(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{
			{TaskID: "source-a", Name: "Source A", Kind: "http_check", Enabled: true},
			{TaskID: "task-a", Name: "Task A", Kind: "data_process", Enabled: true},
		},
		dependencies: []config.TaskDependency{{
			ID:               "dep-1",
			UpstreamTaskID:   "source-a",
			DownstreamTaskID: "task-a",
			Condition:        "check_status == pass",
		}},
	}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/task-graph", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"nodes"`) || !strings.Contains(body, `"edges"`) || !strings.Contains(body, `"upstream_task_id":"source-a"`) {
		t.Fatalf("expected task graph contract, got %s", body)
	}
}

func TestTaskDefinitionValidateRejectsInvalidCondition(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{}, &fakeArtifactStore{}, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/task-defs/validate", strings.NewReader(`{
		"task_id":"task-a",
		"name":"Task A",
		"kind":"http_check",
		"enabled":true,
		"trigger":"on_run",
		"watch_task_id":"source-a",
		"watch_condition":"unknown == value"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"valid":false`) {
		t.Fatalf("expected invalid validation response, got %s", rec.Body.String())
	}
}

func testPlatform() config.PlatformConfigSummary {
	return config.PlatformConfigSummary{Mode: "active", Applied: true}
}

type fakeTaskManager struct{}

type emptyTaskManager struct {
	fakeTaskManager
}

func (m *emptyTaskManager) ListTasks() []store.TaskState {
	return nil
}

func (m *fakeTaskManager) ListTasks() []store.TaskState {
	now := time.Now()
	return []store.TaskState{{
		TaskID:          "task-a",
		Name:            "Task A",
		Kind:            "http_check",
		Enabled:         true,
		Status:          "running",
		Labels:          map[string]string{"env": "test"},
		LastRunAt:       &now,
		LastRunStatus:   "success",
		LastCheckStatus: "pass",
		UpdatedAt:       now,
	}}
}
func (m *fakeTaskManager) GetTask(string) (store.TaskState, bool) {
	return store.TaskState{}, false
}
func (m *fakeTaskManager) RunTask(context.Context, string, task.TriggerType) (store.RunRecord, error) {
	return store.RunRecord{}, nil
}
func (m *fakeTaskManager) ReloadTask(context.Context, string) error           { return nil }
func (m *fakeTaskManager) SetTaskEnabled(context.Context, string, bool) error { return nil }
func (m *fakeTaskManager) UpsertTaskFromDB(context.Context, config.TaskDefinition) (store.TaskState, error) {
	return store.TaskState{}, nil
}
func (m *fakeTaskManager) ValidateTaskDefinition(def config.TaskDefinition) (config.TaskSpec, error) {
	spec, err := def.ToTaskSpec()
	if err != nil {
		return spec, err
	}
	var cfg config.Config
	cfg.Normalize()
	spec.Normalize(cfg)
	return spec, spec.ValidateBasic()
}
func (m *fakeTaskManager) TestRunTaskDefinition(context.Context, config.TaskDefinition) (store.RunRecord, error) {
	return store.RunRecord{RunID: "dry-run-1", TriggerType: "dry_run", RunStatus: "success", CheckStatus: "pass"}, nil
}
func (m *fakeTaskManager) RemoveTaskByID(context.Context, string) error { return nil }

type fakeRepository struct {
	artifactsByRun map[string][]store.ArtifactRef
	artifactsByID  map[string]store.ArtifactRef
	runs           map[string]store.RunRecord
	runItems       []store.RunListItem
	defs           []config.TaskDefinition
	dependencies   []config.TaskDependency
}

func (r *fakeRepository) Close() error                                           { return nil }
func (r *fakeRepository) UpsertTaskState(context.Context, store.TaskState) error { return nil }
func (r *fakeRepository) DeleteTaskState(context.Context, string) error          { return nil }
func (r *fakeRepository) InsertRun(context.Context, store.RunRecord) error       { return nil }
func (r *fakeRepository) ListRuns(context.Context, string, int, int, time.Duration) ([]store.RunRecord, error) {
	return nil, nil
}
func (r *fakeRepository) CountRuns(context.Context, string, time.Duration) (int, error) {
	return 0, nil
}
func (r *fakeRepository) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return r.runItems, nil
}
func (r *fakeRepository) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return r.runItems, len(r.runItems), nil
}
func (r *fakeRepository) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (r *fakeRepository) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (r *fakeRepository) GetRun(_ context.Context, taskID, runID string) (store.RunRecord, error) {
	record, ok := r.runs[taskID+"/"+runID]
	if !ok {
		return store.RunRecord{}, sql.ErrNoRows
	}
	return record, nil
}
func (r *fakeRepository) ListArtifactsByRun(_ context.Context, taskID, runID string) ([]store.ArtifactRef, error) {
	return r.artifactsByRun[taskID+"/"+runID], nil
}
func (r *fakeRepository) GetArtifact(_ context.Context, artifactID string) (store.ArtifactRef, error) {
	artifact, ok := r.artifactsByID[artifactID]
	if !ok {
		return store.ArtifactRef{}, sql.ErrNoRows
	}
	return artifact, nil
}
func (r *fakeRepository) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (r *fakeRepository) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error { return nil }
func (r *fakeRepository) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (r *fakeRepository) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return r.defs, nil
}
func (r *fakeRepository) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *fakeRepository) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *fakeRepository) DeleteTaskDefinition(context.Context, string) error       { return nil }
func (r *fakeRepository) ListPipelines(context.Context) ([]config.Pipeline, error) { return nil, nil }
func (r *fakeRepository) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (r *fakeRepository) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (r *fakeRepository) DeletePipeline(context.Context, string) error          { return nil }
func (r *fakeRepository) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return r.defs, nil
}
func (r *fakeRepository) UpdateTaskPipeline(context.Context, string, *string) error { return nil }
func (r *fakeRepository) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return r.dependencies, nil
}
func (r *fakeRepository) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return r.dependencies, nil
}
func (r *fakeRepository) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (r *fakeRepository) UpsertTaskDependency(_ context.Context, dep config.TaskDependency) (config.TaskDependency, error) {
	return dep, nil
}
func (r *fakeRepository) DeleteTaskDependency(context.Context, string) error { return nil }
func (r *fakeRepository) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (r *fakeRepository) SetMeta(context.Context, string, string) error { return nil }
func (r *fakeRepository) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{MaxPayloadBytes: 4096}, nil
}
func (r *fakeRepository) SaveGlobalSettings(context.Context, config.GlobalSettings) error { return nil }
func (r *fakeRepository) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (r *fakeRepository) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}

type fakeArtifactStore struct {
	bodies map[string]string
}

func (s *fakeArtifactStore) Kind() string { return "s3" }
func (s *fakeArtifactStore) Put(context.Context, string, io.Reader, store.ArtifactMeta) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, nil
}
func (s *fakeArtifactStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if s.bodies != nil {
		return io.NopCloser(strings.NewReader(s.bodies[key])), nil
	}
	return io.NopCloser(strings.NewReader("")), nil
}
func (s *fakeArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://download.local/object", nil
}
func (s *fakeArtifactStore) Delete(context.Context, string) error { return nil }
