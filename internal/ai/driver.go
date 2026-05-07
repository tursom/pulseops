package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

type Driver struct {
	client    *Client
	store     store.Repository
	sources *DataSourceRegistry
	writers *OutputWriterRegistry
}

func NewDriver(client *Client, store store.Repository) *Driver {
	return &Driver{
		client:    client,
		store:     store,
		sources: NewDataSourceRegistry(),
		writers: NewOutputWriterRegistry(),
	}
}

func (d *Driver) Kind() string { return "ai_analyze" }

func (d *Driver) Validate(spec config.TaskSpec) error {
	params, err := d.decodeParams(spec.Params)
	if err != nil {
		return fmt.Errorf("decode ai_analyze params: %w", err)
	}
	if len(params.DataSources) == 0 {
		return fmt.Errorf("ai_analyze requires at least one data source")
	}
	if params.Prompt.Text == "" {
		return fmt.Errorf("ai_analyze requires prompt.text")
	}
	return nil
}

func (d *Driver) NewRunner(spec config.TaskSpec, deps task.RunnerDeps) (task.Runner, error) {
	params, err := d.decodeParams(spec.Params)
	if err != nil {
		return nil, fmt.Errorf("decode ai_analyze params: %w", err)
	}
	return &runner{
		spec:      spec,
		params:    params,
		client:    d.client,
		store:     d.store,
		deps:      deps,
		sources: d.sources,
		writers: d.writers,
	}, nil
}

func (d *Driver) decodeParams(raw map[string]any) (AIAnalyzeParams, error) {
	params, err := task.DecodeParams[AIAnalyzeParams](raw)
	if err != nil {
		return AIAnalyzeParams{}, err
	}
	if params.AnalysisType == "" {
		params.AnalysisType = AnalysisDiagnose
	}
	return params, nil
}

type runner struct {
	spec      config.TaskSpec
	params    AIAnalyzeParams
	client    *Client
	store     store.Repository
	deps      task.RunnerDeps
	sources *DataSourceRegistry
	writers *OutputWriterRegistry
}

func (r *runner) Run(ctx context.Context, trigger task.TriggerType) (task.Result, error) {
	startedAt := time.Now()

	promptText, err := r.renderPrompt(ctx)
	if err != nil {
		return task.Result{CheckStatus: "fail"}, fmt.Errorf("render prompt: %w", err)
	}

	resp, err := r.client.Chat(ctx, promptText)
	durationMS := time.Since(startedAt).Milliseconds()

	runID := runnerRunID(ctx, r.spec.ID)
	record := store.AIAnalysisRecord{
		RunID:        runID,
		TaskID:       r.spec.ID,
		AnalysisType: r.params.AnalysisType,
		Model:        r.client.model,
		Prompt:       promptText,
		DurationMS:   durationMS,
	}

	if err != nil {
		record.Status = "error"
		record.ErrorMessage = err.Error()
		if insErr := r.store.InsertAIAnalysis(ctx, record); insErr != nil {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("ai analysis error: %s, insert: %w", err, insErr)
		}
		return task.Result{
			CheckStatus: "fail",
			Summary:     map[string]any{"ai_error": err.Error()},
		}, nil
	}

	record.Status = "success"
	record.Response = resp.Content
	record.TokensIn = resp.TokensIn
	record.TokensOut = resp.TokensOut

	parsedJSON := tryParseJSON(resp.Content)

	outputDeps := OutputDeps{
		DBRepository:  r.store,
		HTTPClient:    r.deps.HTTPClient,
		CurrentRunID:  runID,
		CurrentTaskID: r.spec.ID,
	}
	outputInput := OutputInput{
		RawResponse: resp.Content,
		ParsedJSON:  parsedJSON,
		RunID:       runID,
		TaskID:      r.spec.ID,
		TokensIn:    resp.TokensIn,
		TokensOut:   resp.TokensOut,
		DurationMS:  durationMS,
	}

	if err := r.store.InsertAIAnalysis(ctx, record); err != nil {
		return task.Result{CheckStatus: "fail"}, fmt.Errorf("insert ai analysis: %w", err)
	}

	var outputFindings []store.Finding
	outputSummary := map[string]any{}
	for _, outSpec := range r.params.Outputs {
		writer, ok := r.writers.Get(outSpec.Type)
		if !ok {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("unknown output type %q", outSpec.Type)
		}
		result, err := writer.Write(ctx, outSpec, outputDeps, outputInput)
		if err != nil {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("write output %q: %w", outSpec.Type, err)
		}
		if result != nil {
			outputFindings = append(outputFindings, result.Findings...)
			for k, v := range result.Summary {
				outputSummary[k] = v
			}
		}
	}

	summary := map[string]any{
		"ai_analysis_id": runID,
		"ai_tokens_in":   resp.TokensIn,
		"ai_tokens_out":  resp.TokensOut,
		"ai_duration_ms": durationMS,
	}
	for k, v := range outputSummary {
		summary[k] = v
	}

	return task.Result{
		CheckStatus: "pass",
		Summary:     summary,
		Findings:    outputFindings,
		Payload:     resp.Content,
	}, nil
}

func (r *runner) renderPrompt(ctx context.Context) (string, error) {
	dataSources, err := r.fetchDataSources(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch data sources: %w", err)
	}
	return renderTemplate(r.params.Prompt, dataSources)
}

func (r *runner) fetchDataSources(ctx context.Context) (map[string]any, error) {
	deps := FetchDeps{
		DBRepository: r.store,
		HTTPClient:   r.deps.HTTPClient,
	}
	if tr, ok := ctx.Value(CtxTriggerRun).(*store.RunRecord); ok {
		deps.TriggerRun = tr
	}
	result := make(map[string]any, len(r.params.DataSources))
	for _, dsSpec := range r.params.DataSources {
		source, ok := r.sources.Get(dsSpec.Type)
		if !ok {
			return nil, fmt.Errorf("unknown data source type %q", dsSpec.Type)
		}
		data, err := source.Fetch(ctx, dsSpec, deps)
		if err != nil {
			return nil, fmt.Errorf("fetch data source %q: %w", dsSpec.Type, err)
		}
		result[dsSpec.Type] = data
	}
	return result, nil
}

func tryParseJSON(content string) map[string]any {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "{") {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err == nil {
			return parsed
		}
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(line), &parsed); err == nil {
				return parsed
			}
		}
	}
	return nil
}
