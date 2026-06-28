package pluginhook

import (
	"context"
	"encoding/json"
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
