package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

type pluginOutputWriter struct {
	cap pluginmodel.Capability
	cfg config.PluginsConfig
}

type pluginOutputPayload struct {
	Summary  map[string]any  `json:"summary"`
	Findings []store.Finding `json:"findings"`
}

func (w *pluginOutputWriter) Name() string {
	return w.cap.Name
}

func (w *pluginOutputWriter) Validate(spec OutputSpec) error {
	configMap := mergeOutputConfig(w.cap.Defaults, spec.Config)
	for name, field := range w.cap.Schema {
		if field.Required && configMap[name] == nil {
			return fmt.Errorf("output writer %q requires config.%s", w.cap.Name, name)
		}
	}
	return pluginruntime.NewClient(w.cap, w.cfg).ValidateAvailable()
}

func (w *pluginOutputWriter) Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error) {
	resp, err := pluginruntime.NewClient(w.cap, w.cfg).Call(ctx, pluginruntime.Request{
		Action: "write",
		Config: mergeOutputConfig(w.cap.Defaults, spec.Config),
		Input: map[string]any{
			"raw_response": input.RawResponse,
			"parsed_json":  input.ParsedJSON,
			"run_id":       input.RunID,
			"task_id":      input.TaskID,
			"tokens_in":    input.TokensIn,
			"tokens_out":   input.TokensOut,
			"duration_ms":  input.DurationMS,
		},
	}, pluginruntime.Deps{
		HTTPClient:    deps.HTTPClient,
		CurrentRunID:  deps.CurrentRunID,
		CurrentTaskID: deps.CurrentTaskID,
	})
	if err != nil {
		return nil, err
	}
	return pluginResponseToOutputResult(resp, deps), nil
}

func pluginResponseToOutputResult(resp pluginruntime.Response, deps OutputDeps) *OutputResult {
	result := &OutputResult{Summary: cloneOutputMap(resp.Summary)}
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return result
	}
	var payload pluginOutputPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return result
	}
	for key, value := range payload.Summary {
		if result.Summary == nil {
			result.Summary = map[string]any{}
		}
		result.Summary[key] = value
	}
	for i := range payload.Findings {
		if payload.Findings[i].RunID == "" {
			payload.Findings[i].RunID = deps.CurrentRunID
		}
		if payload.Findings[i].TaskID == "" {
			payload.Findings[i].TaskID = deps.CurrentTaskID
		}
	}
	result.Findings = payload.Findings
	return result
}

func mergeOutputConfig(defaults, configMap map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(configMap))
	for key, value := range defaults {
		out[key] = value
	}
	for key, value := range configMap {
		out[key] = value
	}
	return out
}

func cloneOutputMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
