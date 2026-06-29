package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

type Sink interface {
	Name() string
	Kind() string
	Write(ctx context.Context, record store.RunRecord) error
}

type Manager struct {
	logger          *slog.Logger
	sinks           map[string]Sink
	pluginSinks     map[string]Sink
	artifactStore   store.ArtifactStore
	maxPayloadBytes int
	mu              sync.RWMutex
}

func NewManager(logger *slog.Logger, artifactStore store.ArtifactStore, maxPayloadBytes int) *Manager {
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = 4096
	}
	return &Manager{
		logger:          logger,
		sinks:           map[string]Sink{},
		pluginSinks:     map[string]Sink{},
		artifactStore:   artifactStore,
		maxPayloadBytes: maxPayloadBytes,
	}
}

func (m *Manager) Register(sink Sink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	m.sinks[sink.Name()] = sink
	m.mu.Unlock()
}

func (m *Manager) SyncPluginSinks(caps []pluginmodel.Capability, cfg config.PluginsConfig, httpClient *http.Client, configStore any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginSinks = map[string]Sink{}
	for _, cap := range caps {
		if cap.Type != pluginmodel.CapabilityTraceSink {
			continue
		}
		if cap.Runtime != "process" && cap.Runtime != "http" && cap.Runtime != "http_plugin" {
			continue
		}
		m.pluginSinks[cap.ID] = NewPluginSink(cap, cfg, httpClient, configStore, m.artifactStore)
	}
}

func (m *Manager) SetMaxPayloadBytes(maxPayloadBytes int) {
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = 4096
	}
	m.mu.Lock()
	m.maxPayloadBytes = maxPayloadBytes
	m.mu.Unlock()
}

func (m *Manager) Process(ctx context.Context, policy config.TracePolicy, record store.RunRecord) (store.RunRecord, error) {
	if strings.EqualFold(policy.Level, "off") || strings.EqualFold(policy.Level, "none") {
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
	maxBytes := m.maxPayloadBytes
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	var errs []error
	if len(processed.Payload) > maxBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "payload", "payload.json", processed.Payload, "application/json")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["artifact_error"] = err.Error()
			processed.Payload = processed.Payload[:maxBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Payload = nil
		}
	}
	if len(processed.Stdout) > maxBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "stdout", "stdout.txt", []byte(processed.Stdout), "text/plain")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["stdout_artifact_error"] = err.Error()
			processed.Stdout = processed.Stdout[:maxBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Stdout = ""
		}
	}
	if len(processed.Stderr) > maxBytes {
		artifactRef, err := m.writeArtifact(ctx, processed, "stderr", "stderr.txt", []byte(processed.Stderr), "text/plain")
		if err != nil {
			errs = append(errs, err)
			processed.Summary["stderr_artifact_error"] = err.Error()
			processed.Stderr = processed.Stderr[:maxBytes]
		} else {
			processed.ArtifactRefs = append(processed.ArtifactRefs, artifactRef)
			processed.Stderr = ""
		}
	}
	return processed, combineErrors(errs...)
}

func (m *Manager) Dispatch(ctx context.Context, policy config.TracePolicy, record store.RunRecord) {
	if strings.EqualFold(policy.Level, "off") || strings.EqualFold(policy.Level, "none") {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sink := range m.sinks {
		if err := sink.Write(ctx, record); err != nil {
			m.logger.ErrorContext(ctx, "write trace sink failed", "sink", sink.Name(), "task_id", record.TaskID, "err", err)
		}
	}
	for _, sink := range m.pluginSinks {
		if err := sink.Write(ctx, record); err != nil {
			m.logger.ErrorContext(ctx, "write plugin trace sink failed", "sink", sink.Name(), "task_id", record.TaskID, "err", err)
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
	case "detail", "debug":
		// keep everything (will be truncated by maxPayloadBytes in Process)
	default:
		// unknown level, treat as summary
		record.Payload = nil
		record.ArtifactRefs = nil
		record.Stdout = ""
		record.Stderr = ""
	}
	return record
}

func (m *Manager) ReloadSinks(entries []config.SinkEntry, store *store.PostgresStore) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sinks = map[string]Sink{}
	for _, entry := range entries {
		switch entry.Kind {
		case "postgres":
			m.sinks[entry.Name] = NewPostgresSink(entry.Name, store)
		case "webhook":
			timeout := 3 * time.Second
			if entry.Timeout != "" {
				if d, err := time.ParseDuration(entry.Timeout); err == nil {
					timeout = d
				}
			}
			m.sinks[entry.Name] = NewWebhookSink(entry.Name, entry.URL, timeout)
		}
	}
	return nil
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

func NewWebhookSink(name, url string, timeout time.Duration) *WebhookSink {
	client := &http.Client{}
	if timeout > 0 {
		client.Timeout = timeout
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
