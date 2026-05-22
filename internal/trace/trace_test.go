package trace

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
)

func TestApplyPolicySummaryMasksAndStripsVerboseFields(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"token":"secret","nested":{"password":"hidden"}}`)
	record := store.RunRecord{
		Summary: map[string]any{
			"token": "secret",
		},
		Payload: payload,
		Stdout:  "hello",
		Stderr:  "oops",
	}

	trimmed := applyPolicy(config.TracePolicy{
		Level:      "summary",
		MaskFields: []string{"token", "password"},
	}, record)

	if trimmed.Payload != nil {
		t.Fatalf("expected summary level to drop payload")
	}
	if trimmed.Stdout != "" || trimmed.Stderr != "" {
		t.Fatalf("expected summary level to drop stdout/stderr")
	}
	if trimmed.Summary["token"] != "***" {
		t.Fatalf("expected summary token masked, got %#v", trimmed.Summary)
	}
}

func TestManagerProcessExternalizesLargePayloadAfterMasking(t *testing.T) {
	t.Parallel()

	artifactStore := &fakeArtifactStore{}
	manager := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)), artifactStore, 20)
	record := store.RunRecord{
		RunID:     "run-1",
		TaskID:    "task-1",
		StartedAt: time.Date(2026, 4, 27, 1, 0, 0, 0, time.UTC),
		Summary:   map[string]any{},
		Payload:   []byte(`{"token":"secret","message":"abcdefghijklmnopqrstuvwxyz"}`),
	}

	processed, err := manager.Process(context.Background(), config.TracePolicy{
		Level:      "detail",
		MaskFields: []string{"token"},
	}, record)
	if err != nil {
		t.Fatalf("process trace record: %v", err)
	}
	if processed.Payload != nil {
		t.Fatalf("expected payload to be externalized, got %q", string(processed.Payload))
	}
	if len(processed.ArtifactRefs) != 1 {
		t.Fatalf("expected one artifact ref, got %#v", processed.ArtifactRefs)
	}
	if !strings.Contains(artifactStore.lastBody, `"token":"***"`) {
		t.Fatalf("expected masked payload to be uploaded, got %q", artifactStore.lastBody)
	}
}

func TestManagerDispatchWritesRegisteredSink(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := NewManager(logger, &fakeArtifactStore{}, 4096)
	sink := &captureSink{name: "capture", kind: "webhook"}
	manager.Register(sink)

	manager.Dispatch(context.Background(), config.TracePolicy{
		Level: "detail",
	}, store.RunRecord{RunID: "run-1", TaskID: "task-1"})

	if sink.called != 1 {
		t.Fatalf("expected sink to be called once, got %d", sink.called)
	}
}

type fakeArtifactStore struct {
	lastKey  string
	lastBody string
}

func (s *fakeArtifactStore) Kind() string { return "s3" }

func (s *fakeArtifactStore) Put(_ context.Context, key string, body io.Reader, meta store.ArtifactMeta) (store.ArtifactRef, error) {
	raw, _ := io.ReadAll(body)
	s.lastKey = key
	s.lastBody = string(raw)
	return store.ArtifactRef{
		ArtifactID:  "artifact-1",
		Kind:        meta.Kind,
		StorageKind: "s3",
		URI:         "s3://bucket/" + key,
		ContentType: meta.ContentType,
		SizeBytes:   int64(len(raw)),
		SHA256:      "abc",
		PreviewText: s.lastBody,
	}, nil
}

func (s *fakeArtifactStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (s *fakeArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://download.local/object", nil
}

func (s *fakeArtifactStore) Delete(context.Context, string) error { return nil }

func (s *fakeArtifactStore) BuildObjectKey(taskID, runID, artifactName string, startedAt time.Time) string {
	return strings.Join([]string{taskID, startedAt.Format("2006/01/02"), runID, artifactName}, "/")
}

type captureSink struct {
	name   string
	kind   string
	called int
}

func (s *captureSink) Name() string { return s.name }
func (s *captureSink) Kind() string { return s.kind }

func (s *captureSink) Write(_ context.Context, _ store.RunRecord) error {
	s.called++
	return nil
}

var _ = os.Stdout
