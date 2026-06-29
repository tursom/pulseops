package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

func TestFetchSampleDataInlinePayloadSupportsJQ(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(`{"items":[{"price":1.5},{"price":2.5}],"flag":false,"zero":0,"empty":""}`),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", ".items[].price")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available {
		t.Fatalf("expected sample to be available: %#v", resp)
	}
	result, ok := resp.JQResult.([]any)
	if !ok || len(result) != 2 || result[0] != 1.5 || result[1] != 2.5 {
		t.Fatalf("unexpected jq result: %#v", resp.JQResult)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object payload, got %#v", resp.Data)
	}
	if data["flag"] != false || data["zero"] != float64(0) || data["empty"] != "" {
		t.Fatalf("expected falsy JSON values to be preserved, got %#v", data)
	}
}

func TestFetchSampleDataExternalizedPayloadReadsArtifactKey(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		ArtifactRefs: []store.ArtifactRef{{
			Kind:        "payload",
			URI:         "s3://pulseops-artifacts/local/source-task/run-1/payload.json",
			ContentType: "application/json",
		}},
	}}}
	artifacts := &sampleArtifactStore{
		bodies: map[string]string{
			"local/source-task/run-1/payload.json": `{"body":{"status":"ok"}}`,
		},
	}

	resp, err := FetchSampleData(context.Background(), repo, artifacts, "source-task", "payload", ".body.status")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if artifacts.gotKey != "local/source-task/run-1/payload.json" {
		t.Fatalf("expected artifact key, got %q", artifacts.gotKey)
	}
	if !resp.Available || resp.JQResult != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestFetchSampleDataArtifactPayloadAddsDisplayDataAndJQPrefix(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		ArtifactRefs: []store.ArtifactRef{{
			Kind:        "payload",
			URI:         "s3://pulseops-artifacts/local/source-task/run-1/payload.json",
			ContentType: "application/json",
		}},
	}}}
	artifacts := &sampleArtifactStore{
		bodies: map[string]string{
			"local/source-task/run-1/payload.json": `{"body":"{\"data\":{\"goods\":[{\"goods_name\":\"极限竞速：地平线6\"}]}}"}`,
		},
	}

	resp, err := FetchSampleData(context.Background(), repo, artifacts, "source-task", "artifact:payload", ".body | fromjson | .data.goods[0].goods_name")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if artifacts.gotKey != "local/source-task/run-1/payload.json" {
		t.Fatalf("expected artifact key, got %q", artifacts.gotKey)
	}
	if !resp.Available || resp.JQPrefix != ".body | fromjson" || resp.JQResult != "极限竞速：地平线6" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	display, ok := resp.DisplayData.(map[string]any)
	if !ok {
		t.Fatalf("expected parsed display data, got %#v", resp.DisplayData)
	}
	data, ok := display["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", display)
	}
	goods, ok := data["goods"].([]any)
	if !ok || len(goods) != 1 {
		t.Fatalf("expected goods array, got %#v", data["goods"])
	}
}

func TestFetchSampleDataArtifactPayloadNotFoundReturnsUnavailable(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "artifact:payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected unavailable artifact sample: %#v", resp)
	}
	if resp.Reason != "artifact_not_found" {
		t.Fatalf("expected artifact_not_found, got %#v", resp)
	}
}

func TestFetchSampleDataPayloadBodyJSONAddsDisplayDataAndJQPrefix(t *testing.T) {
	t.Parallel()

	payload := `{"body":"{\"code\":0,\"message\":\"成功\",\"data\":{\"goods\":[{\"goods_name\":\"极限竞速：地平线6\"}],\"total\":1}}"}`
	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(payload),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", ".body | fromjson | .data.goods[0].goods_name")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available || resp.JQPrefix != ".body | fromjson" || resp.JQResult != "极限竞速：地平线6" {
		t.Fatalf("unexpected response metadata: %#v", resp)
	}
	display, ok := resp.DisplayData.(map[string]any)
	if !ok {
		t.Fatalf("expected parsed display data, got %#v", resp.DisplayData)
	}
	if display["code"] != float64(0) || display["message"] != "成功" {
		t.Fatalf("unexpected display data: %#v", display)
	}
	result, err := ApplyJQ(".body | fromjson | .data.goods[0].goods_name", resp.Data)
	if err != nil {
		t.Fatalf("apply generated jq: %v", err)
	}
	if result != "极限竞速：地平线6" {
		t.Fatalf("unexpected generated jq result: %#v", result)
	}
}

func TestFetchSampleDataPayloadBodyNonJSONKeepsRawDisplay(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
		Payload:   []byte(`{"body":"plain text response"}`),
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if !resp.Available {
		t.Fatalf("expected sample to be available: %#v", resp)
	}
	if resp.DisplayData != nil || resp.JQPrefix != "" {
		t.Fatalf("expected raw payload display, got display=%#v prefix=%q", resp.DisplayData, resp.JQPrefix)
	}
}

func TestFetchSampleDataPayloadNotSavedReturnsActionableMessage(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "success",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected payload sample to be unavailable: %#v", resp)
	}
	if resp.Reason != "payload_not_saved" || !strings.Contains(resp.Message, "detail/debug") {
		t.Fatalf("expected actionable payload message, got %#v", resp)
	}
}

func TestFetchSampleDataNoSuccessRunReturnsUnavailable(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runs: []store.RunRecord{{
		RunID:     "run-1",
		TaskID:    "source-task",
		RunStatus: "failed",
	}}}

	resp, err := FetchSampleData(context.Background(), repo, nil, "source-task", "payload", "")
	if err != nil {
		t.Fatalf("fetch sample data: %v", err)
	}
	if resp.Available {
		t.Fatalf("expected unavailable sample: %#v", resp)
	}
	if resp.Reason != "no_success_run" {
		t.Fatalf("expected no_success_run, got %#v", resp)
	}
}

func TestUpstreamDataRunnerReadsMultipleSourceKeys(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{runsByTask: map[string][]store.RunRecord{
		"source-a": {{
			RunID:     "run-a",
			TaskID:    "source-a",
			RunStatus: "success",
			Summary:   map[string]any{"value": 2},
		}},
		"source-b": {{
			RunID:     "run-b",
			TaskID:    "source-b",
			RunStatus: "success",
			Summary:   map[string]any{"value": 3},
		}},
	}}
	driver := NewUpstreamDataDriver(repo, nil, nil)
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{"key": "left", "task_id": "source-a"},
				map[string]any{"key": "right", "task_id": "source-b"},
			},
			"extract_exprs": []any{
				map[string]any{"field": "left_value", "source_key": "left", "source": "summary", "jq_expr": ".value"},
				map[string]any{"field": "right_value", "source_key": "right", "source": "summary", "jq_expr": ".value"},
			},
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, RunnerDeps{})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary["left_value"] != 2 || result.Summary["right_value"] != 3 {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
}

func TestUpstreamDataRunnerReadsPluginDataSourceAlias(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"inventory": map[string]any{"count": 7}},
		})
	}))
	defer server.Close()

	driver := NewUpstreamDataDriver(&sampleRepository{}, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{{
		Type:     pluginmodel.CapabilityDataSource,
		PluginID: "@test/source",
		Name:     "inventory_source",
		Runtime:  "http",
		Endpoint: server.URL,
	}}, config.PluginsConfig{})
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{"type": "inventory_source", "alias": "inventory"},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary["inventory_count"] != float64(7) {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
}

func TestUpstreamDataRunnerMergesPluginConfigRefsAndOverrides(t *testing.T) {
	t.Parallel()

	captured := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Config map[string]any `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode plugin envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		captured <- envelope.Config
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"inventory": map[string]any{"count": 7}},
		})
	}))
	defer server.Close()

	repo := &sampleRepository{
		pluginConfigInstances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-common": {
				ID:       "cfg-common",
				PluginID: "@test/source",
				Scope:    "plugin",
				Status:   "active",
			},
			"cfg-inventory": {
				ID:           "cfg-inventory",
				PluginID:     "@test/source",
				CapabilityID: "@test/source:data_source:inventory_source",
				Scope:        "capability",
				Status:       "active",
			},
		},
		pluginConfigVersions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-common": {
				InstanceID: "cfg-common",
				Version:    1,
				Status:     "active",
				Values: map[string]any{
					"endpoint": "inventory.service:9090",
					"timeout":  3,
				},
			},
			"cfg-inventory": {
				InstanceID: "cfg-inventory",
				Version:    2,
				Status:     "active",
				Values: map[string]any{
					"service": "yym.inventory.v1.InventoryService",
					"method":  "GetInventory",
					"request": map[string]any{"user_id": "default"},
				},
			},
		},
	}
	driver := NewUpstreamDataDriver(repo, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{testInventoryCapability(server.URL)}, config.PluginsConfig{})

	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":                  "inventory_source",
					"alias":                 "inventory",
					"plugin_config_ref":     "cfg-common",
					"capability_config_ref": "cfg-inventory",
					"overrides": map[string]any{
						"method":  "GetInventoryV2",
						"request": map[string]any{"user_id": "42"},
					},
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary["inventory_count"] != float64(7) {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if result.PluginConfigVersions["cfg-common"] != 1 || result.PluginConfigVersions["cfg-inventory"] != 2 {
		t.Fatalf("unexpected plugin config version trace: %#v", result.PluginConfigVersions)
	}
	if _, ok := result.PluginTaskOverrides["inventory"].(map[string]any); !ok {
		t.Fatalf("expected inventory override trace, got %#v", result.PluginTaskOverrides)
	}
	got := <-captured
	if got["schema_mode"] != "reflection" {
		t.Fatalf("expected schema default, got %#v", got)
	}
	if got["endpoint"] != "inventory.service:9090" || got["service"] != "yym.inventory.v1.InventoryService" || got["method"] != "GetInventoryV2" {
		t.Fatalf("unexpected merged config: %#v", got)
	}
	request, ok := got["request"].(map[string]any)
	if !ok || request["user_id"] != "42" {
		t.Fatalf("expected override request, got %#v", got["request"])
	}
}

func TestUpstreamDataRunnerResolvesPluginConfigAssetsAndSecrets(t *testing.T) {
	t.Parallel()

	type capturedConfig struct {
		Authorization string
		ProtoContent  string
		ProtoPath     string
	}
	captured := make(chan capturedConfig, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Config map[string]any `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode plugin envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		protoPath, _ := envelope.Config["proto"].(string)
		raw, err := os.ReadFile(protoPath)
		if err != nil {
			t.Errorf("read resolved proto file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		authorization, _ := envelope.Config["authorization"].(string)
		captured <- capturedConfig{
			Authorization: authorization,
			ProtoContent:  string(raw),
			ProtoPath:     protoPath,
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"inventory": map[string]any{"count": 7}},
		})
	}))
	defer server.Close()

	ciphertext, encryptionMeta, err := pluginconfig.EncryptSecret("Bearer test-token")
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	repo := &sampleRepository{
		pluginConfigInstances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-inventory": {
				ID:           "cfg-inventory",
				PluginID:     "@test/source",
				CapabilityID: "@test/source:data_source:inventory_source",
				Scope:        "capability",
				Status:       "active",
			},
		},
		pluginConfigVersions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-inventory": {
				InstanceID: "cfg-inventory",
				Version:    2,
				Status:     "active",
				Values: map[string]any{
					"service":       "yym.inventory.v1.InventoryService",
					"method":        "GetInventory",
					"request":       map[string]any{"user_id": "42"},
					"proto":         map[string]any{"asset_id": "inventory-proto"},
					"authorization": "sec-auth",
				},
			},
		},
		pluginAssets: map[string]pluginmodel.AssetRecord{
			"inventory-proto": {
				ID:            "inventory-proto",
				PluginID:      "@test/source",
				CapabilityID:  "@test/source:data_source:inventory_source",
				Scope:         pluginmodel.AssetScopeCapabilityShared,
				Kind:          "proto_files",
				Status:        "active",
				ActiveVersion: 1,
			},
		},
		pluginAssetVersions: map[string]pluginmodel.AssetVersionRecord{
			"inventory-proto": {
				AssetID:    "inventory-proto",
				Version:    1,
				Status:     "active",
				Filename:   "inventory.proto",
				StorageURI: "s3://pulseops-artifacts/plugins/test-source/assets/inventory-proto/1/inventory.proto",
			},
		},
		pluginSecrets: map[string]pluginmodel.SecretRecord{
			"sec-auth": {
				ID:     "sec-auth",
				Status: "active",
			},
		},
		pluginSecretValues: map[string]pluginmodel.SecretValueRecord{
			"sec-auth": {
				SecretID:       "sec-auth",
				Ciphertext:     ciphertext,
				EncryptionMeta: encryptionMeta,
			},
		},
	}
	artifacts := &sampleArtifactStore{
		bodies: map[string]string{
			"plugins/test-source/assets/inventory-proto/1/inventory.proto": `syntax = "proto3";`,
		},
	}
	driver := NewUpstreamDataDriver(repo, artifacts, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{testInventoryCapability(server.URL)}, config.PluginsConfig{})

	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":                  "inventory_source",
					"alias":                 "inventory",
					"capability_config_ref": "cfg-inventory",
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Summary["inventory_count"] != float64(7) {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if result.PluginConfigVersions["cfg-inventory"] != 2 {
		t.Fatalf("unexpected plugin config version trace: %#v", result.PluginConfigVersions)
	}
	if result.PluginAssetVersions["inventory-proto"] != 1 {
		t.Fatalf("unexpected plugin asset version trace: %#v", result.PluginAssetVersions)
	}
	got := <-captured
	if got.Authorization != "Bearer test-token" || got.ProtoContent != `syntax = "proto3";` {
		t.Fatalf("unexpected resolved config: %#v", got)
	}
	if _, err := os.Stat(got.ProtoPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp proto file to be cleaned up, stat error: %v", err)
	}
	if artifacts.gotKey != "plugins/test-source/assets/inventory-proto/1/inventory.proto" {
		t.Fatalf("expected artifact key, got %q", artifacts.gotKey)
	}
}

func TestUpstreamDataDriverRejectsNonOverridablePluginDataSourceOverride(t *testing.T) {
	t.Parallel()

	driver := NewUpstreamDataDriver(&sampleRepository{}, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{testInventoryCapability("http://plugin.example.test")}, config.PluginsConfig{})
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":  "inventory_source",
					"alias": "inventory",
					"overrides": map[string]any{
						"service": "yym.inventory.v1.OtherService",
					},
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	err := driver.Validate(spec)
	if err == nil || !strings.Contains(err.Error(), "config.service is not overridable") {
		t.Fatalf("expected non-overridable override error, got %v", err)
	}
}

func TestUpstreamDataDriverRejectsInvalidPluginDataSourceOverrideType(t *testing.T) {
	t.Parallel()

	driver := NewUpstreamDataDriver(&sampleRepository{}, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{testInventoryCapability("http://plugin.example.test")}, config.PluginsConfig{})
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":  "inventory_source",
					"alias": "inventory",
					"overrides": map[string]any{
						"request": map[string]any{"user_id": 42},
					},
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	err := driver.Validate(spec)
	if err == nil || !strings.Contains(err.Error(), "config.request.user_id must be a string") {
		t.Fatalf("expected override type error, got %v", err)
	}
}

func TestUpstreamDataDriverRejectsMissingPluginConfigRef(t *testing.T) {
	t.Parallel()

	driver := NewUpstreamDataDriver(&sampleRepository{}, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{testInventoryCapability("http://plugin.example.test")}, config.PluginsConfig{})
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":                  "inventory_source",
					"alias":                 "inventory",
					"capability_config_ref": "missing-config",
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	err := driver.Validate(spec)
	if err == nil || !strings.Contains(err.Error(), `load plugin config instance "missing-config"`) {
		t.Fatalf("expected missing config ref error, got %v", err)
	}
}

func TestUpstreamDataDriverRejectsDisallowedPluginConfigRef(t *testing.T) {
	t.Parallel()

	repo := &sampleRepository{
		pluginConfigInstances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-common": {
				ID:       "cfg-common",
				PluginID: "@test/source",
				Scope:    "plugin",
				Status:   "active",
			},
		},
		pluginConfigVersions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-common": {
				InstanceID: "cfg-common",
				Version:    1,
				Status:     "active",
				Values:     map[string]any{"endpoint": "inventory.service:9090"},
			},
		},
	}
	capability := testInventoryCapability("http://plugin.example.test")
	capability.Config.AllowPluginConfigRef = false
	driver := NewUpstreamDataDriver(repo, nil, nil)
	driver.SyncPluginDataSources([]pluginmodel.Capability{capability}, config.PluginsConfig{})
	spec := config.TaskSpec{
		ID:      "processor",
		Kind:    "data_process",
		Enabled: true,
		Params: map[string]any{
			"data_sources": []any{
				map[string]any{
					"type":              "inventory_source",
					"alias":             "inventory",
					"plugin_config_ref": "cfg-common",
				},
			},
			"extract_exprs": []any{
				map[string]any{"field": "inventory_count", "source_key": "inventory", "source": "data", "jq_expr": ".inventory.count"},
			},
		},
	}
	err := driver.Validate(spec)
	if err == nil || !strings.Contains(err.Error(), "does not allow plugin_config_ref") {
		t.Fatalf("expected disallowed plugin_config_ref error, got %v", err)
	}
}

func testInventoryCapability(endpoint string) pluginmodel.Capability {
	return pluginmodel.Capability{
		ID:       "@test/source:data_source:inventory_source",
		Type:     pluginmodel.CapabilityDataSource,
		PluginID: "@test/source",
		Name:     "inventory_source",
		Runtime:  "http",
		Endpoint: endpoint,
		ConfigClasses: map[string]pluginmodel.ConfigClass{
			"InventoryRequest": {
				Fields: map[string]pluginmodel.ConfigField{
					"user_id": {Type: "string", Required: true, Overridable: true},
				},
			},
		},
		Config: &pluginmodel.ConfigSchema{AllowPluginConfigRef: true, Fields: map[string]pluginmodel.ConfigField{
			"schema_mode": {
				Type:    "select",
				Default: "reflection",
				Options: []pluginmodel.ConfigOption{
					{Value: "reflection"},
					{Value: "proto_files"},
				},
			},
			"endpoint": {
				Type:        "string",
				Overridable: false,
			},
			"timeout": {
				Type:        "number",
				Overridable: true,
			},
			"service": {
				Type:        "string",
				Overridable: false,
			},
			"method": {
				Type:        "string",
				Overridable: true,
			},
			"request": {
				Type:        "object",
				Class:       "InventoryRequest",
				Overridable: true,
			},
			"proto": {
				Type:       "file",
				AssetKind:  "proto_files",
				AssetScope: pluginmodel.AssetScopeCapabilityShared,
			},
			"authorization": {
				Type: "secret",
			},
		}},
	}
}

type sampleRepository struct {
	runs                  []store.RunRecord
	runsByTask            map[string][]store.RunRecord
	pluginConfigInstances map[string]pluginmodel.ConfigInstanceRecord
	pluginConfigVersions  map[string]pluginmodel.ConfigVersionRecord
	pluginAssets          map[string]pluginmodel.AssetRecord
	pluginAssetVersions   map[string]pluginmodel.AssetVersionRecord
	pluginSecrets         map[string]pluginmodel.SecretRecord
	pluginSecretValues    map[string]pluginmodel.SecretValueRecord
}

func (r *sampleRepository) Close() error { return nil }
func (r *sampleRepository) UpsertTaskState(context.Context, store.TaskState) error {
	return nil
}
func (r *sampleRepository) DeleteTaskState(context.Context, string) error { return nil }
func (r *sampleRepository) InsertRun(context.Context, store.RunRecord) error {
	return nil
}
func (r *sampleRepository) ListRuns(_ context.Context, taskID string, _ int, _ int, _ time.Duration) ([]store.RunRecord, error) {
	if r.runsByTask != nil {
		return r.runsByTask[taskID], nil
	}
	return r.runs, nil
}
func (r *sampleRepository) CountRuns(context.Context, string, time.Duration) (int, error) {
	return len(r.runs), nil
}
func (r *sampleRepository) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return nil, nil
}
func (r *sampleRepository) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return nil, 0, nil
}
func (r *sampleRepository) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (r *sampleRepository) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (r *sampleRepository) GetRun(context.Context, string, string) (store.RunRecord, error) {
	return store.RunRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) ListArtifactsByRun(context.Context, string, string) ([]store.ArtifactRef, error) {
	return nil, nil
}
func (r *sampleRepository) GetArtifact(context.Context, string) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, sql.ErrNoRows
}
func (r *sampleRepository) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (r *sampleRepository) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error {
	return nil
}
func (r *sampleRepository) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (r *sampleRepository) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (r *sampleRepository) SetMeta(context.Context, string, string) error { return nil }
func (r *sampleRepository) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{}, nil
}
func (r *sampleRepository) SaveGlobalSettings(context.Context, config.GlobalSettings) error {
	return nil
}
func (r *sampleRepository) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (r *sampleRepository) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}
func (r *sampleRepository) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (r *sampleRepository) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *sampleRepository) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *sampleRepository) DeleteTaskDefinition(context.Context, string) error { return nil }
func (r *sampleRepository) ListPipelines(context.Context) ([]config.Pipeline, error) {
	return nil, nil
}
func (r *sampleRepository) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (r *sampleRepository) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (r *sampleRepository) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (r *sampleRepository) DeletePipeline(context.Context, string) error          { return nil }
func (r *sampleRepository) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (r *sampleRepository) UpdateTaskPipeline(context.Context, string, *string) error {
	return nil
}
func (r *sampleRepository) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return nil, nil
}
func (r *sampleRepository) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return nil, nil
}
func (r *sampleRepository) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (r *sampleRepository) UpsertTaskDependency(context.Context, config.TaskDependency) (config.TaskDependency, error) {
	return config.TaskDependency{}, nil
}
func (r *sampleRepository) DeleteTaskDependency(context.Context, string) error { return nil }
func (r *sampleRepository) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	if record, ok := r.pluginConfigInstances[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigInstanceRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	if record, ok := r.pluginConfigVersions[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigVersionRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) GetPluginAsset(_ context.Context, assetID string) (pluginmodel.AssetRecord, error) {
	if record, ok := r.pluginAssets[assetID]; ok {
		return record, nil
	}
	return pluginmodel.AssetRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) GetPluginAssetVersion(_ context.Context, assetID string, version int) (pluginmodel.AssetVersionRecord, error) {
	record, ok := r.pluginAssetVersions[assetID]
	if !ok || record.Version != version {
		return pluginmodel.AssetVersionRecord{}, sql.ErrNoRows
	}
	return record, nil
}
func (r *sampleRepository) GetActivePluginAssetVersion(_ context.Context, assetID string) (pluginmodel.AssetVersionRecord, error) {
	if record, ok := r.pluginAssetVersions[assetID]; ok {
		return record, nil
	}
	return pluginmodel.AssetVersionRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) GetPluginSecret(_ context.Context, secretID string) (pluginmodel.SecretRecord, error) {
	if record, ok := r.pluginSecrets[secretID]; ok {
		return record, nil
	}
	return pluginmodel.SecretRecord{}, sql.ErrNoRows
}
func (r *sampleRepository) GetPluginSecretValue(_ context.Context, secretID string) (pluginmodel.SecretValueRecord, error) {
	if record, ok := r.pluginSecretValues[secretID]; ok {
		return record, nil
	}
	return pluginmodel.SecretValueRecord{}, sql.ErrNoRows
}

type sampleArtifactStore struct {
	bodies map[string]string
	gotKey string
}

func (s *sampleArtifactStore) Kind() string { return "s3" }
func (s *sampleArtifactStore) Put(context.Context, string, io.Reader, store.ArtifactMeta) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, nil
}
func (s *sampleArtifactStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.gotKey = key
	return io.NopCloser(strings.NewReader(s.bodies[key])), nil
}
func (s *sampleArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (s *sampleArtifactStore) Delete(context.Context, string) error { return nil }
