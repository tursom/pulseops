package evaluator

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
	}, config.PluginsConfig{}, nil, nil)
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

func TestPluginEvaluatorMergesConfigRefsAndOverrides(t *testing.T) {
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
				"check_status": "pass",
				"summary":      map[string]any{"checked": true},
			},
		})
	}))
	defer server.Close()

	capability := pluginmodel.Capability{
		ID:       "external:evaluator:price",
		PluginID: "external",
		Type:     pluginmodel.CapabilityEvaluator,
		Name:     "external_price",
		Runtime:  "http",
		Endpoint: server.URL,
		Defaults: map[string]any{"region": "default", "threshold": 10},
		Config: &pluginmodel.ConfigSchema{
			AllowPluginConfigRef: true,
			Fields: map[string]pluginmodel.ConfigField{
				"region":    {Type: "string"},
				"threshold": {Type: "number", Overridable: true},
				"channel":   {Type: "string"},
				"mode":      {Type: "string", Default: "strict", Overridable: true},
			},
		},
	}
	repo := &evaluatorPluginConfigRepo{
		instances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-evaluator-common": {
				ID:       "cfg-evaluator-common",
				PluginID: "external",
				Scope:    "plugin",
				Status:   "active",
			},
			"cfg-evaluator-price": {
				ID:           "cfg-evaluator-price",
				PluginID:     "external",
				CapabilityID: "external:evaluator:price",
				Scope:        "capability",
				Status:       "active",
			},
		},
		versions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-evaluator-common": {
				InstanceID: "cfg-evaluator-common",
				Version:    3,
				Status:     "active",
				Values:     map[string]any{"region": "ap-south", "threshold": 20},
			},
			"cfg-evaluator-price": {
				InstanceID: "cfg-evaluator-price",
				Version:    4,
				Status:     "active",
				Values:     map[string]any{"channel": "scenario"},
			},
		},
	}

	ev := NewPluginEvaluator(capability, config.PluginsConfig{}, repo, nil)
	result, err := ev.Evaluate(context.Background(), Input{
		TaskID:              "scenario-task",
		TaskParams:          map[string]any{"region": "task-region"},
		PluginConfigRef:     "cfg-evaluator-common",
		CapabilityConfigRef: "cfg-evaluator-price",
		Overrides:           map[string]any{"threshold": 7},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if gotConfig["region"] != "task-region" || gotConfig["channel"] != "scenario" || gotConfig["threshold"] != float64(7) || gotConfig["mode"] != "strict" {
		t.Fatalf("unexpected merged config: %#v", gotConfig)
	}
	if result.PluginConfigVersions["cfg-evaluator-common"] != 3 || result.PluginConfigVersions["cfg-evaluator-price"] != 4 {
		t.Fatalf("unexpected config version trace: %#v", result.PluginConfigVersions)
	}
	if result.PluginTaskOverrides["external_price"] == nil {
		t.Fatalf("expected override trace, got %#v", result.PluginTaskOverrides)
	}
}

type evaluatorPluginConfigRepo struct {
	instances map[string]pluginmodel.ConfigInstanceRecord
	versions  map[string]pluginmodel.ConfigVersionRecord
}

func (r *evaluatorPluginConfigRepo) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	if record, ok := r.instances[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigInstanceRecord{}, fmt.Errorf("plugin config instance %q not found", instanceID)
}

func (r *evaluatorPluginConfigRepo) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	if record, ok := r.versions[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigVersionRecord{}, fmt.Errorf("active plugin config version %q not found", instanceID)
}
