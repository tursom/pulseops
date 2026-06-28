package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"pulseops/internal/config"
	"pulseops/internal/ctxkey"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

type PluginDriver struct {
	cap    pluginmodel.Capability
	cfg    config.PluginsConfig
	logger *slog.Logger
}

type pluginTaskPayload struct {
	CheckStatus string          `json:"check_status"`
	Summary     map[string]any  `json:"summary"`
	Payload     any             `json:"payload"`
	Findings    []store.Finding `json:"findings"`
	Stdout      string          `json:"stdout"`
	Stderr      string          `json:"stderr"`
}

func NewPluginDriver(cap pluginmodel.Capability, cfg config.PluginsConfig, logger *slog.Logger) *PluginDriver {
	return &PluginDriver{cap: cap, cfg: cfg, logger: logger}
}

func (d *PluginDriver) Kind() string {
	return d.cap.Name
}

func (d *PluginDriver) Validate(spec config.TaskSpec) error {
	params := mergePluginParams(d.cap.Defaults, spec.Params)
	if err := validatePluginSchema(d.cap, params); err != nil {
		return err
	}
	client := pluginruntime.NewClient(d.cap, d.cfg)
	_, err := client.Call(context.Background(), pluginruntime.Request{
		Action: "validate",
		Config: params,
		Input: map[string]any{
			"task": taskSpecInput(spec),
		},
	}, pluginruntime.Deps{CurrentTaskID: spec.ID})
	if err != nil {
		return fmt.Errorf("validate plugin task driver %q: %w", d.cap.Name, err)
	}
	return nil
}

func (d *PluginDriver) NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error) {
	params := mergePluginParams(d.cap.Defaults, spec.Params)
	return &pluginRunner{
		spec:   spec,
		params: params,
		deps:   deps,
		client: pluginruntime.NewClient(d.cap, d.cfg),
		cfg:    d.cfg,
		logger: d.logger,
	}, nil
}

type pluginRunner struct {
	spec   config.TaskSpec
	params map[string]any
	deps   RunnerDeps
	client *pluginruntime.Client
	cfg    config.PluginsConfig
	logger *slog.Logger
}

func (r *pluginRunner) Run(ctx context.Context, trigger TriggerType) (Result, error) {
	runID, _ := ctx.Value(ctxkey.CtxRunID).(string)
	input := map[string]any{
		"task": taskSpecInput(r.spec),
	}
	if triggerRun, ok := ctx.Value(ctxkey.CtxTriggerRun).(*store.RunRecord); ok && triggerRun != nil {
		input["trigger_run"] = triggerRun
	}
	resp, err := r.client.Call(ctx, pluginruntime.Request{
		Action: "run",
		Config: r.params,
		Input:  input,
	}, pluginruntime.Deps{
		HTTPClient:    r.deps.HTTPClient,
		CurrentRunID:  runID,
		CurrentTaskID: r.spec.ID,
		TriggerType:   string(trigger),
	})
	if err != nil {
		return Result{CheckStatus: "fail"}, err
	}
	return pluginResponseToTaskResult(resp, r.cfg), nil
}

func pluginResponseToTaskResult(resp pluginruntime.Response, cfg config.PluginsConfig) Result {
	limit := pluginruntime.MaxOutputBytes(cfg)
	result := Result{
		CheckStatus: "pass",
		Summary:     clonePluginMap(resp.Summary),
		Payload:     resp.Data,
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return result
	}
	known := false
	for _, key := range []string{"check_status", "summary", "payload", "findings", "stdout", "stderr"} {
		if _, ok := dataMap[key]; ok {
			known = true
			break
		}
	}
	if !known {
		return result
	}
	var payload pluginTaskPayload
	raw, err := json.Marshal(resp.Data)
	if err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	if payload.CheckStatus != "" {
		result.CheckStatus = payload.CheckStatus
	}
	for key, value := range payload.Summary {
		if result.Summary == nil {
			result.Summary = map[string]any{}
		}
		result.Summary[key] = value
	}
	result.Payload = payload.Payload
	result.Findings = payload.Findings
	result.Stdout = pluginruntime.TruncateString(payload.Stdout, limit)
	result.Stderr = pluginruntime.TruncateString(payload.Stderr, limit)
	return result
}

func validatePluginSchema(cap pluginmodel.Capability, params map[string]any) error {
	for name, field := range cap.Schema {
		if field.Required && params[name] == nil {
			return fmt.Errorf("%s requires params.%s", cap.Name, name)
		}
	}
	return nil
}

func mergePluginParams(defaults, params map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(params))
	for key, value := range defaults {
		out[key] = value
	}
	for key, value := range params {
		out[key] = value
	}
	return out
}

func taskSpecInput(spec config.TaskSpec) map[string]any {
	raw, err := json.Marshal(spec)
	if err != nil {
		return map[string]any{"id": spec.ID, "kind": spec.Kind}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{"id": spec.ID, "kind": spec.Kind}
	}
	return result
}

func clonePluginMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
