package trace

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

func TestManagerDispatchesPluginTraceSink(t *testing.T) {
	t.Parallel()

	var action string
	var taskID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		action, _ = req["action"].(string)
		input, _ := req["input"].(map[string]any)
		record, _ := input["record"].(map[string]any)
		taskID, _ = record["task_id"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"written": true}})
	}))
	defer server.Close()

	manager := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, 4096)
	manager.SyncPluginSinks([]pluginmodel.Capability{{
		ID:       "external:trace_sink:notify",
		PluginID: "external",
		Type:     pluginmodel.CapabilityTraceSink,
		Name:     "notify",
		Runtime:  "http",
		Endpoint: server.URL,
	}}, config.PluginsConfig{}, server.Client())
	manager.Dispatch(context.Background(), config.TracePolicy{Level: "detail"}, store.RunRecord{
		RunID:       "run-1",
		TaskID:      "task-1",
		TriggerType: "manual",
		StartedAt:   time.Now(),
	})
	if action != "write_trace" {
		t.Fatalf("expected write_trace action, got %q", action)
	}
	if taskID != "task-1" {
		t.Fatalf("expected task-1, got %q", taskID)
	}
}
