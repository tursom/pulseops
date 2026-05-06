package task

import (
	"context"
	"fmt"

	"pulseops/internal/config"
	"pulseops/internal/scenario"
	"pulseops/internal/store"
)

type ScenarioCheckDriver struct{}

func (ScenarioCheckDriver) Kind() string {
	return "scenario_check"
}

func (ScenarioCheckDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[scenario.Params](spec.Params)
	if err != nil {
		return err
	}
	if params.Source.URL == "" {
		return fmt.Errorf("scenario_check requires params.source.url")
	}
	if params.Fanout.URL == "" {
		return fmt.Errorf("scenario_check requires params.fanout.url")
	}
	if params.Evaluator.Name == "" {
		return fmt.Errorf("scenario_check requires params.evaluator.name")
	}
	return nil
}

func (ScenarioCheckDriver) NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error) {
	params, err := DecodeParams[scenario.Params](spec.Params)
	if err != nil {
		return nil, err
	}
	return &scenarioCheckRunner{
		taskID: spec.ID,
		params: params,
		executor: scenario.Executor{
			HTTPClient: deps.HTTPClient,
			Evaluators: deps.Evaluators,
		},
	}, nil
}

type scenarioCheckRunner struct {
	taskID   string
	params   scenario.Params
	executor scenario.Executor
}

func (r *scenarioCheckRunner) Run(ctx context.Context, _ TriggerType) (Result, error) {
	output, err := r.executor.Run(ctx, r.taskID, r.params)
	if err != nil {
		return Result{}, err
	}
	return Result{
		CheckStatus: output.CheckStatus,
		Summary:     output.Summary,
		Payload:     output.Payload,
		Findings:    mapScenarioFindings(r.taskID, output.Findings),
	}, nil
}

func mapScenarioFindings(taskID string, findings []map[string]any) []store.Finding {
	if len(findings) == 0 {
		return nil
	}
	result := make([]store.Finding, 0, len(findings))
	for _, finding := range findings {
		result = append(result, store.Finding{
			TaskID:   taskID,
			SampleID: stringifyFindingField(finding["sample_id"]),
			Reason:   stringifyFindingField(finding["reason"]),
			Data:     cloneFindingMap(finding),
		})
	}
	return result
}

func cloneFindingMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func stringifyFindingField(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}
