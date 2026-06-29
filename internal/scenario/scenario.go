package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"pulseops/internal/evaluator"
)

type HTTPJSONRequest struct {
	Kind      string            `json:"kind"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      map[string]any    `json:"body"`
	ItemsPath string            `json:"items_path"`
}

type SampleConfig struct {
	Strategy string   `json:"strategy"`
	Count    int      `json:"count"`
	SeedMode string   `json:"seed_mode"`
	Seed     int64    `json:"seed"`
	FixedIDs []string `json:"fixed_ids"`
}

type HTTPJSONFanout struct {
	Kind          string            `json:"kind"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	BodyTemplate  map[string]string `json:"body_template"`
	QueryTemplate map[string]string `json:"query_template"`
	Concurrency   int               `json:"concurrency"`
}

type EvaluatorConfig struct {
	Name                string         `json:"name"`
	Params              map[string]any `json:"params"`
	PluginConfigRef     string         `json:"plugin_config_ref"`
	CapabilityConfigRef string         `json:"capability_config_ref"`
	Overrides           map[string]any `json:"overrides"`
}

type Thresholds struct {
	MaxMismatchCount int `json:"max_mismatch_count"`
	MaxErrorCount    int `json:"max_error_count"`
}

type Params struct {
	Source     HTTPJSONRequest `json:"source"`
	Sample     SampleConfig    `json:"sample"`
	Fanout     HTTPJSONFanout  `json:"fanout"`
	Evaluator  EvaluatorConfig `json:"evaluator"`
	Thresholds Thresholds      `json:"thresholds"`
}

type Output struct {
	CheckStatus          string           `json:"check_status"`
	Summary              map[string]any   `json:"summary"`
	Payload              map[string]any   `json:"payload"`
	Findings             []map[string]any `json:"findings"`
	PluginConfigVersions map[string]any   `json:"plugin_config_versions,omitempty"`
	PluginAssetVersions  map[string]any   `json:"plugin_asset_versions,omitempty"`
	PluginTaskOverrides  map[string]any   `json:"plugin_task_overrides,omitempty"`
}

type Executor struct {
	HTTPClient *http.Client
	Evaluators *evaluator.Registry
}

func (e *Executor) Run(ctx context.Context, taskID string, params Params) (Output, error) {
	if e.HTTPClient == nil {
		e.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	ev, ok := e.Evaluators.Get(params.Evaluator.Name)
	if !ok {
		return Output{}, fmt.Errorf("scenario evaluator %q not found", params.Evaluator.Name)
	}
	sourcePayload, err := e.fetchJSON(ctx, params.Source, nil, nil)
	if err != nil {
		return Output{}, fmt.Errorf("load scenario source: %w", err)
	}
	items, err := extractItems(sourcePayload, params.Source.ItemsPath)
	if err != nil {
		return Output{}, err
	}
	sampled, seed := sampleItems(items, params.Sample)
	fanoutItems := e.fanout(ctx, sampled, params.Fanout)
	evalResult, err := ev.Evaluate(ctx, evaluator.Input{
		TaskID:              taskID,
		TaskParams:          params.Evaluator.Params,
		PluginConfigRef:     params.Evaluator.PluginConfigRef,
		CapabilityConfigRef: params.Evaluator.CapabilityConfigRef,
		Overrides:           params.Evaluator.Overrides,
		SourceItems:         items,
		SampledItems:        sampled,
		FanoutItems:         fanoutItems,
	})
	if err != nil {
		return Output{}, fmt.Errorf("evaluate scenario: %w", err)
	}
	summary := cloneMap(evalResult.Summary)
	summary["sample_seed"] = seed
	checkStatus := applyThresholds(evalResult.CheckStatus, summary, params.Thresholds)
	return Output{
		CheckStatus: checkStatus,
		Summary:     summary,
		Payload: map[string]any{
			"sample_seed":   seed,
			"sampled_items": sampled,
			"findings":      evalResult.Findings,
		},
		Findings:             evalResult.Findings,
		PluginConfigVersions: evalResult.PluginConfigVersions,
		PluginAssetVersions:  evalResult.PluginAssetVersions,
		PluginTaskOverrides:  evalResult.PluginTaskOverrides,
	}, nil
}

func (e *Executor) fanout(ctx context.Context, sampled []map[string]any, cfg HTTPJSONFanout) []evaluator.FanoutItem {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}
	results := make([]evaluator.FanoutItem, len(sampled))
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	for idx, item := range sampled {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, item map[string]any) {
			defer wg.Done()
			defer func() { <-sem }()
			query := renderTemplateMap(cfg.QueryTemplate, item)
			body := renderTemplateMap(cfg.BodyTemplate, item)
			payload, err := e.fetchJSON(ctx, HTTPJSONRequest{
				Method:  cfg.Method,
				URL:     cfg.URL,
				Headers: cfg.Headers,
				Body:    mapStringAny(body),
			}, query, body)
			results[idx] = evaluator.FanoutItem{Item: item}
			if err != nil {
				results[idx].Error = err.Error()
				return
			}
			results[idx].Detail = payload
		}(idx, item)
	}
	wg.Wait()
	return results
}

func (e *Executor) fetchJSON(ctx context.Context, cfg HTTPJSONRequest, query map[string]string, body map[string]string) (any, error) {
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodGet
	}
	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", cfg.URL, err)
	}
	values := parsedURL.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	parsedURL.RawQuery = values.Encode()

	var requestBody io.Reader
	if method != http.MethodGet {
		if len(body) > 0 {
			raw, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("marshal request body: %w", err)
			}
			requestBody = bytes.NewReader(raw)
		} else if len(cfg.Body) > 0 {
			raw, err := json.Marshal(cfg.Body)
			if err != nil {
				return nil, fmt.Errorf("marshal request body: %w", err)
			}
			requestBody = bytes.NewReader(raw)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("response status %d: %s", resp.StatusCode, string(raw))
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode json response: %w", err)
	}
	return decoded, nil
}

func extractItems(payload any, path string) ([]map[string]any, error) {
	current := payload
	path = strings.TrimSpace(path)
	if path == "" {
		path = "$"
	}
	if path != "$" {
		path = strings.TrimPrefix(path, "$.")
		for _, part := range strings.Split(path, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("items_path %q does not point to object", path)
			}
			next, ok := object[part]
			if !ok {
				return nil, fmt.Errorf("items_path %q missing field %q", path, part)
			}
			current = next
		}
	}
	list, ok := current.([]any)
	if !ok {
		return nil, fmt.Errorf("items_path does not point to array")
	}
	items := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("scenario source item is not object")
		}
		items = append(items, object)
	}
	return items, nil
}

func sampleItems(items []map[string]any, cfg SampleConfig) ([]map[string]any, int64) {
	if cfg.Count <= 0 || cfg.Count > len(items) {
		cfg.Count = len(items)
	}
	if len(items) == 0 || cfg.Count == 0 {
		return nil, cfg.Seed
	}
	switch strings.ToLower(cfg.Strategy) {
	case "", "random":
		seed := cfg.Seed
		if seed == 0 || !strings.EqualFold(cfg.SeedMode, "fixed") {
			seed = time.Now().UnixNano()
		}
		random := rand.New(rand.NewSource(seed))
		indexes := random.Perm(len(items))[:cfg.Count]
		sampled := make([]map[string]any, 0, len(indexes))
		for _, idx := range indexes {
			sampled = append(sampled, items[idx])
		}
		return sampled, seed
	case "first_n":
		return append([]map[string]any(nil), items[:cfg.Count]...), cfg.Seed
	case "fixed_ids":
		set := make(map[string]struct{}, len(cfg.FixedIDs))
		for _, id := range cfg.FixedIDs {
			set[id] = struct{}{}
		}
		sampled := make([]map[string]any, 0, cfg.Count)
		for _, item := range items {
			id := candidateID(item)
			if _, ok := set[id]; ok {
				sampled = append(sampled, item)
				if len(sampled) >= cfg.Count {
					break
				}
			}
		}
		return sampled, cfg.Seed
	default:
		return append([]map[string]any(nil), items[:cfg.Count]...), cfg.Seed
	}
}

func applyThresholds(defaultStatus string, summary map[string]any, thresholds Thresholds) string {
	status := defaultStatus
	mismatch := intValue(summary["mismatch_count"])
	errors := intValue(summary["error_count"])
	if mismatch > thresholds.MaxMismatchCount || errors > thresholds.MaxErrorCount {
		return "fail"
	}
	if status == "" {
		return "pass"
	}
	return status
}

func renderTemplateMap(template map[string]string, item map[string]any) map[string]string {
	if len(template) == 0 {
		return nil
	}
	result := make(map[string]string, len(template))
	for key, value := range template {
		result[key] = renderTemplate(value, item)
	}
	return result
}

func renderTemplate(template string, item map[string]any) string {
	if !strings.Contains(template, "{{") {
		return template
	}
	trimmed := strings.TrimSpace(template)
	trimmed = strings.TrimPrefix(trimmed, "{{")
	trimmed = strings.TrimSuffix(trimmed, "}}")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "item.")
	if value, ok := item[trimmed]; ok {
		return fmt.Sprint(value)
	}
	return ""
}

func mapStringAny(input map[string]string) map[string]any {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func candidateID(item map[string]any) string {
	for _, key := range []string{"goods_id", "id", "sku_id", "item_id"} {
		if value, ok := item[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
