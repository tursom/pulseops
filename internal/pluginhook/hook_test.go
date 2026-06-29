package pluginhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestManagerDispatchesHTTPHook(t *testing.T) {
	t.Parallel()

	var action string
	var eventType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		action, _ = req["action"].(string)
		input, _ := req["input"].(map[string]any)
		event, _ := input["event"].(map[string]any)
		eventType, _ = event["type"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"handled": true}})
	}))
	defer server.Close()

	manager := NewManager(config.PluginsConfig{}, server.Client(), nil)
	manager.SyncPluginHooks([]pluginmodel.Capability{{
		ID:       "external:hook:run_finished",
		PluginID: "external",
		Type:     pluginmodel.CapabilityHook,
		Name:     "run_finished",
		Runtime:  "http",
		Endpoint: server.URL,
	}}, config.PluginsConfig{})

	manager.Dispatch(context.Background(), Event{
		Type:   EventRunFinished,
		TaskID: "task-1",
		RunID:  "run-1",
	})
	if action != "handle_event" {
		t.Fatalf("expected handle_event action, got %q", action)
	}
	if eventType != EventRunFinished {
		t.Fatalf("expected event %q, got %q", EventRunFinished, eventType)
	}
}

func TestManagerDispatchesHookWithDefaultConfigRef(t *testing.T) {
	t.Parallel()

	var gotConfig map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotConfig, _ = req["config"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"handled": true}})
	}))
	defer server.Close()

	repo := &hookPluginConfigRepo{
		instances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-hook-prod": {
				ID:           "cfg-hook-prod",
				PluginID:     "external",
				CapabilityID: "external:hook:run_finished",
				Scope:        "capability",
				Status:       "active",
			},
		},
		versions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-hook-prod": {
				InstanceID: "cfg-hook-prod",
				Version:    8,
				Status:     "active",
				Values: map[string]any{
					"webhook_url": "https://hooks.local/run-finished",
					"severity":    "warning",
				},
			},
		},
	}

	manager := NewManager(config.PluginsConfig{}, server.Client(), nil)
	manager.SetConfigStore(repo, nil)
	manager.SyncPluginHooks([]pluginmodel.Capability{{
		ID:       "external:hook:run_finished",
		PluginID: "external",
		Type:     pluginmodel.CapabilityHook,
		Name:     "run_finished",
		Runtime:  "http",
		Endpoint: server.URL,
		Defaults: map[string]any{
			"config_ref": "cfg-hook-prod",
			"config": map[string]any{
				"severity": "info",
			},
		},
		Config: &pluginmodel.ConfigSchema{
			Fields: map[string]pluginmodel.ConfigField{
				"webhook_url": {Type: "string", Required: true},
				"severity":    {Type: "string"},
			},
		},
	}}, config.PluginsConfig{})

	manager.Dispatch(context.Background(), Event{Type: EventRunFinished, TaskID: "task-1", RunID: "run-1"})
	if gotConfig["webhook_url"] != "https://hooks.local/run-finished" || gotConfig["severity"] != "warning" {
		t.Fatalf("unexpected hook config: %#v", gotConfig)
	}
}

type hookPluginConfigRepo struct {
	instances map[string]pluginmodel.ConfigInstanceRecord
	versions  map[string]pluginmodel.ConfigVersionRecord
}

func (r *hookPluginConfigRepo) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	if record, ok := r.instances[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigInstanceRecord{}, fmt.Errorf("plugin config instance %q not found", instanceID)
}

func (r *hookPluginConfigRepo) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	if record, ok := r.versions[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigVersionRecord{}, fmt.Errorf("active plugin config version %q not found", instanceID)
}
