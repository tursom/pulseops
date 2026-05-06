package evaluator

import (
	"context"
	"fmt"
	"sync"
)

type Input struct {
	TaskID       string
	TaskParams   map[string]any
	SourceItems  []map[string]any
	SampledItems []map[string]any
	FanoutItems  []FanoutItem
}

type FanoutItem struct {
	Item   map[string]any `json:"item"`
	Detail any            `json:"detail,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type Result struct {
	CheckStatus string           `json:"check_status"`
	Summary     map[string]any   `json:"summary"`
	Findings    []map[string]any `json:"findings"`
}

type ScenarioEvaluator interface {
	Name() string
	Evaluate(ctx context.Context, input Input) (Result, error)
}

type Registry struct {
	mu         sync.RWMutex
	evaluators map[string]ScenarioEvaluator
}

func NewRegistry() *Registry {
	return &Registry{evaluators: map[string]ScenarioEvaluator{}}
}

func (r *Registry) Register(evaluator ScenarioEvaluator) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := evaluator.Name()
	if _, exists := r.evaluators[name]; exists {
		return fmt.Errorf("evaluator %q already registered", name)
	}
	r.evaluators[name] = evaluator
	return nil
}

func (r *Registry) Get(name string) (ScenarioEvaluator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	evaluator, ok := r.evaluators[name]
	return evaluator, ok
}
