package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/itchyny/gojq"

	"pulseops/internal/config"
	"pulseops/internal/ctxkey"
	"pulseops/internal/store"
)

type UpstreamDataParams struct {
	SourceTaskID string        `json:"source_task_id"`
	ExtractExprs []ExtractExpr `json:"extract_exprs"`
}

type ExtractExpr struct {
	Field   string `json:"field"`
	Source  string `json:"source"`
	JQExpr  string `json:"jq_expr"`
	AggMode string `json:"agg_mode"`
}

type UpstreamDataDriver struct {
	repo     store.Repository
	artStore store.ArtifactStore
	logger   *slog.Logger
}

func NewUpstreamDataDriver(repo store.Repository, artStore store.ArtifactStore, logger *slog.Logger) *UpstreamDataDriver {
	return &UpstreamDataDriver{repo: repo, artStore: artStore, logger: logger}
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
		switch {
		case expr.Source == "payload", expr.Source == "summary", expr.Source == "record", strings.HasPrefix(expr.Source, "artifact:"):
		default:
			return fmt.Errorf("data_process extract_expr[%d].source %q must be 'payload', 'summary', 'record', or 'artifact:<kind>'", i, expr.Source)
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
		spec:     spec,
		params:   params,
		repo:     d.repo,
		artStore: d.artStore,
		logger:   d.logger,
		deps:     deps,
	}, nil
}

type upstreamDataRunner struct {
	spec     config.TaskSpec
	params   UpstreamDataParams
	repo     store.Repository
	artStore store.ArtifactStore
	logger   *slog.Logger
	deps     RunnerDeps
}

func (r *upstreamDataRunner) Run(ctx context.Context, _ TriggerType) (Result, error) {
	sourceRecord, ok := ctx.Value(ctxkey.CtxTriggerRun).(*store.RunRecord)
	if !ok || sourceRecord == nil {
		return Result{CheckStatus: "fail"}, fmt.Errorf("data_process requires a trigger run in context")
	}

	sourceTaskID := r.params.SourceTaskID
	if sourceTaskID == "" {
		sourceTaskID = r.spec.WatchTaskID
	}

	summary := map[string]any{}
	var findings []store.Finding
	var artifacts []store.ArtifactRef

	for _, expr := range r.params.ExtractExprs {
		srcData, err := r.resolveSource(ctx, expr.Source, sourceTaskID, sourceRecord, &artifacts)
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

func (r *upstreamDataRunner) resolveSource(
	ctx context.Context,
	source string,
	sourceTaskID string,
	sourceRecord *store.RunRecord,
	artifacts *[]store.ArtifactRef,
) (any, error) {
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
		if err := r.ensureArtifacts(ctx, sourceTaskID, sourceRecord, artifacts); err != nil {
			return nil, err
		}
		kind := strings.TrimPrefix(source, "artifact:")
		var matched []store.ArtifactRef
		for _, ref := range *artifacts {
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

func (r *upstreamDataRunner) ensureArtifacts(
	ctx context.Context,
	sourceTaskID string,
	sourceRecord *store.RunRecord,
	artifacts *[]store.ArtifactRef,
) error {
	if len(*artifacts) > 0 {
		return nil
	}
	refs, err := r.repo.ListArtifactsByRun(ctx, sourceTaskID, sourceRecord.RunID)
	if err != nil {
		return fmt.Errorf("list artifacts for task %q run %q: %w", sourceTaskID, sourceRecord.RunID, err)
	}
	*artifacts = refs
	return nil
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
// upstreamTaskID 是上游任务 ID（已由调用方解析）。返回 nil 表示没有成功运行。
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
		return &store.SampleResponse{Available: false}, nil
	}

	data, err := resolveSampleSource(ctx, source, successRecord, artifactStore)
	if err != nil {
		return nil, err
	}

	var jqResult any
	if jqExpr != "" && data != nil {
		if result, err := ApplyJQ(jqExpr, data); err == nil {
			jqResult = result
		}
	}

	return &store.SampleResponse{
		Available: true,
		TaskID:    upstreamTaskID,
		RunID:     successRecord.RunID,
		Source:    source,
		Data:      data,
		JQResult:  jqResult,
	}, nil
}

func resolveSampleSource(ctx context.Context, source string, record *store.RunRecord, artifactStore store.ArtifactStore) (any, error) {
	switch {
	case source == "payload":
		if len(record.Payload) > 0 {
			var v any
			if err := json.Unmarshal(record.Payload, &v); err != nil {
				return nil, fmt.Errorf("unmarshal payload: %w", err)
			}
			return v, nil
		}
		// Payload was externalized to artifact store — try to read it back
		for _, ref := range record.ArtifactRefs {
			if ref.Kind == "payload" && artifactStore != nil {
				rc, err := artifactStore.Get(ctx, ref.URI)
				if err != nil {
					return nil, fmt.Errorf("read payload artifact: %w", err)
				}
				defer rc.Close()
				raw, err := io.ReadAll(rc)
				if err != nil {
					return nil, fmt.Errorf("read payload artifact body: %w", err)
				}
				var v any
				if err := json.Unmarshal(raw, &v); err != nil {
					return nil, fmt.Errorf("unmarshal artifact payload: %w", err)
				}
				return v, nil
			}
		}
		return nil, nil

	case source == "summary":
		return record.Summary, nil

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
		}, nil

	default:
		return nil, fmt.Errorf("unsupported sample source %q", source)
	}
}
