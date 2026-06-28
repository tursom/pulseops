package pluginruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestClientLimitsConcurrentCallsPerCapability(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	inFlight := 0
	maxSeen := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxSeen {
			maxSeen = inFlight
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"ok": true}})
	}))
	defer server.Close()

	client := NewClient(pluginmodel.Capability{
		ID:       "external:task_driver:limited",
		PluginID: "external",
		Type:     pluginmodel.CapabilityTaskDriver,
		Name:     "limited",
		Runtime:  "http",
		Endpoint: server.URL,
	}, config.PluginsConfig{MaxConcurrentCalls: 1})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Call(context.Background(), Request{Action: "run"}, Deps{HTTPClient: server.Client()}); err != nil {
				t.Errorf("call: %v", err)
			}
		}()
	}
	wg.Wait()
	if maxSeen != 1 {
		t.Fatalf("expected max concurrency 1, got %d", maxSeen)
	}
}

func TestResolveSecretRefs(t *testing.T) {
	t.Setenv("PULSEOPS_PLUGIN_TOKEN", "secret-value")
	resolved := ResolveSecretRefs(map[string]any{
		"token": "secret://PULSEOPS_PLUGIN_TOKEN",
		"headers": map[string]any{
			"authorization": map[string]any{"secret_ref": "PULSEOPS_PLUGIN_TOKEN"},
		},
		"items": []any{"secret://PULSEOPS_PLUGIN_TOKEN"},
	})
	if resolved["token"] != "secret-value" {
		t.Fatalf("unexpected token: %#v", resolved)
	}
	headers := resolved["headers"].(map[string]any)
	if headers["authorization"] != "secret-value" {
		t.Fatalf("unexpected headers: %#v", headers)
	}
	items := resolved["items"].([]any)
	if items[0] != "secret-value" {
		t.Fatalf("unexpected items: %#v", items)
	}
}
