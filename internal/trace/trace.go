package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
)

type Sink interface {
	Name() string
	Kind() string
	Write(ctx context.Context, record store.RunRecord) error
}

type Manager struct {
	logger        *slog.Logger
	sinks         map[string]Sink
	artifactStore store.ArtifactStore
}

func NewManager(logger *slog.Logger, artifactStore store.ArtifactStore) *Manager {
	return &Manager{
		logger:        logger,
		sinks:         map[string]Sink{},
		artifactStore: artifactStore,
	}
}

func (m *Manager) Register(sink Sink) {
	if sink == nil {
		return
	}
	m.sinks[sink.Name()] = sink
}

func (m *Manager) Process(ctx context.Context, policy config.TracePolicy, record store.RunRecord) (store.RunRecord, error) {
	if !policy.Enabled || strings.EqualFold(policy.Level, "none") {
		record.Payload = nil
		record.Stdout = ""
		record.Stderr = ""
		record.ArtifactRefs = nil
		return record, nil
	}
	processed := applyPolicy(policy, record)
	if processed.Summary == nil {
		processed.Summary = map[string]any{}
	}
	var errs []error
	if policy.StoreResultPayload && len(processed.Payload) > 0 && policy.MaxPayloadBytes > 0 && len(processed.Payload) > policy.MaxPayloadBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "payload", "payload.json", processed.Payload, "application/json")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["artifact_error"] = err.Error()
			processed.Payload = processed.Payload[:policy.MaxPayloadBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Payload = nil
		}
	}
	if policy.StoreStdout && policy.MaxPayloadBytes > 0 && len(processed.Stdout) > policy.MaxPayloadBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "stdout", "stdout.txt", []byte(processed.Stdout), "text/plain")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["stdout_artifact_error"] = err.Error()
			processed.Stdout = processed.Stdout[:policy.MaxPayloadBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Stdout = ""
		}
	}
	if policy.StoreStderr && policy.MaxPayloadBytes > 0 && len(processed.Stderr) > policy.MaxPayloadBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "stderr", "stderr.txt", []byte(processed.Stderr), "text/plain")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["stderr_artifact_error"] = err.Error()
			processed.Stderr = processed.Stderr[:policy.MaxPayloadBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Stderr = ""
		}
	}
	return processed, combineErrors(errs...)
}

func (m *Manager) Dispatch(ctx context.Context, policy config.TracePolicy, record store.RunRecord) {
	if !policy.Enabled || strings.EqualFold(policy.Level, "none") {
		return
	}
	for _, sinkName := range policy.Sinks {
		sink, ok := m.sinks[sinkName]
		if !ok {
			m.logger.WarnContext(ctx, "trace sink not found", "sink", sinkName, "task_id", record.TaskID)
			continue
		}
		if err := sink.Write(ctx, record); err != nil {
			m.logger.ErrorContext(ctx, "write trace sink failed", "sink", sinkName, "task_id", record.TaskID, "err", err)
		}
	}
}

func applyPolicy(policy config.TracePolicy, record store.RunRecord) store.RunRecord {
	record.Summary = maskMap(record.Summary, policy.MaskFields)
	record.Payload = maskPayload(record.Payload, policy.MaskFields)
	level := strings.ToLower(policy.Level)
	switch level {
	case "summary":
		record.Payload = nil
		record.ArtifactRefs = nil
		record.Stdout = ""
		record.Stderr = ""
	case "detail":
		if !policy.StoreResultPayload {
			record.Payload = nil
		}
		if !policy.StoreStdout {
			record.Stdout = ""
		}
		if !policy.StoreStderr {
			record.Stderr = ""
		}
	case "debug":
		if !policy.StoreResultPayload {
			record.Payload = nil
		}
	default:
		if !policy.StoreResultPayload {
			record.Payload = nil
		}
		if !policy.StoreStdout {
			record.Stdout = ""
		}
		if !policy.StoreStderr {
			record.Stderr = ""
		}
	}
	return record
}

func (m *Manager) writeArtifact(ctx context.Context, record store.RunRecord, kind, artifactName string, body []byte, contentType string) (store.ArtifactRef, error) {
	if m.artifactStore == nil {
		return store.ArtifactRef{}, fmt.Errorf("artifact store is not configured")
	}
	type keyBuilder interface {
		BuildObjectKey(taskID, runID, artifactName string, startedAt time.Time) string
	}
	builder, ok := m.artifactStore.(keyBuilder)
	if !ok {
		return store.ArtifactRef{}, fmt.Errorf("artifact store does not support key building")
	}
	key := builder.BuildObjectKey(record.TaskID, record.RunID, artifactName, record.StartedAt)
	return m.artifactStore.Put(ctx, key, bytes.NewReader(body), store.ArtifactMeta{
		TaskID:       record.TaskID,
		RunID:        record.RunID,
		ArtifactName: artifactName,
		Kind:         kind,
		ContentType:  contentType,
	})
}

func maskMap(input map[string]any, fields []string) map[string]any {
	if len(input) == 0 || len(fields) == 0 {
		return input
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return input
	}
	maskAny(decoded, toFieldSet(fields))
	if result, ok := decoded.(map[string]any); ok {
		return result
	}
	return input
}

func maskPayload(payload []byte, fields []string) []byte {
	if len(payload) == 0 || len(fields) == 0 {
		return payload
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return payload
	}
	maskAny(decoded, toFieldSet(fields))
	raw, err := json.Marshal(decoded)
	if err != nil {
		return payload
	}
	return raw
}

func maskAny(value any, fields map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			if _, ok := fields[strings.ToLower(key)]; ok {
				typed[key] = "***"
				continue
			}
			maskAny(inner, fields)
		}
	case []any:
		for _, inner := range typed {
			maskAny(inner, fields)
		}
	}
}

func toFieldSet(fields []string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[strings.ToLower(field)] = struct{}{}
	}
	return set
}

type PostgresSink struct {
	name  string
	store store.Repository
}

func NewPostgresSink(name string, st store.Repository) *PostgresSink {
	return &PostgresSink{name: name, store: st}
}

func (s *PostgresSink) Name() string {
	return s.name
}

func (s *PostgresSink) Kind() string {
	return "postgres"
}

func (s *PostgresSink) Write(ctx context.Context, record store.RunRecord) error {
	return s.store.InsertRun(ctx, record)
}

type WebhookSink struct {
	name   string
	url    string
	client *http.Client
}

func NewWebhookSink(name, url string, timeout config.Duration) *WebhookSink {
	client := &http.Client{}
	if timeout.Duration > 0 {
		client.Timeout = timeout.Duration
	}
	return &WebhookSink{name: name, url: url, client: client}
}

func (s *WebhookSink) Name() string {
	return s.name
}

func (s *WebhookSink) Kind() string {
	return "webhook"
}

func (s *WebhookSink) Write(ctx context.Context, record store.RunRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal webhook record: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("call webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func combineErrors(errs ...error) error {
	var filtered []error
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return fmt.Errorf("multiple trace errors: %v", filtered)
}
