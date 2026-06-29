package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

type fakeTemplateDriver struct{}

func (fakeTemplateDriver) Kind() string { return "template_kind" }
func (fakeTemplateDriver) Validate(config.TaskSpec) error {
	return nil
}
func (fakeTemplateDriver) NewRunner(config.TaskSpec, task.RunnerDeps) (task.Runner, error) {
	return nil, errors.New("not implemented")
}

type fakeTemplateProvider struct {
	drivers *task.Registry
	caps    []pluginmodel.Capability
}

func (p fakeTemplateProvider) ActiveDriverRegistry() (*task.Registry, string) {
	return p.drivers, "gen-test"
}
func (p fakeTemplateProvider) ActiveEvaluatorRegistry() (*evaluator.Registry, string) {
	return evaluator.NewRegistry(), "gen-test"
}
func (p fakeTemplateProvider) ActiveCapabilities() ([]pluginmodel.Capability, string) {
	return append([]pluginmodel.Capability(nil), p.caps...), "gen-test"
}

type fakeTemplateStore struct {
	instances map[string]pluginmodel.ConfigInstanceRecord
	versions  map[string]pluginmodel.ConfigVersionRecord
}

func (s fakeTemplateStore) Close() error { return nil }
func (s fakeTemplateStore) UpsertTaskState(context.Context, store.TaskState) error {
	return nil
}
func (s fakeTemplateStore) DeleteTaskState(context.Context, string) error    { return nil }
func (s fakeTemplateStore) InsertRun(context.Context, store.RunRecord) error { return nil }
func (s fakeTemplateStore) ListRuns(context.Context, string, int, int, time.Duration) ([]store.RunRecord, error) {
	return nil, nil
}
func (s fakeTemplateStore) CountRuns(context.Context, string, time.Duration) (int, error) {
	return 0, nil
}
func (s fakeTemplateStore) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return nil, nil
}
func (s fakeTemplateStore) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return nil, 0, nil
}
func (s fakeTemplateStore) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return nil, nil
}
func (s fakeTemplateStore) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (s fakeTemplateStore) GetRun(context.Context, string, string) (store.RunRecord, error) {
	return store.RunRecord{}, sql.ErrNoRows
}
func (s fakeTemplateStore) ListArtifactsByRun(context.Context, string, string) ([]store.ArtifactRef, error) {
	return nil, nil
}
func (s fakeTemplateStore) GetArtifact(context.Context, string) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, sql.ErrNoRows
}
func (s fakeTemplateStore) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (s fakeTemplateStore) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error {
	return nil
}
func (s fakeTemplateStore) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (s fakeTemplateStore) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (s fakeTemplateStore) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (s fakeTemplateStore) SetMeta(context.Context, string, string) error { return nil }
func (s fakeTemplateStore) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{}, nil
}
func (s fakeTemplateStore) SaveGlobalSettings(context.Context, config.GlobalSettings) error {
	return nil
}
func (s fakeTemplateStore) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (s fakeTemplateStore) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}
func (s fakeTemplateStore) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (s fakeTemplateStore) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (s fakeTemplateStore) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (s fakeTemplateStore) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (s fakeTemplateStore) DeleteTaskDefinition(context.Context, string) error { return nil }
func (s fakeTemplateStore) ListPipelines(context.Context) ([]config.Pipeline, error) {
	return nil, nil
}
func (s fakeTemplateStore) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (s fakeTemplateStore) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (s fakeTemplateStore) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (s fakeTemplateStore) DeletePipeline(context.Context, string) error          { return nil }
func (s fakeTemplateStore) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (s fakeTemplateStore) UpdateTaskPipeline(context.Context, string, *string) error {
	return nil
}
func (s fakeTemplateStore) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return nil, nil
}
func (s fakeTemplateStore) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return nil, nil
}
func (s fakeTemplateStore) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (s fakeTemplateStore) UpsertTaskDependency(context.Context, config.TaskDependency) (config.TaskDependency, error) {
	return config.TaskDependency{}, nil
}
func (s fakeTemplateStore) DeleteTaskDependency(context.Context, string) error { return nil }

func (s fakeTemplateStore) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	record, ok := s.instances[instanceID]
	if !ok {
		return pluginmodel.ConfigInstanceRecord{}, errors.New("not found")
	}
	return record, nil
}

func (s fakeTemplateStore) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	record, ok := s.versions[instanceID]
	if !ok {
		return pluginmodel.ConfigVersionRecord{}, errors.New("not found")
	}
	return record, nil
}

func TestValidateTaskTemplateConfigRefs(t *testing.T) {
	t.Parallel()

	capability := pluginmodel.Capability{
		ID:       "plugin-a:task_template:inventory-template",
		Type:     pluginmodel.CapabilityTaskTemplate,
		Name:     "inventory-template",
		Kind:     "template_kind",
		PluginID: "plugin-a",
		Status:   "active",
		Enabled:  true,
		Schema:   pluginmodel.Schema{"target": {Type: "string", Required: true}},
		Config: &pluginmodel.ConfigSchema{AllowPluginConfigRef: true, Fields: map[string]pluginmodel.ConfigField{
			"endpoint": {Type: "string", Required: true},
			"timeout":  {Type: "number", Overridable: true},
			"token":    {Type: "string"},
		}},
	}
	registry := task.NewRegistry()
	if err := registry.Register(fakeTemplateDriver{}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	manager := NewManager(context.Background(), config.Config{}, slog.Default(), registry, task.RunnerDeps{}, fakeTemplateStore{
		instances: map[string]pluginmodel.ConfigInstanceRecord{
			"common": {
				ID:            "common",
				PluginID:      "plugin-a",
				Scope:         "plugin",
				Status:        "active",
				ActiveVersion: 1,
			},
			"cap": {
				ID:             "cap",
				PluginID:       "plugin-a",
				CapabilityID:   capability.ID,
				CapabilityType: pluginmodel.CapabilityTaskTemplate,
				CapabilityName: "inventory-template",
				Scope:          "capability",
				Status:         "active",
				ActiveVersion:  2,
			},
		},
		versions: map[string]pluginmodel.ConfigVersionRecord{
			"common": {InstanceID: "common", Version: 1, Status: "active", Values: map[string]any{"endpoint": "https://api.test"}},
			"cap":    {InstanceID: "cap", Version: 2, Status: "active", Values: map[string]any{"timeout": 3}},
		},
	}, nil)
	manager.SetPluginGenerationProvider(fakeTemplateProvider{drivers: registry, caps: []pluginmodel.Capability{capability}})

	def := config.TaskDefinition{
		TaskID:  "task-a",
		Name:    "Task A",
		Kind:    "template_kind",
		Enabled: true,
		Params: map[string]any{
			"target":                "inventory",
			"plugin_template_ref":   map[string]any{"capability_id": capability.ID},
			"plugin_config_ref":     "common",
			"capability_config_ref": "cap",
			"overrides":             map[string]any{"timeout": 5},
		},
	}
	def.ParamsJSON = mustJSON(t, def.Params)
	if _, err := manager.ValidateTaskDefinition(def); err != nil {
		t.Fatalf("validate template task: %v", err)
	}

	def.Params["overrides"] = map[string]any{"token": "inline"}
	def.ParamsJSON = mustJSON(t, def.Params)
	_, err := manager.ValidateTaskDefinition(def)
	if err == nil || !strings.Contains(err.Error(), "not overridable") {
		t.Fatalf("expected non-overridable override error, got %v", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}
