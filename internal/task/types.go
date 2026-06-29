package task

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/store"
)

type TriggerType string

const (
	TriggerScheduled TriggerType = "scheduled"
	TriggerManual    TriggerType = "manual"
	TriggerRerun     TriggerType = "rerun"
	TriggerDependent TriggerType = "dependent"
)

type Result struct {
	CheckStatus          string
	Summary              map[string]any
	Payload              any
	Findings             []store.Finding
	Stdout               string
	Stderr               string
	PluginConfigVersions map[string]any
	PluginAssetVersions  map[string]any
	PluginTaskOverrides  map[string]any
}

type Runner interface {
	Run(ctx context.Context, trigger TriggerType) (Result, error)
}

type Driver interface {
	Kind() string
	Validate(spec config.TaskSpec) error
	NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error)
}

type RunnerDeps struct {
	BaseDir    string
	HTTPClient *http.Client
	Evaluators *evaluator.Registry
}

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewRegistry() *Registry {
	return &Registry{drivers: map[string]Driver{}}
}

func (r *Registry) Register(driver Driver) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[driver.Kind()]; exists {
		return fmt.Errorf("driver %q already exists", driver.Kind())
	}
	r.drivers[driver.Kind()] = driver
	return nil
}

func (r *Registry) Get(kind string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[kind]
	return driver, ok
}

func DecodeParams[T any](params map[string]any) (T, error) {
	var target T
	raw, err := json.Marshal(params)
	if err != nil {
		return target, fmt.Errorf("marshal params: %w", err)
	}
	if err := json.Unmarshal(raw, &target); err != nil {
		return target, fmt.Errorf("decode params: %w", err)
	}
	return target, nil
}
