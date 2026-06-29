package runtime

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
	"pulseops/internal/task"
	"pulseops/internal/trace"
)

type generationTestProvider struct {
	drivers      *task.Registry
	generationID string
}

func (p generationTestProvider) ActiveDriverRegistry() (*task.Registry, string) {
	return p.drivers, p.generationID
}

func (p generationTestProvider) ActiveEvaluatorRegistry() (*evaluator.Registry, string) {
	return evaluator.NewRegistry(), p.generationID
}

func (p generationTestProvider) ActiveCapabilities() ([]pluginmodel.Capability, string) {
	return nil, p.generationID
}

type generationTestDriver struct {
	result task.Result
	err    error
}

func (d generationTestDriver) Kind() string {
	return "generation_check"
}

func (d generationTestDriver) Validate(config.TaskSpec) error {
	return nil
}

func (d generationTestDriver) NewRunner(config.TaskSpec, task.RunnerDeps) (task.Runner, error) {
	return generationTestRunner{result: d.result, err: d.err}, nil
}

type generationTestRunner struct {
	result task.Result
	err    error
}

func (r generationTestRunner) Run(context.Context, task.TriggerType) (task.Result, error) {
	return r.result, r.err
}

type generationRefStore struct {
	mu       sync.Mutex
	acquires []string
	releases []string
	records  []store.RunRecord
}

func (s *generationRefStore) AcquirePluginGeneration(_ context.Context, generationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquires = append(s.acquires, generationID)
	return nil
}

func (s *generationRefStore) ReleasePluginGeneration(_ context.Context, generationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, generationID)
	return nil
}

func (s *generationRefStore) InsertRun(_ context.Context, record store.RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *generationRefStore) snapshot() (acquires, releases []string, records []store.RunRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.acquires...), append([]string(nil), s.releases...), append([]store.RunRecord(nil), s.records...)
}

func TestManagerRunTaskBindsPluginGenerationAndReleasesRef(t *testing.T) {
	t.Parallel()

	registry := task.NewRegistry()
	if err := registry.Register(generationTestDriver{result: task.Result{
		CheckStatus: "pass",
		Summary:     map[string]any{"driver": "generation"},
		Payload:     map[string]any{"ok": true},
	}}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	st := &generationRefStore{}
	tracer := trace.NewManager(slog.Default(), nil, 4096)
	tracer.Register(trace.NewPostgresSink("postgres", st))
	manager := newGenerationTestManager(registry, st, tracer)
	manager.SetPluginGenerationProvider(generationTestProvider{drivers: registry, generationID: "plugin-gen-42"})

	if err := manager.UpsertTaskSpec(context.Background(), config.TaskSpec{
		ID:      "task-generation",
		Name:    "Task Generation",
		Kind:    "generation_check",
		Enabled: true,
		Trigger: "manual",
		Trace:   config.TracePolicy{Level: "detail"},
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	t.Cleanup(manager.Close)

	record, err := manager.RunTask(context.Background(), "task-generation", task.TriggerManual)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if record.PluginGenerationID != "plugin-gen-42" {
		t.Fatalf("record generation=%q, want plugin-gen-42", record.PluginGenerationID)
	}
	acquires, releases, records := st.snapshot()
	assertGenerationRefs(t, acquires, releases, "plugin-gen-42")
	if len(records) != 1 || records[0].PluginGenerationID != "plugin-gen-42" {
		t.Fatalf("expected persisted run with generation id, got %#v", records)
	}
}

func TestManagerTestRunTaskDefinitionBindsPluginGenerationAndReleasesRef(t *testing.T) {
	t.Parallel()

	registry := task.NewRegistry()
	if err := registry.Register(generationTestDriver{result: task.Result{
		CheckStatus: "pass",
		Summary:     map[string]any{"dry_run": true},
		Payload:     map[string]any{"ok": true},
	}}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	st := &generationRefStore{}
	manager := newGenerationTestManager(registry, st, trace.NewManager(slog.Default(), nil, 4096))
	manager.SetPluginGenerationProvider(generationTestProvider{drivers: registry, generationID: "plugin-gen-dry"})

	record, err := manager.TestRunTaskDefinition(context.Background(), config.TaskDefinition{
		TaskID:  "dry-generation",
		Name:    "Dry Generation",
		Kind:    "generation_check",
		Enabled: true,
		Trigger: "manual",
	})
	if err != nil {
		t.Fatalf("test run definition: %v", err)
	}
	if record.PluginGenerationID != "plugin-gen-dry" {
		t.Fatalf("record generation=%q, want plugin-gen-dry", record.PluginGenerationID)
	}
	acquires, releases, records := st.snapshot()
	assertGenerationRefs(t, acquires, releases, "plugin-gen-dry")
	if len(records) != 0 {
		t.Fatalf("dry run should not persist run records, got %#v", records)
	}
}

func assertGenerationRefs(t *testing.T, acquires, releases []string, want string) {
	t.Helper()
	if len(acquires) != 1 || acquires[0] != want {
		t.Fatalf("acquires=%v, want [%s]", acquires, want)
	}
	if len(releases) != 1 || releases[0] != want {
		t.Fatalf("releases=%v, want [%s]", releases, want)
	}
}

func newGenerationTestManager(registry *task.Registry, st store.Repository, tracer *trace.Manager) *Manager {
	return &Manager{
		rootCtx:  context.Background(),
		logger:   slog.Default(),
		drivers:  registry,
		deps:     task.RunnerDeps{},
		store:    st,
		tracer:   tracer,
		metrics:  newGenerationTestMetrics(),
		tasks:    map[string]*managedTask{},
		pathToID: map[string]string{},
		depTasks: map[string][]managedDependency{},
	}
}

func newGenerationTestMetrics() *Metrics {
	return &Metrics{
		tasksLoaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pulseops_generation_test_tasks_loaded",
			Help: "Generation test loaded tasks.",
		}),
		taskRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_generation_test_task_runs_total",
			Help: "Generation test task runs.",
		}, []string{"task_id", "kind", "run_status", "check_status"}),
		taskLastDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulseops_generation_test_task_last_duration_ms",
			Help: "Generation test task duration.",
		}, []string{"task_id", "kind"}),
	}
}

var _ store.Repository = (*generationRefStore)(nil)
var _ pluginGenerationRefStore = (*generationRefStore)(nil)

func (s *generationRefStore) Close() error { return nil }
func (s *generationRefStore) UpsertTaskState(context.Context, store.TaskState) error {
	return nil
}
func (s *generationRefStore) DeleteTaskState(context.Context, string) error { return nil }
func (s *generationRefStore) ListRuns(context.Context, string, int, int, time.Duration) ([]store.RunRecord, error) {
	return nil, nil
}
func (s *generationRefStore) CountRuns(context.Context, string, time.Duration) (int, error) {
	return 0, nil
}
func (s *generationRefStore) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return nil, nil
}
func (s *generationRefStore) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return nil, 0, nil
}
func (s *generationRefStore) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return nil, nil
}
func (s *generationRefStore) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (s *generationRefStore) GetRun(context.Context, string, string) (store.RunRecord, error) {
	return store.RunRecord{}, sql.ErrNoRows
}
func (s *generationRefStore) ListArtifactsByRun(context.Context, string, string) ([]store.ArtifactRef, error) {
	return nil, nil
}
func (s *generationRefStore) GetArtifact(context.Context, string) (store.ArtifactRef, error) {
	return store.ArtifactRef{}, sql.ErrNoRows
}
func (s *generationRefStore) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (s *generationRefStore) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error {
	return nil
}
func (s *generationRefStore) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (s *generationRefStore) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (s *generationRefStore) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (s *generationRefStore) SetMeta(context.Context, string, string) error { return nil }
func (s *generationRefStore) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{}, nil
}
func (s *generationRefStore) SaveGlobalSettings(context.Context, config.GlobalSettings) error {
	return nil
}
func (s *generationRefStore) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (s *generationRefStore) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}
func (s *generationRefStore) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (s *generationRefStore) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (s *generationRefStore) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (s *generationRefStore) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (s *generationRefStore) DeleteTaskDefinition(context.Context, string) error { return nil }
func (s *generationRefStore) ListPipelines(context.Context) ([]config.Pipeline, error) {
	return nil, nil
}
func (s *generationRefStore) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (s *generationRefStore) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (s *generationRefStore) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (s *generationRefStore) DeletePipeline(context.Context, string) error          { return nil }
func (s *generationRefStore) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return nil, nil
}
func (s *generationRefStore) UpdateTaskPipeline(context.Context, string, *string) error {
	return nil
}
func (s *generationRefStore) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return nil, nil
}
func (s *generationRefStore) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return nil, nil
}
func (s *generationRefStore) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (s *generationRefStore) UpsertTaskDependency(context.Context, config.TaskDependency) (config.TaskDependency, error) {
	return config.TaskDependency{}, nil
}
func (s *generationRefStore) DeleteTaskDependency(context.Context, string) error { return nil }
