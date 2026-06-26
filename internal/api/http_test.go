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
	}, &fakeArtifactStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

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

type fakeTaskManager struct{}

func (m *fakeTaskManager) ListTasks() []store.TaskState { return nil }
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
func (m *fakeTaskManager) RemoveTaskByID(context.Context, string) error { return nil }

type fakeRepository struct {
	artifactsByRun map[string][]store.ArtifactRef
	artifactsByID  map[string]store.ArtifactRef
	runs           map[string]store.RunRecord
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
	return nil, nil
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
	return nil, nil
}
func (r *fakeRepository) UpdateTaskPipeline(context.Context, string, *string) error { return nil }
func (r *fakeRepository) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (r *fakeRepository) SetMeta(context.Context, string, string) error { return nil }
func (r *fakeRepository) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{MaxPayloadBytes: 4096}, nil
}
func (r *fakeRepository) SaveGlobalSettings(context.Context, config.GlobalSettings) error { return nil }

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
