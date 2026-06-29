package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPluginOutputWriterMergesConfigRefsAndOverrides(t *testing.T) {
	t.Parallel()

	var gotConfig map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotConfig, _ = req["config"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"summary": map[string]any{"published": true},
			},
		})
	}))
	defer server.Close()

	capability := pluginmodel.Capability{
		ID:       "external:output_writer:publish",
		PluginID: "external",
		Type:     pluginmodel.CapabilityOutputWriter,
		Name:     "publish_ai",
		Runtime:  "http",
		Endpoint: server.URL,
		Defaults: map[string]any{
			"format": "json",
		},
		Config: &pluginmodel.ConfigSchema{
			AllowPluginConfigRef: true,
			Fields: map[string]pluginmodel.ConfigField{
				"endpoint": {Type: "string", Required: true},
				"channel":  {Type: "string", Required: true, Overridable: true},
				"format":   {Type: "string"},
			},
		},
	}
	repo := &aiPluginConfigRepo{
		instances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-writer-common": {
				ID:       "cfg-writer-common",
				PluginID: "external",
				Scope:    "plugin",
				Status:   "active",
			},
			"cfg-writer-publish": {
				ID:           "cfg-writer-publish",
				PluginID:     "external",
				CapabilityID: "external:output_writer:publish",
				Scope:        "capability",
				Status:       "active",
			},
		},
		versions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-writer-common": {
				InstanceID: "cfg-writer-common",
				Version:    2,
				Status:     "active",
				Values:     map[string]any{"endpoint": "https://notify.local"},
			},
			"cfg-writer-publish": {
				InstanceID: "cfg-writer-publish",
				Version:    5,
				Status:     "active",
				Values:     map[string]any{"channel": "ops"},
			},
		},
	}
	writer := &pluginOutputWriter{
		cap:          capability,
		cfg:          config.PluginsConfig{},
		configReader: repo,
	}

	result, err := writer.Write(context.Background(), OutputSpec{
		Type:                "publish_ai",
		PluginConfigRef:     "cfg-writer-common",
		CapabilityConfigRef: "cfg-writer-publish",
		Config:              map[string]any{"format": "markdown"},
		Overrides:           map[string]any{"channel": "urgent"},
	}, OutputDeps{HTTPClient: server.Client(), CurrentRunID: "run-1", CurrentTaskID: "task-1"}, OutputInput{RawResponse: "ok"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if gotConfig["endpoint"] != "https://notify.local" || gotConfig["channel"] != "urgent" || gotConfig["format"] != "markdown" {
		t.Fatalf("unexpected merged config: %#v", gotConfig)
	}
	if result.PluginConfigVersions["cfg-writer-common"] != 2 || result.PluginConfigVersions["cfg-writer-publish"] != 5 {
		t.Fatalf("unexpected config version trace: %#v", result.PluginConfigVersions)
	}
	if result.PluginTaskOverrides["publish_ai"] == nil {
		t.Fatalf("expected override trace, got %#v", result.PluginTaskOverrides)
	}
}

func TestPluginOutputWriterRejectsInactiveConfigRef(t *testing.T) {
	t.Parallel()

	writer := &pluginOutputWriter{
		cap: pluginmodel.Capability{
			ID:       "external:output_writer:publish",
			PluginID: "external",
			Type:     pluginmodel.CapabilityOutputWriter,
			Name:     "publish_ai",
			Runtime:  "http",
			Endpoint: "http://plugin.invalid",
			Config: &pluginmodel.ConfigSchema{
				AllowPluginConfigRef: true,
				Fields: map[string]pluginmodel.ConfigField{
					"endpoint": {Type: "string", Required: true},
				},
			},
		},
		cfg: config.PluginsConfig{},
		configReader: &aiPluginConfigRepo{
			instances: map[string]pluginmodel.ConfigInstanceRecord{
				"cfg-disabled": {
					ID:       "cfg-disabled",
					PluginID: "external",
					Scope:    "plugin",
					Status:   "disabled",
				},
			},
		},
	}

	err := writer.Validate(OutputSpec{Type: "publish_ai", PluginConfigRef: "cfg-disabled"})
	if err == nil || !strings.Contains(err.Error(), `plugin config instance "cfg-disabled" is not active`) {
		t.Fatalf("expected inactive config error, got %v", err)
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
