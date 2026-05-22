package ai

import (
	"context"
	"fmt"
	"time"

	"pulseops/internal/ctxkey"
)

var (
	CtxRunID      = ctxkey.CtxRunID
	CtxTriggerRun = ctxkey.CtxTriggerRun
)

func runnerRunID(ctx context.Context, taskID string) string {
	if id, ok := ctx.Value(CtxRunID).(string); ok && id != "" {
		return id
	}
	return fmt.Sprintf("%s-%d", taskID, time.Now().UnixNano())
}

type AnalysisType = string

const (
	AnalysisDiagnose AnalysisType = "diagnose"
	AnalysisTrend    AnalysisType = "trend"
	AnalysisEvaluate AnalysisType = "evaluate"
)

type DataSourceSpec struct {
	Type    string         `toml:"type" json:"type"`
	Config  map[string]any `toml:"config" json:"config"`
	Alias   string         `toml:"alias" json:"alias"`
	OnError string         `toml:"on_error" json:"on_error"`
}

type PromptSpec struct {
	Text string `toml:"text" json:"text"`
}

type OutputSpec struct {
	Type   string         `toml:"type" json:"type"`
	Config map[string]any `toml:"config" json:"config"`
}

type AIAnalyzeParams struct {
	DataSources  []DataSourceSpec `toml:"data_sources" json:"data_sources"`
	Prompt       PromptSpec       `toml:"prompt" json:"prompt"`
	Outputs      []OutputSpec     `toml:"outputs" json:"outputs"`
	AnalysisType string           `toml:"analysis_type" json:"analysis_type"`
}
