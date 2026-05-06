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

	server := NewServer(config.ServerConfig{Addr: ":8080"}, &fakeTaskManager{}, &fakeRepository{
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

	req := httptest.NewRequest(http.MethodGet, "/tasks/task-a/runs/run-1/artifacts", nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"artifact_id":"artifact-1"`) {
		t.Fatalf("unexpected artifacts response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/artifacts/artifact-1", nil)
	rec = httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"download_url":"https://download.local/object"`) {
		t.Fatalf("unexpected artifact detail response: %d %s", rec.Code, rec.Body.String())
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

type fakeRepository struct {
	artifactsByRun map[string][]store.ArtifactRef
	artifactsByID  map[string]store.ArtifactRef
}

func (r *fakeRepository) Close() error                                           { return nil }
func (r *fakeRepository) UpsertTaskState(context.Context, store.TaskState) error { return nil }
func (r *fakeRepository) DeleteTaskState(context.Context, string) error          { return nil }
func (r *fakeRepository) InsertRun(context.Context, store.RunRecord) error       { return nil }
func (r *fakeRepository) ListRuns(context.Context, string, int) ([]store.RunRecord, error) {
	return nil, nil
}
func (r *fakeRepository) GetRun(context.Context, string, string) (store.RunRecord, error) {
	return store.RunRecord{}, sql.ErrNoRows
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

type fakeArtifactStore struct{}

func (s *fakeArtifactStore) Kind() string { return "s3" }
func (s *fakeArtifactStore) Put(context.Context, string, io.Reader, store.ArtifactMeta) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, nil
}
func (s *fakeArtifactStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (s *fakeArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://download.local/object", nil
}
func (s *fakeArtifactStore) Delete(context.Context, string) error { return nil }
