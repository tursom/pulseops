package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"pulseops/internal/config"
	"pulseops/internal/ctxkey"
	"pulseops/internal/pluginmodel"
)

func TestPluginDriverHTTPValidateAndRun(t *testing.T) {
	t.Parallel()

	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		action, _ := req["action"].(string)
		actions = append(actions, action)
		switch action {
		case "validate":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"valid": true}})
		case "run":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"data": map[string]any{
					"check_status": "fail",
					"summary":      map[string]any{"status": "external"},
					"payload":      map[string]any{"value": 7},
					"stdout":       "external stdout",
				},
			})
		default:
			t.Fatalf("unexpected action %q", action)
		}
	}))
	defer server.Close()

	driver := NewPluginDriver(pluginmodel.Capability{
		PluginID: "@test/external",
		Name:     "external_check",
		Runtime:  "http",
		Endpoint: server.URL,
		Schema: pluginmodel.Schema{
			"target": {Type: "string", Required: true},
		},
	}, config.PluginsConfig{}, nil)
	spec := config.TaskSpec{
		ID:   "external-task",
		Kind: "external_check",
		Params: map[string]any{
			"target": "steam",
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, RunnerDeps{HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	ctx := context.WithValue(context.Background(), ctxkey.CtxRunID, "run-1")
	result, err := runner.Run(ctx, TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.CheckStatus != "fail" || result.Summary["status"] != "external" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Payload.(map[string]any)["value"] != float64(7) || result.Stdout != "external stdout" {
		t.Fatalf("unexpected payload/stdout: %#v", result)
	}
	if len(actions) != 2 || actions[0] != "validate" || actions[1] != "run" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}

func TestPluginDriverProcessRun(t *testing.T) {
	t.Parallel()

	releaseDir := t.TempDir()
	entrypoint := filepath.Join(releaseDir, "driver.sh")
	script := `#!/bin/sh
input=$(cat)
case "$input" in
  *'"action":"validate"'*) echo '{"ok":true,"data":{"valid":true}}' ;;
  *'"action":"run"'*) echo '{"ok":true,"data":{"check_status":"pass","summary":{"source":"process"},"payload":{"value":"ok"}}}' ;;
  *) echo '{"ok":false,"error":{"message":"bad action"}}' ;;
esac
`
	if err := os.WriteFile(entrypoint, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	driver := NewPluginDriver(pluginmodel.Capability{
		PluginID:    "@test/process",
		Name:        "process_check_external",
		Runtime:     "process",
		Entrypoint:  "driver.sh",
		ReleasePath: releaseDir,
	}, config.PluginsConfig{}, nil)
	spec := config.TaskSpec{ID: "external-task", Kind: "process_check_external"}
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
	if result.CheckStatus != "pass" || result.Summary["source"] != "process" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPluginDriverProcessCrashDoesNotPanic(t *testing.T) {
	t.Parallel()

	releaseDir := t.TempDir()
	entrypoint := filepath.Join(releaseDir, "driver.sh")
	if err := os.WriteFile(entrypoint, []byte("#!/bin/sh\necho crashed >&2\nexit 2\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	driver := NewPluginDriver(pluginmodel.Capability{
		PluginID:    "@test/process",
		Name:        "crashing_external",
		Runtime:     "process",
		Entrypoint:  "driver.sh",
		ReleasePath: releaseDir,
	}, config.PluginsConfig{}, nil)
	runner, err := driver.NewRunner(config.TaskSpec{ID: "external-task", Kind: "crashing_external"}, RunnerDeps{})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), TriggerManual)
	if err == nil {
		t.Fatal("expected plugin process error")
	}
	if result.CheckStatus != "fail" {
		t.Fatalf("expected fail result on crash, got %#v", result)
	}
}

func TestPluginDriverValidatesSchemaTypes(t *testing.T) {
	t.Parallel()

	capability := pluginmodel.Capability{
		PluginID: "@test/external",
		Name:     "external_check",
		Runtime:  "http",
		Schema: pluginmodel.Schema{
			"target":    {Type: "string", Required: true},
			"threshold": {Type: "number"},
			"enabled":   {Type: "bool"},
			"payload":   {Type: "object"},
			"tags":      {Type: "array"},
		},
	}
	err := validatePluginSchema(capability, map[string]any{
		"target":    "steam",
		"threshold": 3,
		"enabled":   true,
		"payload":   map[string]any{"region": "cn"},
		"tags":      []any{"prod"},
	})
	if err != nil {
		t.Fatalf("schema validation should pass: %v", err)
	}

	err = validatePluginSchema(capability, map[string]any{"target": 42})
	if err == nil || err.Error() != "external_check params.target must be a string" {
		t.Fatalf("expected schema type error, got %v", err)
	}
}
