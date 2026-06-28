package pluginhook

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

const (
	EventRunStarted   = "run.started"
	EventRunFinished  = "run.finished"
	EventTaskUpdated  = "task.updated"
	EventPluginLoaded = "plugin.loaded"
)

type Event struct {
	Type         string           `json:"type"`
	GenerationID string           `json:"generation_id,omitempty"`
	TaskID       string           `json:"task_id,omitempty"`
	TaskKind     string           `json:"task_kind,omitempty"`
	RunID        string           `json:"run_id,omitempty"`
	TriggerType  string           `json:"trigger_type,omitempty"`
	Task         *config.TaskSpec `json:"task,omitempty"`
	Record       *store.RunRecord `json:"record,omitempty"`
	Data         map[string]any   `json:"data,omitempty"`
}

type Manager struct {
	mu         sync.RWMutex
	cfg        config.PluginsConfig
	hooks      []pluginmodel.Capability
	httpClient *http.Client
	logger     *slog.Logger
}

func NewManager(cfg config.PluginsConfig, httpClient *http.Client, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, httpClient: httpClient, logger: logger}
}

func (m *Manager) SyncPluginHooks(caps []pluginmodel.Capability, cfg config.PluginsConfig) {
	var hooks []pluginmodel.Capability
	for _, cap := range caps {
		if cap.Type == pluginmodel.CapabilityHook && isPluginRuntime(cap.Runtime) {
			hooks = append(hooks, cap)
		}
	}
	m.mu.Lock()
	m.cfg = cfg
	m.hooks = hooks
	m.mu.Unlock()
}

func (m *Manager) Dispatch(ctx context.Context, event Event) {
	if m == nil || event.Type == "" {
		return
	}
	m.mu.RLock()
	hooks := append([]pluginmodel.Capability(nil), m.hooks...)
	cfg := m.cfg
	client := m.httpClient
	m.mu.RUnlock()
	for _, cap := range hooks {
		if _, err := pluginruntime.NewClient(cap, cfg).Call(ctx, pluginruntime.Request{
			Action: "handle_event",
			Config: cloneMap(cap.Defaults),
			Input: map[string]any{
				"event": event,
			},
		}, pluginruntime.Deps{
			HTTPClient:    client,
			CurrentRunID:  event.RunID,
			CurrentTaskID: event.TaskID,
			TriggerType:   event.TriggerType,
		}); err != nil {
			m.logger.ErrorContext(ctx, "dispatch plugin hook failed", "hook", cap.Name, "event", event.Type, "err", err)
		}
	}
}

func isPluginRuntime(runtime string) bool {
	return runtime == "process" || runtime == "http" || runtime == "http_plugin"
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
