package datasource

import (
	"context"
	"net/http"
	"sync"
)

type Spec struct {
	Type    string
	Config  map[string]any
	Alias   string
	OnError string
}

type FetchDeps struct {
	HTTPClient    *http.Client
	CurrentRunID  string
	CurrentTaskID string
	TriggerType   string
}

type Source interface {
	Name() string
	Fetch(ctx context.Context, spec Spec, deps FetchDeps) (any, error)
}

type Validator interface {
	ValidateSpec(spec Spec) error
}

type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}}
}

func (r *Registry) Register(name string, source Source) {
	if r == nil || source == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[name] = source
}

func (r *Registry) Get(name string) (Source, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	source, ok := r.sources[name]
	return source, ok
}

func (r *Registry) Snapshot() map[string]Source {
	if r == nil {
		return map[string]Source{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Source, len(r.sources))
	for name, source := range r.sources {
		out[name] = source
	}
	return out
}
