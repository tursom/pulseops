package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"pulseops/internal/config"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

type pluginOutputWriter struct {
	cap           pluginmodel.Capability
	cfg           config.PluginsConfig
	configReader  pluginconfig.ConfigReader
	runtimeStore  pluginconfig.RuntimeStore
	artifactStore store.ArtifactStore
}

type pluginOutputPayload struct {
	Summary  map[string]any  `json:"summary"`
	Findings []store.Finding `json:"findings"`
}

func (w *pluginOutputWriter) Name() string {
	return w.cap.Name
}

func (w *pluginOutputWriter) Validate(spec OutputSpec) error {
	resolved, err := w.resolveSpec(context.Background(), spec)
	if resolved.Cleanup != nil {
		defer resolved.Cleanup()
	}
	if err != nil {
		return err
	}
	return pluginruntime.NewClient(w.cap, w.cfg).ValidateAvailable()
}

func (w *pluginOutputWriter) Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error) {
	resolved, err := w.resolveSpec(ctx, spec)
	if resolved.Cleanup != nil {
		defer resolved.Cleanup()
	}
	if err != nil {
		return nil, err
	}
	resp, err := pluginruntime.NewClient(w.cap, w.cfg).Call(ctx, pluginruntime.Request{
		Action: "write",
		Config: resolved.Config,
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
	result := pluginResponseToOutputResult(resp, deps)
	result.PluginConfigVersions = resolved.ConfigVersions
	result.PluginAssetVersions = resolved.AssetVersions
	result.PluginTaskOverrides = resolved.TaskOverrides
	return result, nil
}

func (w *pluginOutputWriter) resolveSpec(ctx context.Context, spec OutputSpec) (pluginconfig.ResolveCapabilityResult, error) {
	result, err := pluginconfig.ResolveCapabilityConfig(ctx, w.configReader, w.cap, pluginconfig.ResolveCapabilityOptions{
		PluginConfigRef:     spec.PluginConfigRef,
		CapabilityConfigRef: spec.CapabilityConfigRef,
		Config:              spec.Config,
		Overrides:           spec.Overrides,
		RuntimeStore:        w.runtimeStore,
		ArtifactStore:       w.artifactStore,
	})
	if err != nil {
		return result, err
	}
	configMap := result.Config
	for name, field := range w.cap.Schema {
		if field.Required && configMap[name] == nil {
			return result, fmt.Errorf("output writer %q requires config.%s", w.cap.Name, name)
		}
	}
	return result, nil
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
