package evaluator

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

type PluginEvaluator struct {
	cap           pluginmodel.Capability
	cfg           config.PluginsConfig
	configReader  pluginconfig.ConfigReader
	runtimeStore  pluginconfig.RuntimeStore
	artifactStore store.ArtifactStore
}

type pluginEvaluatorPayload struct {
	CheckStatus string           `json:"check_status"`
	Summary     map[string]any   `json:"summary"`
	Findings    []map[string]any `json:"findings"`
}

func NewPluginEvaluator(cap pluginmodel.Capability, cfg config.PluginsConfig, configStore any, artifactStore store.ArtifactStore) *PluginEvaluator {
	reader, _ := configStore.(pluginconfig.ConfigReader)
	runtimeStore, _ := configStore.(pluginconfig.RuntimeStore)
	return &PluginEvaluator{cap: cap, cfg: cfg, configReader: reader, runtimeStore: runtimeStore, artifactStore: artifactStore}
}

func (e *PluginEvaluator) Name() string {
	return e.cap.Name
}

func (e *PluginEvaluator) Evaluate(ctx context.Context, input Input) (Result, error) {
	resolved, err := pluginconfig.ResolveCapabilityConfig(ctx, e.configReader, e.cap, pluginconfig.ResolveCapabilityOptions{
		PluginConfigRef:     input.PluginConfigRef,
		CapabilityConfigRef: input.CapabilityConfigRef,
		Config:              input.TaskParams,
		Overrides:           input.Overrides,
		RuntimeStore:        e.runtimeStore,
		ArtifactStore:       e.artifactStore,
	})
	if resolved.Cleanup != nil {
		defer resolved.Cleanup()
	}
	if err != nil {
		return Result{CheckStatus: "fail"}, err
	}
	resp, err := pluginruntime.NewClient(e.cap, e.cfg).Call(ctx, pluginruntime.Request{
		Action: "evaluate",
		Config: resolved.Config,
		Input:  evaluatorInputMap(input),
	}, pluginruntime.Deps{CurrentTaskID: input.TaskID})
	if err != nil {
		return Result{CheckStatus: "fail"}, err
	}
	result := pluginResponseToEvaluatorResult(resp)
	result.PluginConfigVersions = resolved.ConfigVersions
	result.PluginAssetVersions = resolved.AssetVersions
	result.PluginTaskOverrides = resolved.TaskOverrides
	return result, nil
}

func pluginResponseToEvaluatorResult(resp pluginruntime.Response) Result {
	result := Result{
		CheckStatus: "pass",
		Summary:     cloneEvaluatorMap(resp.Summary),
	}
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return result
	}
	var payload pluginEvaluatorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		if resp.Data != nil {
			result.Summary = map[string]any{"data": resp.Data}
		}
		return result
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
	result.Findings = payload.Findings
	return result
}

func evaluatorInputMap(input Input) map[string]any {
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{"task_id": input.TaskID}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"task_id": input.TaskID}
	}
	return out
}

func cloneEvaluatorMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func ValidatePluginEvaluator(cap pluginmodel.Capability, cfg config.PluginsConfig) error {
	if cap.Type != pluginmodel.CapabilityEvaluator {
		return fmt.Errorf("capability %q is not an evaluator", cap.ID)
	}
	return pluginruntime.NewClient(cap, cfg).ValidateAvailable()
}
