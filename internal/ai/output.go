package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"pulseops/internal/store"
)

type OutputDeps struct {
	DBRepository  store.Repository
	ArtifactStore store.ArtifactStore
	HTTPClient    *http.Client
	CurrentRunID  string
	CurrentTaskID string
}

type OutputInput struct {
	RawResponse string
	ParsedJSON  map[string]any
	RunID       string
	TaskID      string
	TokensIn    int
	TokensOut   int
	DurationMS  int64
}

type OutputResult struct {
	Findings []store.Finding
	Summary  map[string]any
}

type OutputWriter interface {
	Name() string
	Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error)
}

type OutputWriterRegistry struct {
	mu      sync.RWMutex
	writers map[string]OutputWriter
}

func NewOutputWriterRegistry() *OutputWriterRegistry {
	r := &OutputWriterRegistry{writers: map[string]OutputWriter{}}
	r.registerBuiltins()
	return r
}

func (r *OutputWriterRegistry) Register(name string, writer OutputWriter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writers[name] = writer
}

func (r *OutputWriterRegistry) Get(name string) (OutputWriter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.writers[name]
	return w, ok
}

func (r *OutputWriterRegistry) Snapshot() map[string]OutputWriter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]OutputWriter, len(r.writers))
	for name, writer := range r.writers {
		out[name] = writer
	}
	return out
}

func (r *OutputWriterRegistry) registerBuiltins() {
	r.Register("summary", &summaryWriter{})
	r.Register("findings", &findingsWriter{})
	r.Register("artifact", &artifactWriter{})
}

type summaryWriter struct{}

func (w *summaryWriter) Name() string { return "summary" }

func (w *summaryWriter) Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error) {
	field, _ := spec.Config["field"].(string)
	if field == "" {
		field = "ai_analysis"
	}
	if input.ParsedJSON != nil {
		value, ok := input.ParsedJSON[field]
		if ok {
			return &OutputResult{
				Summary: map[string]any{field: value},
			}, nil
		}
	}
	return &OutputResult{
		Summary: map[string]any{field: input.RawResponse},
	}, nil
}

type findingsWriter struct{}

func (w *findingsWriter) Name() string { return "findings" }

func (w *findingsWriter) Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error) {
	jsonText := input.RawResponse
	if input.ParsedJSON != nil {
		raw, err := json.Marshal(input.ParsedJSON)
		if err != nil {
			return nil, fmt.Errorf("marshal parsed json for findings: %w", err)
		}
		jsonText = string(raw)
	}
	jsonText = strings.TrimSpace(jsonText)
	if strings.HasPrefix(jsonText, "[") {
		var findings []store.Finding
		if err := json.Unmarshal([]byte(jsonText), &findings); err != nil {
			return &OutputResult{}, nil
		}
		for i := range findings {
			if findings[i].RunID == "" {
				findings[i].RunID = deps.CurrentRunID
			}
			if findings[i].TaskID == "" {
				findings[i].TaskID = deps.CurrentTaskID
			}
		}
		return &OutputResult{Findings: findings}, nil
	}
	if strings.HasPrefix(jsonText, "{") {
		var finding store.Finding
		if err := json.Unmarshal([]byte(jsonText), &finding); err != nil {
			return &OutputResult{}, nil
		}
		if finding.RunID == "" {
			finding.RunID = deps.CurrentRunID
		}
		if finding.TaskID == "" {
			finding.TaskID = deps.CurrentTaskID
		}
		return &OutputResult{Findings: []store.Finding{finding}}, nil
	}
	return &OutputResult{}, nil
}

type artifactWriter struct{}

func (w *artifactWriter) Name() string { return "artifact" }

func (w *artifactWriter) Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error) {
	return &OutputResult{}, nil
}
