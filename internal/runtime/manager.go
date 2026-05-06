package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"pulseops/internal/config"
	"pulseops/internal/store"
	"pulseops/internal/task"
	"pulseops/internal/trace"
)

type Manager struct {
	rootCtx context.Context
	cfg     config.Config
	logger  *slog.Logger
	drivers *task.Registry
	deps    task.RunnerDeps
	store   store.Repository
	tracer  *trace.Manager
	metrics *Metrics

	mu       sync.RWMutex
	tasks    map[string]*managedTask
	pathToID map[string]string
}

func NewManager(
	rootCtx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	drivers *task.Registry,
	deps task.RunnerDeps,
	store store.Repository,
	tracer *trace.Manager,
) *Manager {
	return &Manager{
		rootCtx:  rootCtx,
		cfg:      cfg,
		logger:   logger,
		drivers:  drivers,
		deps:     deps,
		store:    store,
		tracer:   tracer,
		metrics:  NewMetrics(),
		tasks:    map[string]*managedTask{},
		pathToID: map[string]string{},
	}
}

func (m *Manager) LoadAll(ctx context.Context) error {
	specs, err := config.LoadTaskSpecs(m.cfg)
	if err != nil {
		return err
	}
	var loadErrs []error
	for _, spec := range specs {
		if err := m.UpsertTaskSpec(ctx, spec); err != nil {
			loadErrs = append(loadErrs, err)
		}
	}
	return errors.Join(loadErrs...)
}

func (m *Manager) UpsertTaskFromPath(ctx context.Context, path string) error {
	spec, err := config.LoadTaskSpec(m.cfg, path)
	if err != nil {
		m.markReloadFailure(ctx, path, err)
		return err
	}
	if err := m.UpsertTaskSpec(ctx, spec); err != nil {
		m.markReloadFailure(ctx, path, err)
		return err
	}
	return nil
}

func (m *Manager) UpsertTaskSpec(ctx context.Context, spec config.TaskSpec) error {
	driver, ok := m.drivers.Get(spec.Kind)
	if !ok {
		return fmt.Errorf("task %s driver %q not found", spec.ID, spec.Kind)
	}
	if err := driver.Validate(spec); err != nil {
		return fmt.Errorf("validate task %s: %w", spec.ID, err)
	}
	candidate, err := m.newManagedTask(spec)
	if err != nil {
		return err
	}

	var toStop []*managedTask
	m.mu.Lock()
	oldByPathID := m.pathToID[spec.SourcePath]
	oldByPath := m.tasks[oldByPathID]
	if oldByPath != nil && oldByPath.spec.SourceHash == spec.SourceHash && oldByPath.spec.ID == spec.ID {
		m.mu.Unlock()
		candidate.stop()
		return nil
	}
	if existingByID := m.tasks[spec.ID]; existingByID != nil && existingByID.spec.SourcePath != spec.SourcePath {
		m.mu.Unlock()
		candidate.stop()
		return fmt.Errorf("task id %q already bound to %s", spec.ID, existingByID.spec.SourcePath)
	}
	if oldByPath != nil {
		delete(m.tasks, oldByPath.spec.ID)
		toStop = append(toStop, oldByPath)
	}
	if oldByID := m.tasks[spec.ID]; oldByID != nil {
		toStop = appendUniqueTask(toStop, oldByID)
	}
	m.tasks[spec.ID] = candidate
	m.pathToID[spec.SourcePath] = spec.ID
	m.metrics.tasksLoaded.Set(float64(len(m.tasks)))
	m.mu.Unlock()

	candidate.start()
	candidate.clearReloadError(ctx)
	for _, item := range toStop {
		item.stop()
	}
	return nil
}

func (m *Manager) RemoveTaskByPath(ctx context.Context, path string) error {
	m.mu.Lock()
	id, ok := m.pathToID[path]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	taskEntry := m.tasks[id]
	delete(m.pathToID, path)
	delete(m.tasks, id)
	m.metrics.tasksLoaded.Set(float64(len(m.tasks)))
	m.mu.Unlock()
	if taskEntry != nil {
		taskEntry.stop()
	}
	if err := m.store.DeleteTaskState(ctx, id); err != nil {
		return err
	}
	return nil
}

func (m *Manager) RunTask(ctx context.Context, id string, trigger task.TriggerType) (store.RunRecord, error) {
	entry, err := m.getTask(id)
	if err != nil {
		return store.RunRecord{}, err
	}
	return entry.run(ctx, trigger)
}

func (m *Manager) ReloadTask(ctx context.Context, id string) error {
	entry, err := m.getTask(id)
	if err != nil {
		return err
	}
	return m.UpsertTaskFromPath(ctx, entry.spec.SourcePath)
}

func (m *Manager) SetTaskEnabled(ctx context.Context, id string, enabled bool) error {
	entry, err := m.getTask(id)
	if err != nil {
		return err
	}
	return entry.setEnabled(ctx, enabled)
}

func (m *Manager) GetTask(id string) (store.TaskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.tasks[id]
	if !ok {
		return store.TaskState{}, false
	}
	return entry.snapshot(), true
}

func (m *Manager) ListTasks() []store.TaskState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]store.TaskState, 0, len(m.tasks))
	for _, entry := range m.tasks {
		items = append(items, entry.snapshot())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TaskID < items[j].TaskID })
	return items
}

func (m *Manager) Close() {
	m.mu.Lock()
	tasks := make([]*managedTask, 0, len(m.tasks))
	for _, entry := range m.tasks {
		tasks = append(tasks, entry)
	}
	m.tasks = map[string]*managedTask{}
	m.pathToID = map[string]string{}
	m.metrics.tasksLoaded.Set(0)
	m.mu.Unlock()
	for _, entry := range tasks {
		entry.stop()
	}
}

func (m *Manager) getTask(id string) (*managedTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return entry, nil
}

func (m *Manager) newManagedTask(spec config.TaskSpec) (*managedTask, error) {
	driver, ok := m.drivers.Get(spec.Kind)
	if !ok {
		return nil, fmt.Errorf("driver %q not found", spec.Kind)
	}
	runner, err := driver.NewRunner(spec, m.deps)
	if err != nil {
		return nil, fmt.Errorf("build runner for %s: %w", spec.ID, err)
	}
	schedule, err := buildSchedule(spec)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(m.rootCtx)
	entry := &managedTask{
		manager:  m,
		ctx:      ctx,
		cancel:   cancel,
		spec:     spec,
		runner:   runner,
		schedule: schedule,
		cmdCh:    make(chan taskCommand),
		doneCh:   make(chan struct{}),
		state: store.TaskState{
			TaskID:     spec.ID,
			Name:       spec.Name,
			Kind:       spec.Kind,
			Enabled:    spec.Enabled,
			Status:     initialStatus(spec.Enabled, schedule != nil),
			Labels:     cloneLabels(spec.Labels),
			SourcePath: spec.SourcePath,
		},
	}
	return entry, nil
}

func (m *Manager) markReloadFailure(ctx context.Context, path string, err error) {
	m.mu.RLock()
	id := m.pathToID[path]
	entry := m.tasks[id]
	m.mu.RUnlock()
	if entry != nil {
		entry.setReloadError(ctx, err.Error())
	}
	_ = m.store.InsertReloadFailure(ctx, id, path, err.Error())
}

type taskCommand struct {
	kind    string
	trigger task.TriggerType
	enabled bool
	resp    chan taskResponse
}

type taskResponse struct {
	record store.RunRecord
	err    error
}

type schedule interface {
	Next(time.Time) time.Time
}

type intervalSchedule struct {
	interval time.Duration
}

func (s intervalSchedule) Next(from time.Time) time.Time {
	return from.Add(s.interval)
}

type cronSchedule struct {
	schedule cron.Schedule
}

func (s cronSchedule) Next(from time.Time) time.Time {
	return s.schedule.Next(from)
}

func buildSchedule(spec config.TaskSpec) (schedule, error) {
	if spec.Interval.Duration > 0 {
		return intervalSchedule{interval: spec.Interval.Duration}, nil
	}
	if spec.Cron != "" {
		scheduled, err := cron.ParseStandard(spec.Cron)
		if err != nil {
			return nil, fmt.Errorf("parse cron for task %s: %w", spec.ID, err)
		}
		return cronSchedule{schedule: scheduled}, nil
	}
	return nil, nil
}

type managedTask struct {
	manager  *Manager
	ctx      context.Context
	cancel   context.CancelFunc
	spec     config.TaskSpec
	runner   task.Runner
	schedule schedule
	cmdCh    chan taskCommand
	doneCh   chan struct{}
	started  bool
	startMu  sync.Mutex
	stopOnce sync.Once

	stateMu sync.RWMutex
	state   store.TaskState
}

func (t *managedTask) start() {
	t.startMu.Lock()
	if t.started {
		t.startMu.Unlock()
		return
	}
	t.started = true
	t.startMu.Unlock()
	go t.loop()
	_ = t.manager.store.UpsertTaskState(context.Background(), t.snapshot())
}

func (t *managedTask) stop() {
	t.stopOnce.Do(func() {
		t.startMu.Lock()
		started := t.started
		t.startMu.Unlock()
		if !started {
			close(t.doneCh)
			return
		}
		t.cancel()
		select {
		case <-t.doneCh:
		case <-time.After(5 * time.Second):
		}
	})
}

func (t *managedTask) loop() {
	defer close(t.doneCh)
	enabled := t.spec.Enabled
	for {
		var timer *time.Timer
		if enabled && t.schedule != nil {
			next := t.schedule.Next(time.Now())
			t.updateState(context.Background(), func(state *store.TaskState) {
				state.Enabled = true
				state.Status = "running"
				state.NextRunAt = &next
			})
			timer = time.NewTimer(time.Until(next))
		} else {
			t.updateState(context.Background(), func(state *store.TaskState) {
				state.Enabled = enabled
				state.NextRunAt = nil
				if enabled {
					state.Status = "loaded"
				} else {
					state.Status = "disabled"
				}
			})
		}

		var timerCh <-chan time.Time
		if timer != nil {
			timerCh = timer.C
		}

		select {
		case <-t.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case cmd := <-t.cmdCh:
			if timer != nil {
				timer.Stop()
			}
			switch cmd.kind {
			case "run":
				record, err := t.execute(cmd.trigger)
				if cmd.resp != nil {
					cmd.resp <- taskResponse{record: record, err: err}
				}
			case "enable":
				enabled = cmd.enabled
				t.spec.Enabled = cmd.enabled
				t.updateState(context.Background(), func(state *store.TaskState) {
					state.Enabled = cmd.enabled
					if cmd.enabled {
						if t.schedule != nil {
							state.Status = "running"
						} else {
							state.Status = "loaded"
						}
					} else {
						state.Status = "disabled"
						state.NextRunAt = nil
					}
				})
				if cmd.resp != nil {
					cmd.resp <- taskResponse{}
				}
			}
		case <-timerCh:
			_, _ = t.execute(task.TriggerScheduled)
		}
	}
}

func (t *managedTask) run(ctx context.Context, trigger task.TriggerType) (store.RunRecord, error) {
	resp := make(chan taskResponse, 1)
	cmd := taskCommand{
		kind:    "run",
		trigger: trigger,
		resp:    resp,
	}
	select {
	case <-ctx.Done():
		return store.RunRecord{}, ctx.Err()
	case <-t.ctx.Done():
		return store.RunRecord{}, errors.New("task is stopping")
	case t.cmdCh <- cmd:
	}
	select {
	case <-ctx.Done():
		return store.RunRecord{}, ctx.Err()
	case result := <-resp:
		return result.record, result.err
	}
}

func (t *managedTask) setEnabled(ctx context.Context, enabled bool) error {
	resp := make(chan taskResponse, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case t.cmdCh <- taskCommand{kind: "enable", enabled: enabled, resp: resp}:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-resp:
		return result.err
	}
}

func (t *managedTask) execute(trigger task.TriggerType) (store.RunRecord, error) {
	runID := fmt.Sprintf("%s-%d", t.spec.ID, time.Now().UnixNano())
	startedAt := time.Now()
	runCtx := t.ctx
	cancel := func() {}
	if t.spec.Timeout.Duration > 0 {
		runCtx, cancel = context.WithTimeout(t.ctx, t.spec.Timeout.Duration)
	}
	defer cancel()

	result, err := t.runner.Run(runCtx, trigger)
	endedAt := time.Now()
	runStatus := "success"
	if err != nil {
		runStatus = "failed"
		if errors.Is(err, context.DeadlineExceeded) {
			runStatus = "timeout"
		}
	}
	checkStatus := result.CheckStatus
	if checkStatus == "" {
		if err != nil {
			checkStatus = "fail"
		} else {
			checkStatus = "pass"
		}
	}
	payload, payloadErr := json.Marshal(result.Payload)
	if payloadErr != nil {
		err = errors.Join(err, fmt.Errorf("marshal run payload: %w", payloadErr))
		runStatus = "failed"
	}
	record := store.RunRecord{
		RunID:        runID,
		TaskID:       t.spec.ID,
		TaskKind:     t.spec.Kind,
		TriggerType:  string(trigger),
		RunStatus:    runStatus,
		CheckStatus:  checkStatus,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		DurationMS:   endedAt.Sub(startedAt).Milliseconds(),
		ErrorMessage: errString(err),
		Summary:      cloneAnyMap(result.Summary),
		Payload:      payload,
		Findings:     cloneFindings(result.Findings),
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		Labels:       cloneLabels(t.spec.Labels),
	}

	t.updateState(context.Background(), func(state *store.TaskState) {
		state.LastRunAt = &endedAt
		state.LastRunStatus = runStatus
		state.LastCheckStatus = checkStatus
		state.LastError = record.ErrorMessage
		state.LastDurationMS = record.DurationMS
		state.Status = initialStatus(t.spec.Enabled, t.schedule != nil)
		if t.spec.Enabled && t.schedule != nil {
			state.Status = "running"
		}
		if seed, ok := result.Summary["sample_seed"]; ok {
			state.LastSampleSeed = int64Value(seed)
		}
		if count, ok := result.Summary["sample_count"]; ok {
			state.LastSampleCount = intValue(count)
		}
		if count, ok := result.Summary["mismatch_count"]; ok {
			state.LastMismatchCount = intValue(count)
		}
	})

	t.manager.metrics.taskRunsTotal.WithLabelValues(t.spec.ID, t.spec.Kind, runStatus, checkStatus).Inc()
	t.manager.metrics.taskLastDuration.WithLabelValues(t.spec.ID, t.spec.Kind).Set(float64(record.DurationMS))
	processed, traceErr := t.manager.tracer.Process(context.Background(), t.spec.Trace, record)
	if traceErr != nil {
		t.manager.logger.ErrorContext(context.Background(), "process trace record failed", "task_id", t.spec.ID, "err", traceErr)
	}
	t.manager.tracer.Dispatch(context.Background(), t.spec.Trace, processed)
	return processed, err
}

func (t *managedTask) snapshot() store.TaskState {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()
	snapshot := t.state
	snapshot.Labels = cloneLabels(t.state.Labels)
	return snapshot
}

func (t *managedTask) updateState(ctx context.Context, update func(state *store.TaskState)) {
	t.stateMu.Lock()
	update(&t.state)
	state := t.state
	state.Labels = cloneLabels(t.state.Labels)
	t.stateMu.Unlock()
	if err := t.manager.store.UpsertTaskState(ctx, state); err != nil {
		t.manager.logger.ErrorContext(ctx, "persist task state failed", "task_id", t.spec.ID, "err", err)
	}
}

func (t *managedTask) setReloadError(ctx context.Context, message string) {
	t.updateState(ctx, func(state *store.TaskState) {
		state.LastReloadError = message
		state.Status = "reload_failed"
	})
}

func (t *managedTask) clearReloadError(ctx context.Context) {
	t.updateState(ctx, func(state *store.TaskState) {
		state.LastReloadError = ""
		if state.Enabled {
			if t.schedule != nil {
				state.Status = "running"
			} else {
				state.Status = "loaded"
			}
		} else {
			state.Status = "disabled"
		}
	})
}

func initialStatus(enabled bool, hasSchedule bool) string {
	if !enabled {
		return "disabled"
	}
	if hasSchedule {
		return "running"
	}
	return "loaded"
}

func appendUniqueTask(list []*managedTask, entry *managedTask) []*managedTask {
	for _, item := range list {
		if item == entry {
			return list
		}
	}
	return append(list, entry)
}

func cloneLabels(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneFindings(input []store.Finding) []store.Finding {
	if len(input) == 0 {
		return nil
	}
	result := make([]store.Finding, 0, len(input))
	for _, finding := range input {
		cloned := finding
		cloned.Data = cloneAnyMap(finding.Data)
		result = append(result, cloned)
	}
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
