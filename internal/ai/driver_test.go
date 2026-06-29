package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

func TestRenderTemplate(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"run_context": store.RunRecord{
			CheckStatus: "fail",
			RunStatus:   "success",
			DurationMS:  1234,
			StartedAt:   time.Now(),
		},
		"run_history": []store.RunRecord{
			{CheckStatus: "pass", RunStatus: "success", DurationMS: 100},
			{CheckStatus: "pass", RunStatus: "success", DurationMS: 200},
			{CheckStatus: "fail", RunStatus: "failed", DurationMS: 500},
		},
	}

	t.Run("simple template", func(t *testing.T) {
		prompt := PromptSpec{Text: "Status: {{ .DataSources.run_context.CheckStatus }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "Status: fail") {
			t.Fatalf("unexpected result: %s", result)
		}
	})

	t.Run("json function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ json .DataSources.run_context }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "check_status") {
			t.Fatalf("json missing check_status: %s", result)
		}
	})

	t.Run("len function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ len .DataSources.run_history }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "3") {
			t.Fatalf("expected 3, got: %s", result)
		}
	})

	t.Run("avg function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ avg .DataSources.run_history \"DurationMS\" }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "266") {
			t.Fatalf("expected avg ~266, got: %s", result)
		}
	})

	t.Run("filter function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ len (filter .DataSources.run_history \"CheckStatus\" \"fail\") }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "1") {
			t.Fatalf("expected 1 fail, got: %s", result)
		}
	})

	t.Run("failures function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ len (failures .DataSources.run_history) }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "1") {
			t.Fatalf("expected 1 failure, got: %s", result)
		}
	})

	t.Run("table function", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ table .DataSources.run_history \"CheckStatus\" \"RunStatus\" \"DurationMS\" }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "| pass | success | 100 |") {
			t.Fatalf("table missing row, got: %s", result)
		}
	})

	t.Run("count alias", func(t *testing.T) {
		prompt := PromptSpec{Text: "{{ count .DataSources.run_history }}"}
		result, err := renderTemplate(prompt, data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(result, "3") {
			t.Fatalf("expected 3, got: %s", result)
		}
	})

	t.Run("empty prompt rejected", func(t *testing.T) {
		prompt := PromptSpec{Text: ""}
		_, err := renderTemplate(prompt, data)
		if err == nil {
			t.Fatal("expected error for empty prompt")
		}
	})
}

func TestTryParseJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid json object", func(t *testing.T) {
		result := tryParseJSON(`{"status":"ok"}`)
		if result == nil {
			t.Fatal("expected parsed result")
		}
		if result["status"] != "ok" {
			t.Fatalf("expected status=ok, got %v", result["status"])
		}
	})

	t.Run("json object with whitespace", func(t *testing.T) {
		result := tryParseJSON(`  {"x": 1}  `)
		if result == nil || result["x"].(float64) != 1 {
			t.Fatalf("unexpected result: %v", result)
		}
	})

	t.Run("json in single line among text", func(t *testing.T) {
		result := tryParseJSON("some text\n{\"key\":\"value\"}\nmore text")
		if result == nil {
			t.Fatal("expected parsed JSON from second line")
		}
		if result["key"] != "value" {
			t.Fatalf("expected key=value, got %v", result)
		}
	})

	t.Run("non-json returns nil", func(t *testing.T) {
		result := tryParseJSON("just plain text")
		if result != nil {
			t.Fatalf("expected nil for plain text, got %v", result)
		}
	})

	t.Run("array returns nil", func(t *testing.T) {
		result := tryParseJSON(`[1,2,3]`)
		if result != nil {
			t.Fatalf("array should return nil, got %v", result)
		}
	})
}

func TestTryParseFindingJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid json", func(t *testing.T) {
		result := tryParseFindingJSON(`{"status":"abnormal","reason":"price mismatch"}`)
		if result["status"] != "abnormal" {
			t.Fatalf("expected abnormal, got %v", result["status"])
		}
	})

	t.Run("json with markdown fences", func(t *testing.T) {
		result := tryParseFindingJSON("```json\n{\"status\":\"normal\"}\n```")
		if result["status"] != "normal" {
			t.Fatalf("expected normal, got %v", result["status"])
		}
	})

	t.Run("plain text becomes raw_response", func(t *testing.T) {
		result := tryParseFindingJSON("just some text")
		if result["raw_response"] != "just some text" {
			t.Fatalf("expected raw_response, got %v", result)
		}
	})
}

func TestDriverValidate(t *testing.T) {
	t.Parallel()

	d := NewDriver(nil, nil, slog.Default())

	t.Run("requires data sources", func(t *testing.T) {
		spec := config.TaskSpec{
			ID:   "test",
			Kind: "ai_analyze",
			Params: map[string]any{
				"prompt": map[string]any{"text": "hello"},
			},
		}
		err := d.Validate(spec)
		if err == nil || !strings.Contains(err.Error(), "data source") {
			t.Fatalf("expected data source error, got: %v", err)
		}
	})

	t.Run("requires prompt text", func(t *testing.T) {
		spec := config.TaskSpec{
			ID:   "test",
			Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{"type": "run_context"}},
				"prompt":       map[string]any{},
			},
		}
		err := d.Validate(spec)
		if err == nil || !strings.Contains(err.Error(), "prompt.text") {
			t.Fatalf("expected prompt.text error, got: %v", err)
		}
	})

	t.Run("valid spec passes", func(t *testing.T) {
		spec := config.TaskSpec{
			ID:   "test",
			Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{"type": "run_context"}},
				"prompt":       map[string]any{"text": "hello"},
			},
		}
		err := d.Validate(spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestFindingsWriter(t *testing.T) {
	t.Parallel()

	writer := &findingsWriter{}
	deps := OutputDeps{CurrentRunID: "run-1", CurrentTaskID: "task-a"}

	t.Run("single finding as json object", func(t *testing.T) {
		input := OutputInput{RawResponse: `{"reason":"price_mismatch","sample_id":"goods-1"}`, ParsedJSON: nil}
		result, err := writer.Write(context.Background(), OutputSpec{}, deps, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if len(result.Findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(result.Findings))
		}
		if result.Findings[0].RunID != "run-1" || result.Findings[0].TaskID != "task-a" {
			t.Fatalf("finding missing run/task id: %+v", result.Findings[0])
		}
	})

	t.Run("findings array", func(t *testing.T) {
		input := OutputInput{RawResponse: `[{"reason":"a","sample_id":"1"},{"reason":"b","sample_id":"2"}]`}
		result, err := writer.Write(context.Background(), OutputSpec{}, deps, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if len(result.Findings) != 2 {
			t.Fatalf("expected 2 findings, got %d", len(result.Findings))
		}
	})

	t.Run("non-json returns empty", func(t *testing.T) {
		input := OutputInput{RawResponse: "not json at all"}
		result, err := writer.Write(context.Background(), OutputSpec{}, deps, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if len(result.Findings) != 0 {
			t.Fatalf("expected 0 findings, got %d", len(result.Findings))
		}
	})

	t.Run("parsed json takes priority", func(t *testing.T) {
		input := OutputInput{
			RawResponse: "ignored",
			ParsedJSON:  map[string]any{"reason": "from_parsed", "sample_id": "x"},
		}
		result, err := writer.Write(context.Background(), OutputSpec{}, deps, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if len(result.Findings) != 1 || result.Findings[0].Reason != "from_parsed" {
			t.Fatalf("expected parsed result, got %+v", result.Findings)
		}
	})
}

func TestSummaryWriter(t *testing.T) {
	t.Parallel()

	writer := &summaryWriter{}

	t.Run("extracts field from parsed json", func(t *testing.T) {
		input := OutputInput{ParsedJSON: map[string]any{"ai_diagnosis": "normal"}}
		spec := OutputSpec{Config: map[string]any{"field": "ai_diagnosis"}}
		result, err := writer.Write(context.Background(), spec, OutputDeps{}, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if result.Summary["ai_diagnosis"] != "normal" {
			t.Fatalf("expected normal, got %v", result.Summary["ai_diagnosis"])
		}
	})

	t.Run("default field name", func(t *testing.T) {
		input := OutputInput{ParsedJSON: map[string]any{"ai_analysis": "result text"}}
		result, err := writer.Write(context.Background(), OutputSpec{}, OutputDeps{}, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if result.Summary["ai_analysis"] != "result text" {
			t.Fatalf("expected result text, got %v", result.Summary["ai_analysis"])
		}
	})

	t.Run("fallback to raw response when no parsed json", func(t *testing.T) {
		input := OutputInput{RawResponse: "raw analysis text"}
		spec := OutputSpec{Config: map[string]any{"field": "my_field"}}
		result, err := writer.Write(context.Background(), spec, OutputDeps{}, input)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if result.Summary["my_field"] != "raw analysis text" {
			t.Fatalf("expected raw text, got %v", result.Summary["my_field"])
		}
	})
}

func TestClientConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := ClientConfig{}
	c := NewClient(cfg)
	if c.maxTokens != 4096 {
		t.Fatalf("expected default 4096 maxTokens, got %d", c.maxTokens)
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Fatalf("expected default 30s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestDataSourceNames(t *testing.T) {
	t.Parallel()

	reg := NewDataSourceRegistry()

	s, ok := reg.Get("run_context")
	if !ok || s.Name() != "run_context" {
		t.Fatal("run_context source not registered")
	}

	s, ok = reg.Get("run_history")
	if !ok || s.Name() != "run_history" {
		t.Fatal("run_history source not registered")
	}

	s, ok = reg.Get("previous_analysis")
	if !ok || s.Name() != "previous_analysis" {
		t.Fatal("previous_analysis source not registered")
	}

	s, ok = reg.Get("http_call")
	if !ok || s.Name() != "http_call" {
		t.Fatal("http_call source not registered")
	}

	s, ok = reg.Get("upstream_output")
	if !ok || s.Name() != "upstream_output" {
		t.Fatal("upstream_output source not registered")
	}
}

func TestDriverValidateAliasCollision(t *testing.T) {
	t.Parallel()
	d := NewDriver(nil, nil, slog.Default())

	t.Run("alias colliding with builtin", func(t *testing.T) {
		spec := config.TaskSpec{
			ID: "test", Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{
					"type": "run_context", "alias": "run_history",
				}},
				"prompt": map[string]any{"text": "hello"},
			},
		}
		err := d.Validate(spec)
		if err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("expected alias collision error, got: %v", err)
		}
	})

	t.Run("alias not colliding is fine", func(t *testing.T) {
		spec := config.TaskSpec{
			ID: "test", Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{
					"type": "run_context", "alias": "my_custom_name",
				}},
				"prompt": map[string]any{"text": "hello"},
			},
		}
		err := d.Validate(spec)
		if err != nil {
			t.Fatalf("unexpected error for valid alias: %v", err)
		}
	})
}

func TestDriverValidateUnknownType(t *testing.T) {
	t.Parallel()
	d := NewDriver(nil, nil, slog.Default())

	t.Run("unknown type rejected", func(t *testing.T) {
		spec := config.TaskSpec{
			ID: "test", Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{
					"type": "nonexistent_source",
				}},
				"prompt": map[string]any{"text": "hello"},
			},
		}
		err := d.Validate(spec)
		if err == nil || !strings.Contains(err.Error(), "unknown data source type") {
			t.Fatalf("expected unknown type error, got: %v", err)
		}
	})
}

func TestDriverSyncPluginDataSource(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"value": "from-plugin"},
		})
	}))
	defer server.Close()

	d := NewDriver(nil, nil, slog.Default())
	d.SyncPluginDataSources([]pluginmodel.Capability{{
		Type:     pluginmodel.CapabilityDataSource,
		PluginID: "@test/source",
		Name:     "plugin_inventory",
		Runtime:  "http",
		Endpoint: server.URL,
	}}, config.PluginsConfig{})

	spec := config.TaskSpec{
		ID:   "test",
		Kind: "ai_analyze",
		Params: map[string]any{
			"data_sources": []any{map[string]any{
				"type":  "plugin_inventory",
				"alias": "inventory",
			}},
			"prompt": map[string]any{"text": "{{ .DataSources.inventory.value }}"},
		},
	}
	if err := d.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	r, err := d.NewRunner(spec, task.RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	prompt, _, err := r.(*runner).renderPrompt(context.Background(), task.TriggerManual)
	if err != nil {
		t.Fatalf("render prompt: %v", err)
	}
	if prompt != "from-plugin" {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
}

func TestDriverPluginDataSourceConfigRefsAndOverrides(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Config map[string]any `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode plugin envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"method": envelope.Config["method"],
			},
		})
	}))
	defer server.Close()

	repo := &aiPluginConfigRepo{
		instances: map[string]pluginmodel.ConfigInstanceRecord{
			"cfg-inventory": {
				ID:           "cfg-inventory",
				PluginID:     "@test/source",
				CapabilityID: "@test/source:data_source:plugin_inventory",
				Scope:        "capability",
				Status:       "active",
			},
		},
		versions: map[string]pluginmodel.ConfigVersionRecord{
			"cfg-inventory": {
				InstanceID: "cfg-inventory",
				Version:    2,
				Status:     "active",
				Values: map[string]any{
					"method": "GetInventory",
				},
			},
		},
	}
	d := NewDriver(nil, repo, slog.Default())
	d.SyncPluginDataSources([]pluginmodel.Capability{{
		ID:       "@test/source:data_source:plugin_inventory",
		Type:     pluginmodel.CapabilityDataSource,
		PluginID: "@test/source",
		Name:     "plugin_inventory",
		Runtime:  "http",
		Endpoint: server.URL,
		Config: &pluginmodel.ConfigSchema{Fields: map[string]pluginmodel.ConfigField{
			"method": {Type: "string", Overridable: true},
		}},
	}}, config.PluginsConfig{})

	spec := config.TaskSpec{
		ID:   "test",
		Kind: "ai_analyze",
		Params: map[string]any{
			"data_sources": []any{map[string]any{
				"type":                  "plugin_inventory",
				"alias":                 "inventory",
				"capability_config_ref": "cfg-inventory",
				"overrides": map[string]any{
					"method": "GetInventoryV2",
				},
			}},
			"prompt": map[string]any{"text": "{{ .DataSources.inventory.method }}"},
		},
	}
	if err := d.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	taskRunner, err := d.NewRunner(spec, task.RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	prompt, trace, err := taskRunner.(*runner).renderPrompt(context.Background(), task.TriggerManual)
	if err != nil {
		t.Fatalf("render prompt: %v", err)
	}
	if prompt != "GetInventoryV2" {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
	if trace.ConfigVersions["cfg-inventory"] != 2 {
		t.Fatalf("unexpected config version trace: %#v", trace.ConfigVersions)
	}
	if _, ok := trace.TaskOverrides["inventory"].(map[string]any); !ok {
		t.Fatalf("expected task override trace, got %#v", trace.TaskOverrides)
	}
}

func TestDriverIgnoresManifestCABIDataSource(t *testing.T) {
	t.Parallel()

	driver := NewDriver(nil, nil, nil)
	driver.SyncPluginCapabilities([]pluginmodel.Capability{{
		Name:        "native_source",
		Type:        pluginmodel.CapabilityAIDataSource,
		Runtime:     "c_abi",
		Entrypoint:  "native.so",
		ReleasePath: t.TempDir(),
	}}, config.PluginsConfig{})
	if _, ok := driver.sourceRegistry().Get("native_source"); ok {
		t.Fatal("c_abi data source should not be registered")
	}
}

func TestDataSourceOnError(t *testing.T) {
	t.Parallel()

	reg := NewDataSourceRegistry()
	reg.Register("test_failing", &testFailingSource{})
	reg.Register("test_aliased", &testStaticSource{data: "hello"})

	t.Run("on_error skip spec parses", func(t *testing.T) {
		d := NewDriver(nil, nil, slog.Default())
		d.sources = reg

		spec := config.TaskSpec{
			ID: "test", Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{
					"type":     "test_failing",
					"alias":    "my_fail",
					"on_error": "skip",
				}},
				"prompt": map[string]any{"text": "hello"},
			},
		}
		r, err := d.NewRunner(spec, task.RunnerDeps{})
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		if r == nil {
			t.Fatal("expected non-nil runner")
		}
	})

	t.Run("alias key used in result map", func(t *testing.T) {
		d := NewDriver(nil, nil, slog.Default())
		d.sources = reg

		spec := config.TaskSpec{
			ID: "test", Kind: "ai_analyze",
			Params: map[string]any{
				"data_sources": []any{map[string]any{
					"type":  "test_aliased",
					"alias": "greeting",
				}},
				"prompt": map[string]any{"text": "{{ .DataSources.greeting }}"},
			},
		}
		r, err := d.NewRunner(spec, task.RunnerDeps{})
		if err != nil {
			t.Fatalf("NewRunner: %v", err)
		}
		if r == nil {
			t.Fatal("expected non-nil runner")
		}
	})
}

type testFailingSource struct{}

func (s *testFailingSource) Name() string { return "test_failing" }

func (s *testFailingSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	return nil, fmt.Errorf("intentional failure")
}

type testStaticSource struct {
	data any
}

func (s *testStaticSource) Name() string { return "test_aliased" }

func (s *testStaticSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	return s.data, nil
}

type aiPluginConfigRepo struct {
	store.Repository
	instances map[string]pluginmodel.ConfigInstanceRecord
	versions  map[string]pluginmodel.ConfigVersionRecord
}

func (r *aiPluginConfigRepo) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	if record, ok := r.instances[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigInstanceRecord{}, fmt.Errorf("plugin config instance %q not found", instanceID)
}

func (r *aiPluginConfigRepo) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	if record, ok := r.versions[instanceID]; ok {
		return record, nil
	}
	return pluginmodel.ConfigVersionRecord{}, fmt.Errorf("active plugin config version %q not found", instanceID)
}

func TestRunnerRunID(t *testing.T) {
	t.Parallel()

	t.Run("uses context value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), CtxRunID, "my-run-id")
		id := runnerRunID(ctx, "task-x")
		if id != "my-run-id" {
			t.Fatalf("expected my-run-id, got %s", id)
		}
	})

	t.Run("fallback generates id", func(t *testing.T) {
		id := runnerRunID(context.Background(), "task-x")
		if !strings.HasPrefix(id, "task-x-") {
			t.Fatalf("expected task-x- prefix, got %s", id)
		}
	})
}

func TestUnused(t *testing.T) {
	t.Parallel()
	var _ = json.Marshal
}
