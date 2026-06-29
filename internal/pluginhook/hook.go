package pluginhook

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"pulseops/internal/config"
	"pulseops/internal/pluginconfig"
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
	mu            sync.RWMutex
	cfg           config.PluginsConfig
	hooks         []pluginmodel.Capability
	httpClient    *http.Client
	logger        *slog.Logger
	configReader  pluginconfig.ConfigReader
	runtimeStore  pluginconfig.RuntimeStore
	artifactStore store.ArtifactStore
}

func NewManager(cfg config.PluginsConfig, httpClient *http.Client, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, httpClient: httpClient, logger: logger}
}

func (m *Manager) SetConfigStore(configStore any, artifactStore store.ArtifactStore) {
	if m == nil {
		return
	}
	reader, _ := configStore.(pluginconfig.ConfigReader)
	runtimeStore, _ := configStore.(pluginconfig.RuntimeStore)
	m.mu.Lock()
	m.configReader = reader
	m.runtimeStore = runtimeStore
	m.artifactStore = artifactStore
	m.mu.Unlock()
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
	reader := m.configReader
	runtimeStore := m.runtimeStore
	artifactStore := m.artifactStore
	m.mu.RUnlock()
	for _, cap := range hooks {
		resolved, err := pluginconfig.ResolveCapabilityConfig(ctx, reader, cap, pluginconfig.ResolveCapabilityOptions{
			RuntimeStore:  runtimeStore,
			ArtifactStore: artifactStore,
		})
		if resolved.Cleanup != nil {
			defer resolved.Cleanup()
		}
		if err != nil {
			m.logger.ErrorContext(ctx, "resolve plugin hook config failed", "hook", cap.Name, "event", event.Type, "err", err)
			continue
		}
		if _, err := pluginruntime.NewClient(cap, cfg).Call(ctx, pluginruntime.Request{
			Action: "handle_event",
			Config: resolved.Config,
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
