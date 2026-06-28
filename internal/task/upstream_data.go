package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/itchyny/gojq"

	"pulseops/internal/config"
	"pulseops/internal/ctxkey"
	"pulseops/internal/datasource"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

type UpstreamDataParams struct {
	SourceTaskID string               `json:"source_task_id"`
	DataSources  []UpstreamDataSource `json:"data_sources"`
	CommonParams map[string]any       `json:"common_params"`
	ExtractExprs []ExtractExpr        `json:"extract_exprs"`
}

type UpstreamDataSource struct {
	Key     string         `json:"key"`
	TaskID  string         `json:"task_id"`
	Params  map[string]any `json:"params"`
	Type    string         `json:"type"`
	Alias   string         `json:"alias"`
	Config  map[string]any `json:"config"`
	OnError string         `json:"on_error"`
}

func (s UpstreamDataSource) referenceKey() string {
	if s.Alias != "" {
		return s.Alias
	}
	if s.Key != "" {
		return s.Key
	}
	return s.Type
}

func mergedDataSourceConfig(common map[string]any, source UpstreamDataSource) map[string]any {
	out := make(map[string]any, len(common)+len(source.Params)+len(source.Config))
	for key, value := range common {
		out[key] = value
	}
	for key, value := range source.Params {
		out[key] = value
	}
	for key, value := range source.Config {
		out[key] = value
	}
	return out
}

type ExtractExpr struct {
	Field     string `json:"field"`
	SourceKey string `json:"source_key"`
	Source    string `json:"source"`
	JQExpr    string `json:"jq_expr"`
	AggMode   string `json:"agg_mode"`
}

type UpstreamDataDriver struct {
	repo          store.Repository
	artStore      store.ArtifactStore
	logger        *slog.Logger
	dataSourcesMu sync.RWMutex
	dataSources   *datasource.Registry
}

func NewUpstreamDataDriver(repo store.Repository, artStore store.ArtifactStore, logger *slog.Logger) *UpstreamDataDriver {
	return &UpstreamDataDriver{repo: repo, artStore: artStore, logger: logger, dataSources: datasource.NewRegistry()}
}

func (d *UpstreamDataDriver) SyncPluginDataSources(caps []pluginmodel.Capability, cfg config.PluginsConfig) {
	registry := datasource.NewRegistry()
	for _, cap := range caps {
		if cap.Type != pluginmodel.CapabilityDataSource {
			continue
		}
		registry.Register(cap.Name, datasource.NewPluginSource(cap, cfg))
	}
	d.dataSourcesMu.Lock()
	d.dataSources = registry
	d.dataSourcesMu.Unlock()
}

func (d *UpstreamDataDriver) Kind() string { return "data_process" }

func (d *UpstreamDataDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[UpstreamDataParams](spec.Params)
	if err != nil {
		return fmt.Errorf("decode data_process params: %w", err)
	}
	if len(params.ExtractExprs) == 0 {
		return fmt.Errorf("data_process requires at least one extract_expr")
	}
	sourceKeys := map[string]string{}
	pluginSourceKeys := map[string]struct{}{}
	for _, dep := range spec.DependencyRules() {
		if dep.SourceKey != "" {
			sourceKeys[dep.SourceKey] = dep.UpstreamTaskID
		}
	}
	registry := d.dataSourceRegistry()
	for i, source := range params.DataSources {
		if source.Type != "" {
			key := source.referenceKey()
			if key == "" {
				return fmt.Errorf("data_process data_sources[%d].alias must not be empty", i)
			}
			if _, exists := sourceKeys[key]; exists {
				return fmt.Errorf("data_process data_sources[%d].alias %q duplicates another source_key", i, key)
			}
			dataSource, ok := registry.Get(source.Type)
			if !ok {
				return fmt.Errorf("data_process data_sources[%d].type %q is not available", i, source.Type)
			}
			if validator, ok := dataSource.(datasource.Validator); ok {
				if err := validator.ValidateSpec(datasource.Spec{
					Type:    source.Type,
					Config:  mergedDataSourceConfig(params.CommonParams, source),
					Alias:   key,
					OnError: source.OnError,
				}); err != nil {
					return fmt.Errorf("validate data_process data_sources[%d] %q: %w", i, source.Type, err)
				}
			}
			sourceKeys[key] = ""
			pluginSourceKeys[key] = struct{}{}
			continue
		}
		if source.Key == "" {
			return fmt.Errorf("data_process data_sources[%d].key must not be empty", i)
		}
		if source.TaskID == "" {
			return fmt.Errorf("data_process data_sources[%d].task_id must not be empty", i)
		}
		if _, exists := sourceKeys[source.Key]; exists {
			return fmt.Errorf("data_process data_sources[%d].key %q duplicates dependency source_key", i, source.Key)
		}
		sourceKeys[source.Key] = source.TaskID
	}
	for i, expr := range params.ExtractExprs {
		if expr.Field == "" {
			return fmt.Errorf("data_process extract_expr[%d].field must not be empty", i)
		}
		if expr.Source == "" {
			return fmt.Errorf("data_process extract_expr[%d].source must not be empty", i)
		}
		if expr.JQExpr == "" {
			return fmt.Errorf("data_process extract_expr[%d].jq_expr must not be empty", i)
		}
		_, pluginSource := pluginSourceKeys[expr.SourceKey]
		switch {
		case pluginSource && (expr.Source == "data" || expr.Source == "payload"):
		case expr.Source == "payload", expr.Source == "summary", expr.Source == "record", strings.HasPrefix(expr.Source, "artifact:"):
		default:
			return fmt.Errorf("data_process extract_expr[%d].source %q must be 'payload', 'summary', 'record', 'data', or 'artifact:<kind>'", i, expr.Source)
		}
		if expr.SourceKey == "" && params.SourceTaskID == "" && spec.WatchTaskID == "" && len(spec.DependencyRules()) > 1 {
			return fmt.Errorf("data_process extract_expr[%d].source_key is required when multiple upstream dependencies are configured", i)
		}
		if expr.SourceKey != "" {
			if _, ok := sourceKeys[expr.SourceKey]; !ok {
				return fmt.Errorf("data_process extract_expr[%d].source_key %q is not configured", i, expr.SourceKey)
			}
		}
	}
	return nil
}

func (d *UpstreamDataDriver) NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error) {
	params, err := DecodeParams[UpstreamDataParams](spec.Params)
	if err != nil {
		return nil, fmt.Errorf("decode data_process params: %w", err)
	}
	return &upstreamDataRunner{
		spec:               spec,
		params:             params,
		repo:               d.repo,
		artStore:           d.artStore,
		logger:             d.logger,
		deps:               deps,
		dataSourceRegistry: d.dataSourceRegistry,
	}, nil
}

func (d *UpstreamDataDriver) dataSourceRegistry() *datasource.Registry {
	d.dataSourcesMu.RLock()
	defer d.dataSourcesMu.RUnlock()
	if d.dataSources == nil {
		return datasource.NewRegistry()
	}
	return d.dataSources
}

type upstreamDataRunner struct {
	spec               config.TaskSpec
	params             UpstreamDataParams
	repo               store.Repository
	artStore           store.ArtifactStore
	logger             *slog.Logger
	deps               RunnerDeps
	dataSourceRegistry func() *datasource.Registry
}

func (r *upstreamDataRunner) Run(ctx context.Context, trigger TriggerType) (Result, error) {
	sourceRecord, ok := ctx.Value(ctxkey.CtxTriggerRun).(*store.RunRecord)
	if !ok {
		sourceRecord = nil
	}

	summary := map[string]any{}
	var findings []store.Finding
	artifactCache := map[string][]store.ArtifactRef{}
	pluginSources, pluginFindings, err := r.fetchPluginDataSources(ctx, trigger)
	if err != nil {
		return Result{CheckStatus: "fail", Findings: pluginFindings}, err
	}
	findings = append(findings, pluginFindings...)

	for _, expr := range r.params.ExtractExprs {
		if data, ok := pluginSources[expr.SourceKey]; ok {
			result, jqErr := r.applyJQ(expr.JQExpr, data)
			if jqErr != nil {
				findings = append(findings, store.Finding{
					Reason: fmt.Sprintf("jq expr %q for field %q: %v", expr.JQExpr, expr.Field, jqErr),
				})
				continue
			}
			if expr.AggMode != "" {
				result = r.aggregate(expr.AggMode, result)
			}
			summary[expr.Field] = result
			continue
		}
		exprRecord, err := r.resolveSourceRecord(ctx, expr.SourceKey, sourceRecord)
		if err != nil {
			findings = append(findings, store.Finding{
				Reason: fmt.Sprintf("resolve upstream %q for field %q: %v", expr.SourceKey, expr.Field, err),
			})
			continue
		}
		srcData, err := r.resolveSource(ctx, expr.Source, exprRecord, artifactCache)
		if err != nil {
			findings = append(findings, store.Finding{
				Reason: fmt.Sprintf("resolve source %q for field %q: %v", expr.Source, expr.Field, err),
			})
			continue
		}

		result, jqErr := r.applyJQ(expr.JQExpr, srcData)
		if jqErr != nil {
			findings = append(findings, store.Finding{
				Reason: fmt.Sprintf("jq expr %q for field %q: %v", expr.JQExpr, expr.Field, jqErr),
			})
			continue
		}

		if expr.AggMode != "" {
			result = r.aggregate(expr.AggMode, result)
		}

		summary[expr.Field] = result
	}

	return Result{
		CheckStatus: "pass",
		Summary:     summary,
		Payload:     summary,
		Findings:    findings,
	}, nil
}

func (r *upstreamDataRunner) fetchPluginDataSources(ctx context.Context, trigger TriggerType) (map[string]any, []store.Finding, error) {
	registry := r.dataSourceRegistry()
	runID, _ := ctx.Value(ctxkey.CtxRunID).(string)
	deps := datasource.FetchDeps{
		HTTPClient:    r.deps.HTTPClient,
		CurrentRunID:  runID,
		CurrentTaskID: r.spec.ID,
		TriggerType:   string(trigger),
	}
	results := map[string]any{}
	var findings []store.Finding
	for _, spec := range r.params.DataSources {
		if spec.Type == "" {
			continue
		}
		key := spec.referenceKey()
		source, ok := registry.Get(spec.Type)
		if !ok {
			err := fmt.Errorf("data source type %q is not available", spec.Type)
			if spec.OnError == "skip" {
				findings = append(findings, store.Finding{Reason: err.Error()})
				continue
			}
			return results, findings, err
		}
		data, err := source.Fetch(ctx, datasource.Spec{
			Type:    spec.Type,
			Config:  mergedDataSourceConfig(r.params.CommonParams, spec),
			Alias:   key,
			OnError: spec.OnError,
		}, deps)
		if err != nil {
			wrapped := fmt.Errorf("fetch data source %q: %w", spec.Type, err)
			if spec.OnError == "skip" {
				findings = append(findings, store.Finding{Reason: wrapped.Error()})
				continue
			}
			return results, findings, wrapped
		}
		results[key] = data
	}
	return results, findings, nil
}

func (r *upstreamDataRunner) resolveSource(
	ctx context.Context,
	source string,
	sourceRecord *store.RunRecord,
	artifactCache map[string][]store.ArtifactRef,
) (any, error) {
	if sourceRecord == nil {
		return nil, fmt.Errorf("source run is unavailable")
	}
	switch {
	case source == "payload":
		if len(sourceRecord.Payload) == 0 {
			return nil, nil
		}
		var v any
		if err := json.Unmarshal(sourceRecord.Payload, &v); err != nil {
			return nil, fmt.Errorf("unmarshal payload: %w", err)
		}
		return v, nil

	case source == "summary":
		return sourceRecord.Summary, nil

	case source == "record":
		return map[string]any{
			"run_id":        sourceRecord.RunID,
			"task_id":       sourceRecord.TaskID,
			"task_kind":     sourceRecord.TaskKind,
			"trigger_type":  sourceRecord.TriggerType,
			"run_status":    sourceRecord.RunStatus,
			"check_status":  sourceRecord.CheckStatus,
			"started_at":    sourceRecord.StartedAt,
			"ended_at":      sourceRecord.EndedAt,
			"duration_ms":   sourceRecord.DurationMS,
			"error_message": sourceRecord.ErrorMessage,
		}, nil

	case strings.HasPrefix(source, "artifact:"):
		artifacts, err := r.ensureArtifacts(ctx, sourceRecord, artifactCache)
		if err != nil {
			return nil, err
		}
		kind := strings.TrimPrefix(source, "artifact:")
		var matched []store.ArtifactRef
		for _, ref := range artifacts {
			if kind == "*" || ref.Kind == kind {
				matched = append(matched, ref)
			}
		}
		if len(matched) == 0 {
			return nil, nil
		}
		if kind == "*" {
			var results []map[string]any
			for _, ref := range matched {
				content, err := r.readArtifactContent(ctx, ref)
				if err != nil {
					return nil, fmt.Errorf("read artifact %s: %w", ref.ArtifactID, err)
				}
				results = append(results, map[string]any{
					"kind":         ref.Kind,
					"artifact_id":  ref.ArtifactID,
					"content_type": ref.ContentType,
					"content":      content,
				})
			}
			return results, nil
		}
		if len(matched) == 1 {
			return r.readArtifactContent(ctx, matched[0])
		}
		var results []any
		for _, ref := range matched {
			content, err := r.readArtifactContent(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("read artifact %s: %w", ref.ArtifactID, err)
			}
			results = append(results, content)
		}
		return results, nil

	default:
		return nil, fmt.Errorf("unknown source type %q", source)
	}
}

func (r *upstreamDataRunner) resolveSourceRecord(ctx context.Context, sourceKey string, triggerRecord *store.RunRecord) (*store.RunRecord, error) {
	sourceTaskID := r.sourceTaskIDForKey(sourceKey)
	if triggerRecord != nil && (sourceTaskID == "" || triggerRecord.TaskID == sourceTaskID) {
		return triggerRecord, nil
	}
	if sourceTaskID == "" {
		return nil, fmt.Errorf("source_key %q is not configured and no trigger run is available", sourceKey)
	}
	runs, err := r.repo.ListRuns(ctx, sourceTaskID, 20, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list runs for source task %q: %w", sourceTaskID, err)
	}
	for i := range runs {
		if runs[i].RunStatus == "success" {
			return &runs[i], nil
		}
	}
	return nil, fmt.Errorf("source task %q has no successful run", sourceTaskID)
}

func (r *upstreamDataRunner) sourceTaskIDForKey(sourceKey string) string {
	if sourceKey == "" {
		if r.params.SourceTaskID != "" {
			return r.params.SourceTaskID
		}
		if r.spec.WatchTaskID != "" {
			return r.spec.WatchTaskID
		}
		deps := r.spec.DependencyRules()
		if len(deps) == 1 {
			return deps[0].UpstreamTaskID
		}
		return ""
	}
	for _, source := range r.params.DataSources {
		if source.Key == sourceKey {
			return source.TaskID
		}
	}
	for _, dep := range r.spec.DependencyRules() {
		if dep.SourceKey == sourceKey {
			return dep.UpstreamTaskID
		}
	}
	return ""
}

func (r *upstreamDataRunner) ensureArtifacts(
	ctx context.Context,
	sourceRecord *store.RunRecord,
	artifactCache map[string][]store.ArtifactRef,
) ([]store.ArtifactRef, error) {
	cacheKey := sourceRecord.TaskID + "/" + sourceRecord.RunID
	if artifacts, ok := artifactCache[cacheKey]; ok {
		return artifacts, nil
	}
	refs, err := r.repo.ListArtifactsByRun(ctx, sourceRecord.TaskID, sourceRecord.RunID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts for task %q run %q: %w", sourceRecord.TaskID, sourceRecord.RunID, err)
	}
	artifactCache[cacheKey] = refs
	return refs, nil
}

func (r *upstreamDataRunner) readArtifactContent(ctx context.Context, ref store.ArtifactRef) (any, error) {
	key, err := store.ObjectKeyFromURI(ref.URI)
	if err != nil {
		return nil, fmt.Errorf("parse artifact uri %q: %w", ref.URI, err)
	}
	reader, err := r.artStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get artifact %q: %w", key, err)
	}
	defer reader.Close()

	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read artifact content: %w", err)
	}
	if strings.Contains(ref.ContentType, "json") {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("unmarshal artifact json: %w", err)
		}
		return v, nil
	}
	return string(raw), nil
}

func (r *upstreamDataRunner) applyJQ(expr string, input any) (any, error) {
	return ApplyJQ(expr, input)
}

// ApplyJQ evaluates a jq expression against input data and returns the result.
func ApplyJQ(expr string, input any) (any, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("parse jq: %w", err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("compile jq: %w", err)
	}
	iter := code.Run(input)
	var results []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if e, ok := v.(error); ok {
			return nil, fmt.Errorf("jq iteration error: %w", e)
		}
		results = append(results, v)
	}
	switch len(results) {
	case 0:
		return nil, nil
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}

func (r *upstreamDataRunner) aggregate(mode string, input any) any {
	nums, ok := toFloat64Slice(input)
	if !ok || len(nums) == 0 {
		return input
	}
	switch mode {
	case "sum":
		var total float64
		for _, n := range nums {
			total += n
		}
		return total
	case "avg":
		var total float64
		for _, n := range nums {
			total += n
		}
		return total / float64(len(nums))
	case "count":
		return len(nums)
	case "min":
		minimum := nums[0]
		for _, n := range nums[1:] {
			if n < minimum {
				minimum = n
			}
		}
		return minimum
	case "max":
		maximum := nums[0]
		for _, n := range nums[1:] {
			if n > maximum {
				maximum = n
			}
		}
		return maximum
	default:
		return input
	}
}

func toFloat64Slice(v any) ([]float64, bool) {
	switch val := v.(type) {
	case float64:
		return []float64{val}, true
	case float32:
		return []float64{float64(val)}, true
	case int:
		return []float64{float64(val)}, true
	case int64:
		return []float64{float64(val)}, true
	case int32:
		return []float64{float64(val)}, true
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return nil, false
		}
		return []float64{f}, true
	case []any:
		var nums []float64
		for _, item := range val {
			n, ok := toFloat64Single(item)
			if !ok {
				return nil, false
			}
			nums = append(nums, n)
		}
		return nums, true
	case []float64:
		return val, true
	default:
		return nil, false
	}
}

func toFloat64Single(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// FetchSampleData 从上游任务的最近成功运行中获取指定数据源的样本数据。
// upstreamTaskID 是上游任务 ID（已由调用方解析）。没有可用样本时返回 available=false 和原因提示。
// 如果 jqExpr 非空，会对解析出的数据执行 JQ 求值，结果放在 JQResult 字段。
func FetchSampleData(ctx context.Context, repo store.Repository, artifactStore store.ArtifactStore, upstreamTaskID, source string, jqExpr string) (*store.SampleResponse, error) {
	runs, err := repo.ListRuns(ctx, upstreamTaskID, 20, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list runs for %q: %w", upstreamTaskID, err)
	}

	var successRecord *store.RunRecord
	for i := range runs {
		if runs[i].RunStatus == "success" {
			successRecord = &runs[i]
			break
		}
	}
	if successRecord == nil {
		return &store.SampleResponse{
			Available: false,
			TaskID:    upstreamTaskID,
			Source:    source,
			Reason:    "no_success_run",
			Message:   "上游任务尚无成功运行记录，请先触发一次运行。",
		}, nil
	}

	data, reason, message, err := resolveSampleSource(ctx, source, successRecord, artifactStore)
	if err != nil {
		return nil, err
	}
	if data == nil {
		if reason == "" {
			reason = "empty_source"
		}
		if message == "" {
			message = "所选数据源无数据。"
		}
		return &store.SampleResponse{
			Available: false,
			TaskID:    upstreamTaskID,
			RunID:     successRecord.RunID,
			Source:    source,
			Reason:    reason,
			Message:   message,
		}, nil
	}

	var jqResult any
	if jqExpr != "" && data != nil {
		if result, err := ApplyJQ(jqExpr, data); err == nil {
			jqResult = result
		}
	}
	displayData, jqPrefix := resolveSampleDisplayData(source, data)

	return &store.SampleResponse{
		Available:   true,
		TaskID:      upstreamTaskID,
		RunID:       successRecord.RunID,
		Source:      source,
		Data:        data,
		DisplayData: displayData,
		JQPrefix:    jqPrefix,
		JQResult:    jqResult,
	}, nil
}

func resolveSampleDisplayData(source string, data any) (any, string) {
	if source != "payload" && source != "artifact:payload" {
		return nil, ""
	}
	payload, ok := data.(map[string]any)
	if !ok {
		return nil, ""
	}
	body, ok := payload["body"].(string)
	if !ok || strings.TrimSpace(body) == "" {
		return nil, ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return nil, ""
	}
	return decoded, ".body | fromjson"
}

func resolveSampleSource(ctx context.Context, source string, record *store.RunRecord, artifactStore store.ArtifactStore) (any, string, string, error) {
	switch {
	case source == "payload":
		if len(record.Payload) > 0 {
			var v any
			if err := json.Unmarshal(record.Payload, &v); err != nil {
				return nil, "", "", fmt.Errorf("unmarshal payload: %w", err)
			}
			return v, "", "", nil
		}
		// Payload was externalized to artifact store — try to read it back
		for _, ref := range record.ArtifactRefs {
			if ref.Kind == "payload" {
				content, reason, message, err := readSampleArtifactContent(ctx, artifactStore, ref)
				if err != nil || content == nil {
					return content, reason, message, err
				}
				return content, "", "", nil
			}
		}
		return nil, "payload_not_saved", "上游任务最近一次成功运行未保存 Payload。请将上游任务追踪级别设为 detail/debug 后重新运行。", nil

	case strings.HasPrefix(source, "artifact:"):
		kind := strings.TrimPrefix(source, "artifact:")
		if kind == "" {
			return nil, "", "", fmt.Errorf("unsupported sample source %q", source)
		}
		var matched []store.ArtifactRef
		for _, ref := range record.ArtifactRefs {
			if kind == "*" || ref.Kind == kind {
				matched = append(matched, ref)
			}
		}
		if len(matched) == 0 {
			return nil, "artifact_not_found", "上游任务最近一次成功运行没有匹配的产物数据。", nil
		}
		if kind == "*" {
			results := make([]map[string]any, 0, len(matched))
			for _, ref := range matched {
				content, reason, message, err := readSampleArtifactContent(ctx, artifactStore, ref)
				if err != nil || content == nil {
					return content, reason, message, err
				}
				results = append(results, map[string]any{
					"kind":         ref.Kind,
					"artifact_id":  ref.ArtifactID,
					"content_type": ref.ContentType,
					"content":      content,
				})
			}
			return results, "", "", nil
		}
		if len(matched) == 1 {
			return readSampleArtifactContent(ctx, artifactStore, matched[0])
		}
		results := make([]any, 0, len(matched))
		for _, ref := range matched {
			content, reason, message, err := readSampleArtifactContent(ctx, artifactStore, ref)
			if err != nil || content == nil {
				return content, reason, message, err
			}
			results = append(results, content)
		}
		return results, "", "", nil

	case source == "summary":
		return record.Summary, "", "", nil

	case source == "record":
		return map[string]any{
			"run_id":        record.RunID,
			"task_id":       record.TaskID,
			"task_kind":     record.TaskKind,
			"trigger_type":  record.TriggerType,
			"run_status":    record.RunStatus,
			"check_status":  record.CheckStatus,
			"started_at":    record.StartedAt,
			"ended_at":      record.EndedAt,
			"duration_ms":   record.DurationMS,
			"error_message": record.ErrorMessage,
		}, "", "", nil

	default:
		return nil, "", "", fmt.Errorf("unsupported sample source %q", source)
	}
}

func readSampleArtifactContent(ctx context.Context, artifactStore store.ArtifactStore, ref store.ArtifactRef) (any, string, string, error) {
	if artifactStore == nil {
		return nil, "artifact_store_unavailable", "产物已外置存储，但当前服务未配置 artifact store。", nil
	}
	key, err := store.ObjectKeyFromURI(ref.URI)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse artifact uri: %w", err)
	}
	rc, err := artifactStore.Get(ctx, key)
	if err != nil {
		return nil, "", "", fmt.Errorf("read artifact: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", "", fmt.Errorf("read artifact body: %w", err)
	}
	if strings.Contains(ref.ContentType, "json") {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, "", "", fmt.Errorf("unmarshal artifact json: %w", err)
		}
		return v, "", "", nil
	}
	return string(raw), "", "", nil
}
