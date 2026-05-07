package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
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

	d := NewDriver(nil, nil)

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
