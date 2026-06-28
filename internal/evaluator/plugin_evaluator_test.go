package evaluator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestPluginEvaluatorHTTP(t *testing.T) {
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
				"check_status": "fail",
				"summary":      map[string]any{"mismatch_count": 2},
				"findings": []any{
					map[string]any{"reason": "price_mismatch", "sample_id": "goods-1"},
				},
			},
		})
	}))
	defer server.Close()

	ev := NewPluginEvaluator(pluginmodel.Capability{
		ID:       "external:evaluator:price",
		PluginID: "external",
		Type:     pluginmodel.CapabilityEvaluator,
		Name:     "external_price",
		Runtime:  "http",
		Endpoint: server.URL,
	}, config.PluginsConfig{})
	result, err := ev.Evaluate(context.Background(), Input{
		TaskID:       "scenario-task",
		TaskParams:   map[string]any{"threshold": 1},
		SourceItems:  []map[string]any{{"id": "goods-1"}},
		SampledItems: []map[string]any{{"id": "goods-1"}},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if action != "evaluate" {
		t.Fatalf("expected evaluate action, got %q", action)
	}
	if result.CheckStatus != "fail" || result.Summary["mismatch_count"] != float64(2) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(result.Findings) != 1 || result.Findings[0]["reason"] != "price_mismatch" {
		t.Fatalf("unexpected findings: %#v", result.Findings)
	}
}
