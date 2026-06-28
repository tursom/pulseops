package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestPluginOutputWriterHTTP(t *testing.T) {
	t.Parallel()

	var action string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		action, _ = req["action"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"summary": map[string]any{"published": true},
				"findings": []any{
					map[string]any{"reason": "ai_detected", "sample_id": "sample-1"},
				},
			},
		})
	}))
	defer server.Close()

	writer := &pluginOutputWriter{
		cap: pluginmodel.Capability{
			ID:       "external:output_writer:publish",
			PluginID: "external",
			Type:     pluginmodel.CapabilityOutputWriter,
			Name:     "publish_ai",
			Runtime:  "http",
			Endpoint: server.URL,
		},
		cfg: config.PluginsConfig{},
	}
	result, err := writer.Write(context.Background(), OutputSpec{
		Type:   "publish_ai",
		Config: map[string]any{"channel": "ops"},
	}, OutputDeps{
		HTTPClient:    server.Client(),
		CurrentRunID:  "run-1",
		CurrentTaskID: "task-1",
	}, OutputInput{
		RawResponse: `{"status":"bad"}`,
		RunID:       "run-1",
		TaskID:      "task-1",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if action != "write" {
		t.Fatalf("expected write action, got %q", action)
	}
	if result.Summary["published"] != true {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if len(result.Findings) != 1 || result.Findings[0].RunID != "run-1" || result.Findings[0].TaskID != "task-1" {
		t.Fatalf("unexpected findings: %#v", result.Findings)
	}
}

func TestDriverSyncPluginOutputWriterValidation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"valid": true}})
	}))
	defer server.Close()

	driver := NewDriver(nil, nil, nil)
	driver.SyncPluginCapabilities([]pluginmodel.Capability{{
		ID:       "external:output_writer:publish",
		PluginID: "external",
		Type:     pluginmodel.CapabilityOutputWriter,
		Name:     "publish_ai",
		Runtime:  "http",
		Endpoint: server.URL,
	}}, config.PluginsConfig{})
	err := driver.Validate(config.TaskSpec{
		ID:   "ai-task",
		Kind: "ai_analyze",
		Params: map[string]any{
			"data_sources": []any{map[string]any{"type": "run_context"}},
			"prompt":       map[string]any{"text": "hello"},
			"outputs": []any{
				map[string]any{"type": "publish_ai", "config": map[string]any{"channel": "ops"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
}
