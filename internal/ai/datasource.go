package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"pulseops/internal/store"
)

type FetchDeps struct {
	TriggerRun   *store.RunRecord
	DBRepository store.Repository
	HTTPClient   *http.Client
}

type DataSource interface {
	Name() string
	Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error)
}

type DataSourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]DataSource
}

func NewDataSourceRegistry() *DataSourceRegistry {
	r := &DataSourceRegistry{sources: map[string]DataSource{}}
	r.registerBuiltins()
	return r
}

func (r *DataSourceRegistry) Register(name string, source DataSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[name] = source
}

func (r *DataSourceRegistry) Get(name string) (DataSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[name]
	return s, ok
}

func (r *DataSourceRegistry) registerBuiltins() {
	r.Register("run_context", &runContextSource{})
	r.Register("run_history", &runHistorySource{})
	r.Register("previous_analysis", &previousAnalysisSource{})
	r.Register("http_call", &httpCallSource{})
	r.Register("upstream_output", &upstreamOutputSource{})
}

type runContextSource struct{}

func (s *runContextSource) Name() string { return "run_context" }

func (s *runContextSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	if deps.TriggerRun == nil {
		return nil, fmt.Errorf("run_context data source requires a trigger run")
	}
	return *deps.TriggerRun, nil
}

type runHistorySource struct{}

func (s *runHistorySource) Name() string { return "run_history" }

func (s *runHistorySource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	if deps.DBRepository == nil {
		return nil, fmt.Errorf("run_history requires database access")
	}
	taskID, _ := spec.Config["task_id"].(string)
	if taskID == "" {
		taskIDFromDeps, _ := spec.Config["watch_task"].(string)
		taskID = taskIDFromDeps
	}
	if taskID == "" && deps.TriggerRun != nil {
		taskID = deps.TriggerRun.TaskID
	}
	if taskID == "" {
		return nil, fmt.Errorf("run_history requires a task_id")
	}
	limit := 20
	if l, ok := spec.Config["limit"].(float64); ok {
		limit = int(l)
	}
	if l, ok := spec.Config["limit"].(int); ok {
		limit = l
	}
	return deps.DBRepository.ListRuns(ctx, taskID, limit, 0, 0)
}

type previousAnalysisSource struct{}

func (s *previousAnalysisSource) Name() string { return "previous_analysis" }

func (s *previousAnalysisSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	if deps.DBRepository == nil {
		return nil, fmt.Errorf("previous_analysis requires database access")
	}
	taskID, _ := spec.Config["task_id"].(string)
	if taskID == "" && deps.TriggerRun != nil {
		taskID = deps.TriggerRun.TaskID
	}
	if taskID == "" {
		return nil, fmt.Errorf("previous_analysis requires a task_id")
	}
	limit := 5
	if l, ok := spec.Config["limit"].(float64); ok {
		limit = int(l)
	}
	return deps.DBRepository.ListAIAnalyses(ctx, taskID, limit)
}

type upstreamOutputSource struct{}

func (s *upstreamOutputSource) Name() string { return "upstream_output" }

func (s *upstreamOutputSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	if deps.TriggerRun == nil {
		return nil, fmt.Errorf("upstream_output requires a trigger run context (task must be triggered by an upstream task)")
	}
	result := map[string]any{
		"run_id":       deps.TriggerRun.RunID,
		"task_id":      deps.TriggerRun.TaskID,
		"run_status":   deps.TriggerRun.RunStatus,
		"check_status": deps.TriggerRun.CheckStatus,
		"summary":      deps.TriggerRun.Summary,
	}
	if len(deps.TriggerRun.Payload) > 0 {
		var payload any
		if err := json.Unmarshal(deps.TriggerRun.Payload, &payload); err == nil {
			result["payload"] = payload
		} else {
			result["payload"] = string(deps.TriggerRun.Payload)
		}
	}
	return result, nil
}
