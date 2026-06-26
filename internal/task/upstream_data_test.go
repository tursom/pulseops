package task

import (
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
)

func TestFetchSampleDataInlinePayloadSupportsJQ(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(`{"items":[{"price":1.5},{"price":2.5}],"flag":false,"zero":0,"empty":""}`),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", ".items[].price")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available {
		t.Fatalf("expected sample to be available: %#v", resp)
	}
	result, ok := resp.JQResult.([]any)
	if !ok || len(result) != 2 || result[0] != 1.5 || result[1] != 2.5 {
		t.Fatalf("unexpected jq result: %#v", resp.JQResult)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object payload, got %#v", resp.Data)
	}
	if data["flag"] != false || data["zero"] != float64(0) || data["empty"] != "" {
		t.Fatalf("expected falsy JSON values to be preserved, got %#v", data)
	}
}

func TestFetchSampleDataExternalizedPayloadReadsArtifactKey(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		ArtifactRefs: []store.ArtifactRef{{
			Kind:        "payload",
			URI:         "s3://pulseops-artifacts/local/source-task/run-1/payload.json",
			ContentType: "application/json",
		}},
	}}}
	artifacts := &sampleArtifactStore{
		bodies: map[string]string{
			"local/source-task/run-1/payload.json": `{"body":{"status":"ok"}}`,
		},
	}

	resp, err := FetchSampleData(context.Background(), repo, artifacts, "source-task", "payload", ".body.status")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if artifacts.gotKey != "local/source-task/run-1/payload.json" {
		t.Fatalf("expected artifact key, got %q", artifacts.gotKey)
	}
	if !resp.Available || resp.JQResult != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestFetchSampleDataArtifactPayloadAddsDisplayDataAndJQPrefix(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		ArtifactRefs: []store.ArtifactRef{{
			Kind:        "payload",
			URI:         "s3://pulseops-artifacts/local/source-task/run-1/payload.json",
			ContentType: "application/json",
		}},
	}}}
	artifacts := &sampleArtifactStore{
		bodies: map[string]string{
			"local/source-task/run-1/payload.json": `{"body":"{\"data\":{\"goods\":[{\"goods_name\":\"极限竞速：地平线6\"}]}}"}`,
		},
	}

	resp, err := FetchSampleData(context.Background(), repo, artifacts, "source-task", "artifact:payload", ".body | fromjson | .data.goods[0].goods_name")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if artifacts.gotKey != "local/source-task/run-1/payload.json" {
		t.Fatalf("expected artifact key, got %q", artifacts.gotKey)
	}
	if !resp.Available || resp.JQPrefix != ".body | fromjson" || resp.JQResult != "极限竞速：地平线6" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	display, ok := resp.DisplayData.(map[string]any)
	if !ok {
		t.Fatalf("expected parsed display data, got %#v", resp.DisplayData)
	}
	data, ok := display["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", display)
	}
	goods, ok := data["goods"].([]any)
	if !ok || len(goods) != 1 {
		t.Fatalf("expected goods array, got %#v", data["goods"])
	}
}

func TestFetchSampleDataArtifactPayloadNotFoundReturnsUnavailable(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "artifact:payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected unavailable artifact sample: %#v", resp)
	}
	if resp.Reason != "artifact_not_found" {
		t.Fatalf("expected artifact_not_found, got %#v", resp)
	}
}

func TestFetchSampleDataPayloadBodyJSONAddsDisplayDataAndJQPrefix(t *testing.T) {
	t.Parallel()

	payload := `{"body":"{\"code\":0,\"message\":\"成功\",\"data\":{\"goods\":[{\"goods_name\":\"极限竞速：地平线6\"}],\"total\":1}}"}`
	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(payload),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", ".body | fromjson | .data.goods[0].goods_name")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available || resp.JQPrefix != ".body | fromjson" || resp.JQResult != "极限竞速：地平线6" {
		t.Fatalf("unexpected response metadata: %#v", resp)
	}
	display, ok := resp.DisplayData.(map[string]any)
	if !ok {
		t.Fatalf("expected parsed display data, got %#v", resp.DisplayData)
	}
	if display["code"] != float64(0) || display["message"] != "成功" {
		t.Fatalf("unexpected display data: %#v", display)
	}
	result, err := ApplyJQ(".body | fromjson | .data.goods[0].goods_name", resp.Data)
	if err != nil {
		t.Fatalf("apply generated jq: %v", err)
	}
	if result != "极限竞速：地平线6" {
		t.Fatalf("unexpected generated jq result: %#v", result)
	}
}

func TestFetchSampleDataPayloadBodyNonJSONKeepsRawDisplay(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(`{"body":"plain text response"}`),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available {
		t.Fatalf("expected sample to be available: %#v", resp)
	}
	if resp.DisplayData != nil || resp.JQPrefix != "" {
		t.Fatalf("expected raw payload display, got display=%#v prefix=%q", resp.DisplayData, resp.JQPrefix)
	}
}

func TestFetchSampleDataPayloadNotSavedReturnsActionableMessage(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected payload sample to be unavailable: %#v", resp)
	}
	if resp.Reason != "payload_not_saved" || !strings.Contains(resp.Message, "detail/debug") {
		t.Fatalf("expected actionable payload message, got %#v", resp)
	}
}

func TestFetchSampleDataNoSuccessRunReturnsUnavailable(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "failed",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected unavailable sample: %#v", resp)
	}
	if resp.Reason != "no_success_run" {
		t.Fatalf("expected no_success_run, got %#v", resp)
	}
}

type sampleRepository struct {
	runs []store.RunRecord
}

func (r *sampleRepository) Close() error { return nil }
func (r *sampleRepository) UpsertTaskState(context.Context, store.TaskState) error {
	return nil
}
func (r *sampleRepository) DeleteTaskState(context.Context, string) error { return nil }
func (r *sampleRepository) InsertRun(context.Context, store.RunRecord) error {
	return nil
}
func (r *sampleRepository) ListRuns(context.Context, string, int, int, time.Duration) ([]store.RunRecord, error) {
	return r.runs, nil
}
func (r *sampleRepository) CountRuns(context.Context, string, time.Duration) (int, error) {
	return len(r.runs), nil
}
func (r *sampleRepository) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return nil, nil
}
func (r *sampleRepository) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return nil, 0, nil
}
func (r *sampleRepository) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (r *sampleRepository) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (r *sampleRepository) GetRun(context.Context, string, string) (store.RunRecord, error) {
	return store.RunRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) ListArtifactsByRun(context.Context, string, string) ([]store.ArtifactRef, error) {
	return nil, nil
}
func (r *sampleRepository) GetArtifact(context.Context, string) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, sql.ErrNoRows
}
func (r *sampleRepository) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (r *sampleRepository) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error {
	return nil
}
func (r *sampleRepository) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (r *sampleRepository) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (r *sampleRepository) SetMeta(context.Context, string, string) error { return nil }
func (r *sampleRepository) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{}, nil
}
func (r *sampleRepository) SaveGlobalSettings(context.Context, config.GlobalSettings) error {
	return nil
}
func (r *sampleRepository) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (r *sampleRepository) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}
func (r *sampleRepository) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (r *sampleRepository) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *sampleRepository) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *sampleRepository) DeleteTaskDefinition(context.Context, string) error { return nil }
func (r *sampleRepository) ListPipelines(context.Context) ([]config.Pipeline, error) {
	return nil, nil
}
func (r *sampleRepository) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (r *sampleRepository) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (r *sampleRepository) DeletePipeline(context.Context, string) error          { return nil }
func (r *sampleRepository) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (r *sampleRepository) UpdateTaskPipeline(context.Context, string, *string) error {
	return nil
}
func (r *sampleRepository) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return nil, nil
}
func (r *sampleRepository) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return nil, nil
}
func (r *sampleRepository) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (r *sampleRepository) UpsertTaskDependency(context.Context, config.TaskDependency) (config.TaskDependency, error) {
	return config.TaskDependency{}, nil
}
func (r *sampleRepository) DeleteTaskDependency(context.Context, string) error { return nil }

type sampleArtifactStore struct {
	bodies map[string]string
	gotKey string
}

func (s *sampleArtifactStore) Kind() string { return "s3" }
func (s *sampleArtifactStore) Put(context.Context, string, io.Reader, store.ArtifactMeta) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, nil
}
func (s *sampleArtifactStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.gotKey = key
	return io.NopCloser(strings.NewReader(s.bodies[key])), nil
}
func (s *sampleArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (s *sampleArtifactStore) Delete(context.Context, string) error { return nil }
