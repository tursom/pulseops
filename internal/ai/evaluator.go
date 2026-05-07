package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"pulseops/internal/evaluator"
)

type AIEvaluator struct {
	Client *Client
}

func (e *AIEvaluator) Name() string { return "ai" }

func (e *AIEvaluator) Evaluate(ctx context.Context, input evaluator.Input) (evaluator.Result, error) {
	if e.Client == nil {
		return evaluator.Result{CheckStatus: "fail"}, fmt.Errorf("ai evaluator has no client configured")
	}
	promptText, _ := input.TaskParams["prompt"].(string)
	if promptText == "" {
		return evaluator.Result{CheckStatus: "fail"}, fmt.Errorf("ai evaluator requires prompt in evaluator params")
	}
	maxSamples := 5
	if ms, ok := input.TaskParams["max_samples"].(float64); ok {
		maxSamples = int(ms)
	}
	onlyOnMismatch := false
	if oom, ok := input.TaskParams["only_on_mismatch"].(bool); ok {
		onlyOnMismatch = oom
	}

	checked := 0
	mismatchCount := 0
	errorCount := 0
	var findings []map[string]any

	for i, fi := range input.FanoutItems {
		if i >= maxSamples {
			break
		}
		if fi.Error != "" {
			if !onlyOnMismatch {
				errorCount++
				continue
			}
		} else if onlyOnMismatch {
			continue
		}
		finding, err := e.evaluateItem(ctx, promptText, fi)
		if err != nil {
			errorCount++
			continue
		}
		if finding != nil {
			findings = append(findings, finding)
			status, _ := finding["status"].(string)
			if status == "abnormal" || status == "warning" {
				mismatchCount++
			}
		}
		checked++
	}

	checkStatus := "pass"
	if checked == 0 || mismatchCount > 0 || (onlyOnMismatch && errorCount > 0) {
		checkStatus = "fail"
	}

	return evaluator.Result{
		CheckStatus: checkStatus,
		Summary: map[string]any{
			"ai_checked_count":  checked,
			"ai_mismatch_count": mismatchCount,
			"ai_error_count":    errorCount,
		},
		Findings: findings,
	}, nil
}

func (e *AIEvaluator) evaluateItem(ctx context.Context, template string, item evaluator.FanoutItem) (map[string]any, error) {
	prompt := buildItemPrompt(template, item)
	resp, err := e.Client.Chat(ctx, prompt)
	if err != nil {
		return nil, err
	}
	parsed := tryParseFindingJSON(resp.Content)
	return parsed, nil
}

func buildItemPrompt(template string, item evaluator.FanoutItem) string {
	result := template
	for k, v := range item.Item {
		placeholder := fmt.Sprintf("{{ .%s }}", k)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", v))
	}
	if item.Detail != nil {
		detailJSON, _ := json.Marshal(item.Detail)
		result = strings.ReplaceAll(result, "{{ .Detail }}", string(detailJSON))
		if detailMap, ok := item.Detail.(map[string]any); ok {
			for k, v := range detailMap {
				placeholder := fmt.Sprintf("{{ .Detail.%s }}", k)
				result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", v))
			}
		}
	}
	return result
}

func tryParseFindingJSON(content string) map[string]any {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "{") {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err == nil {
			return parsed
		}
	}
	return map[string]any{
		"raw_response": content,
	}
}
