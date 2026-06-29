package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/datasource"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

type Driver struct {
	client        *Client
	store         store.Repository
	artifactStore store.ArtifactStore
	sourcesMu     sync.RWMutex
	sources       *DataSourceRegistry
	legacySources map[string]DataSource
	pluginSources map[string]DataSource
	writersMu     sync.RWMutex
	writers       *OutputWriterRegistry
	pluginWriters map[string]OutputWriter
	logger        *slog.Logger
}

func NewDriver(client *Client, repo store.Repository, logger *slog.Logger, artifactStores ...store.ArtifactStore) *Driver {
	var artifactStore store.ArtifactStore
	if len(artifactStores) > 0 {
		artifactStore = artifactStores[0]
	}
	return &Driver{
		client:        client,
		store:         repo,
		artifactStore: artifactStore,
		sources:       NewDataSourceRegistry(),
		legacySources: map[string]DataSource{},
		pluginSources: map[string]DataSource{},
		writers:       NewOutputWriterRegistry(),
		pluginWriters: map[string]OutputWriter{},
		logger:        logger,
	}
}

func (d *Driver) LoadPlugins(pluginDir string, _ *slog.Logger) error {
	return fmt.Errorf("legacy AI C ABI plugin loading from %q is not supported; use the plugin catalog", pluginDir)
}

func (d *Driver) SyncPluginDataSources(caps []pluginmodel.Capability, cfg config.PluginsConfig) {
	d.SyncPluginCapabilities(caps, cfg)
}

func (d *Driver) SyncPluginCapabilities(caps []pluginmodel.Capability, cfg config.PluginsConfig) {
	pluginSources := map[string]DataSource{}
	pluginWriters := map[string]OutputWriter{}
	for _, cap := range caps {
		switch cap.Type {
		case pluginmodel.CapabilityDataSource:
			pluginSources[cap.Name] = &pluginDataSourceAdapter{source: datasource.NewPluginSource(cap, cfg), repo: d.store, artifactStore: d.artifactStore}
		case pluginmodel.CapabilityAIDataSource:
			if cap.Runtime == "process" || cap.Runtime == "http" || cap.Runtime == "http_plugin" {
				pluginSources[cap.Name] = &pluginDataSourceAdapter{source: datasource.NewPluginSource(cap, cfg), repo: d.store, artifactStore: d.artifactStore}
			}
		case pluginmodel.CapabilityOutputWriter:
			if cap.Runtime == "process" || cap.Runtime == "http" || cap.Runtime == "http_plugin" {
				reader, _ := d.store.(pluginconfig.ConfigReader)
				runtimeStore, _ := d.store.(pluginconfig.RuntimeStore)
				pluginWriters[cap.Name] = &pluginOutputWriter{cap: cap, cfg: cfg, configReader: reader, runtimeStore: runtimeStore, artifactStore: d.artifactStore}
			}
		}
	}
	d.sourcesMu.Lock()
	d.pluginSources = pluginSources
	d.rebuildSourcesLocked()
	d.sourcesMu.Unlock()
	d.writersMu.Lock()
	d.pluginWriters = pluginWriters
	d.rebuildWritersLocked()
	d.writersMu.Unlock()
}

func (d *Driver) loadManifestCABIDataSource(cap pluginmodel.Capability) (DataSource, error) {
	if cap.ReleasePath == "" {
		return nil, fmt.Errorf("c_abi data source %q requires a release path", cap.Name)
	}
	if cap.Entrypoint == "" {
		return nil, fmt.Errorf("c_abi data source %q requires an entrypoint", cap.Name)
	}
	if filepath.IsAbs(cap.Entrypoint) {
		return nil, fmt.Errorf("c_abi data source %q entrypoint must be relative to the plugin release path", cap.Name)
	}
	absRelease, err := filepath.Abs(cap.ReleasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve C ABI release path: %w", err)
	}
	absEntrypoint, err := filepath.Abs(filepath.Join(absRelease, cap.Entrypoint))
	if err != nil {
		return nil, fmt.Errorf("resolve C ABI entrypoint: %w", err)
	}
	rel, err := filepath.Rel(absRelease, absEntrypoint)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("c_abi data source %q entrypoint must stay inside the plugin release path", cap.Name)
	}
	info, err := os.Stat(absEntrypoint)
	if err != nil {
		return nil, fmt.Errorf("stat C ABI entrypoint: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("c_abi data source %q entrypoint is a directory", cap.Name)
	}
	source, err := NewPluginManager(absRelease, d.logger).loadOne(absEntrypoint)
	if err != nil {
		return nil, err
	}
	return &manifestCABIDataSource{name: cap.Name, source: source}, nil
}

func (d *Driver) Kind() string { return "ai_analyze" }

func (d *Driver) Validate(spec config.TaskSpec) error {
	params, err := d.decodeParams(spec.Params)
	if err != nil {
		return fmt.Errorf("decode ai_analyze params: %w", err)
	}
	if len(params.DataSources) == 0 {
		return fmt.Errorf("ai_analyze requires at least one data source")
	}
	if params.Prompt.Text == "" {
		return fmt.Errorf("ai_analyze requires prompt.text")
	}
	registry := d.sourceRegistry()
	for _, dsSpec := range params.DataSources {
		source, ok := registry.Get(dsSpec.Type)
		if !ok {
			return fmt.Errorf("unknown data source type %q", dsSpec.Type)
		}
		if validator, ok := source.(interface{ Validate(DataSourceSpec) error }); ok {
			if err := validator.Validate(dsSpec); err != nil {
				return fmt.Errorf("validate data source %q: %w", dsSpec.Type, err)
			}
		}
	}
	builtins := map[string]bool{
		"run_context":       true,
		"run_history":       true,
		"previous_analysis": true,
		"http_call":         true,
	}
	for _, dsSpec := range params.DataSources {
		if dsSpec.Alias != "" && builtins[dsSpec.Alias] {
			return fmt.Errorf("alias %q conflicts with built-in data source type", dsSpec.Alias)
		}
	}
	writers := d.writerRegistry()
	for _, outSpec := range params.Outputs {
		writer, ok := writers.Get(outSpec.Type)
		if !ok {
			return fmt.Errorf("unknown output type %q", outSpec.Type)
		}
		if validator, ok := writer.(interface{ Validate(OutputSpec) error }); ok {
			if err := validator.Validate(outSpec); err != nil {
				return fmt.Errorf("validate output %q: %w", outSpec.Type, err)
			}
		}
	}
	return nil
}

func (d *Driver) NewRunner(spec config.TaskSpec, deps task.RunnerDeps) (task.Runner, error) {
	params, err := d.decodeParams(spec.Params)
	if err != nil {
		return nil, fmt.Errorf("decode ai_analyze params: %w", err)
	}
	return &runner{
		spec:    spec,
		params:  params,
		client:  d.client,
		store:   d.store,
		deps:    deps,
		sources: d.sourceRegistry(),
		writers: d.writerRegistry(),
		logger:  d.logger,
	}, nil
}

func (d *Driver) sourceRegistry() *DataSourceRegistry {
	d.sourcesMu.RLock()
	defer d.sourcesMu.RUnlock()
	if d.sources == nil {
		return NewDataSourceRegistry()
	}
	return d.sources
}

func (d *Driver) rebuildSourcesLocked() {
	registry := NewDataSourceRegistry()
	for name, source := range d.legacySources {
		registry.Register(name, source)
	}
	for name, source := range d.pluginSources {
		registry.Register(name, source)
	}
	d.sources = registry
}

func (d *Driver) writerRegistry() *OutputWriterRegistry {
	d.writersMu.RLock()
	defer d.writersMu.RUnlock()
	if d.writers == nil {
		return NewOutputWriterRegistry()
	}
	return d.writers
}

func (d *Driver) rebuildWritersLocked() {
	registry := NewOutputWriterRegistry()
	for name, writer := range d.pluginWriters {
		registry.Register(name, writer)
	}
	d.writers = registry
}

func (d *Driver) decodeParams(raw map[string]any) (AIAnalyzeParams, error) {
	params, err := task.DecodeParams[AIAnalyzeParams](raw)
	if err != nil {
		return AIAnalyzeParams{}, err
	}
	if params.AnalysisType == "" {
		params.AnalysisType = AnalysisDiagnose
	}
	return params, nil
}

type runner struct {
	spec    config.TaskSpec
	params  AIAnalyzeParams
	client  *Client
	store   store.Repository
	deps    task.RunnerDeps
	sources *DataSourceRegistry
	writers *OutputWriterRegistry
	logger  *slog.Logger
}

func (r *runner) Run(ctx context.Context, trigger task.TriggerType) (task.Result, error) {
	startedAt := time.Now()

	promptText, pluginTrace, err := r.renderPrompt(ctx, trigger)
	if err != nil {
		return task.Result{CheckStatus: "fail"}, fmt.Errorf("render prompt: %w", err)
	}

	resp, err := r.client.Chat(ctx, promptText)
	durationMS := time.Since(startedAt).Milliseconds()

	runID := runnerRunID(ctx, r.spec.ID)
	record := store.AIAnalysisRecord{
		RunID:        runID,
		TaskID:       r.spec.ID,
		AnalysisType: r.params.AnalysisType,
		Model:        r.client.model,
		Prompt:       promptText,
		DurationMS:   durationMS,
	}

	if err != nil {
		record.Status = "error"
		record.ErrorMessage = err.Error()
		if insErr := r.store.InsertAIAnalysis(ctx, record); insErr != nil {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("ai analysis error: %s, insert: %w", err, insErr)
		}
		return task.Result{
			CheckStatus:          "fail",
			Summary:              map[string]any{"ai_error": err.Error()},
			PluginConfigVersions: pluginTrace.ConfigVersions,
			PluginAssetVersions:  pluginTrace.AssetVersions,
			PluginTaskOverrides:  pluginTrace.TaskOverrides,
		}, nil
	}

	record.Status = "success"
	record.Response = resp.Content
	record.TokensIn = resp.TokensIn
	record.TokensOut = resp.TokensOut

	parsedJSON := tryParseJSON(resp.Content)

	outputDeps := OutputDeps{
		DBRepository:  r.store,
		HTTPClient:    r.deps.HTTPClient,
		CurrentRunID:  runID,
		CurrentTaskID: r.spec.ID,
	}
	outputInput := OutputInput{
		RawResponse: resp.Content,
		ParsedJSON:  parsedJSON,
		RunID:       runID,
		TaskID:      r.spec.ID,
		TokensIn:    resp.TokensIn,
		TokensOut:   resp.TokensOut,
		DurationMS:  durationMS,
	}

	if err := r.store.InsertAIAnalysis(ctx, record); err != nil {
		return task.Result{CheckStatus: "fail"}, fmt.Errorf("insert ai analysis: %w", err)
	}

	var outputFindings []store.Finding
	outputSummary := map[string]any{}
	for _, outSpec := range r.params.Outputs {
		writer, ok := r.writers.Get(outSpec.Type)
		if !ok {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("unknown output type %q", outSpec.Type)
		}
		result, err := writer.Write(ctx, outSpec, outputDeps, outputInput)
		if err != nil {
			return task.Result{CheckStatus: "fail"}, fmt.Errorf("write output %q: %w", outSpec.Type, err)
		}
		if result != nil {
			outputFindings = append(outputFindings, result.Findings...)
			for k, v := range result.Summary {
				outputSummary[k] = v
			}
			mergeAnyMap(pluginTrace.ConfigVersions, result.PluginConfigVersions)
			mergeAnyMap(pluginTrace.AssetVersions, result.PluginAssetVersions)
			mergeAnyMap(pluginTrace.TaskOverrides, result.PluginTaskOverrides)
		}
	}

	summary := map[string]any{
		"ai_analysis_id": runID,
		"ai_tokens_in":   resp.TokensIn,
		"ai_tokens_out":  resp.TokensOut,
		"ai_duration_ms": durationMS,
	}
	for k, v := range outputSummary {
		summary[k] = v
	}

	return task.Result{
		CheckStatus:          "pass",
		Summary:              summary,
		Findings:             outputFindings,
		Payload:              resp.Content,
		PluginConfigVersions: pluginTrace.ConfigVersions,
		PluginAssetVersions:  pluginTrace.AssetVersions,
		PluginTaskOverrides:  pluginTrace.TaskOverrides,
	}, nil
}

func (r *runner) renderPrompt(ctx context.Context, trigger task.TriggerType) (string, pluginDataSourceTrace, error) {
	dataSources, pluginTrace, err := r.fetchDataSources(ctx, trigger)
	if err != nil {
		return "", pluginTrace, fmt.Errorf("fetch data sources: %w", err)
	}
	rendered, err := renderTemplate(r.params.Prompt, dataSources)
	return rendered, pluginTrace, err
}

func (r *runner) fetchDataSources(ctx context.Context, trigger task.TriggerType) (map[string]any, pluginDataSourceTrace, error) {
	registry := r.sources
	pluginTrace := newPluginDataSourceTrace()
	deps := FetchDeps{
		DBRepository:  r.store,
		HTTPClient:    r.deps.HTTPClient,
		CurrentRunID:  runnerRunID(ctx, r.spec.ID),
		CurrentTaskID: r.spec.ID,
		TriggerType:   string(trigger),
	}
	if tr, ok := ctx.Value(CtxTriggerRun).(*store.RunRecord); ok {
		deps.TriggerRun = tr
	}
	result := make(map[string]any, len(r.params.DataSources))
	for _, dsSpec := range r.params.DataSources {
		source, ok := registry.Get(dsSpec.Type)
		if !ok {
			return nil, pluginTrace, fmt.Errorf("unknown data source type %q", dsSpec.Type)
		}
		runSpec := dsSpec
		cleanup := func() {}
		if resolver, ok := source.(interface {
			ResolveSpec(context.Context, DataSourceSpec) (DataSourceSpec, pluginDataSourceTrace, func(), error)
		}); ok {
			resolved, trace, release, err := resolver.ResolveSpec(ctx, dsSpec)
			if err != nil {
				return nil, pluginTrace, fmt.Errorf("resolve data source %q config: %w", dsSpec.Type, err)
			}
			runSpec = resolved
			cleanup = release
			mergeAnyMap(pluginTrace.ConfigVersions, trace.ConfigVersions)
			mergeAnyMap(pluginTrace.AssetVersions, trace.AssetVersions)
			mergeAnyMap(pluginTrace.TaskOverrides, trace.TaskOverrides)
		}
		data, err := source.Fetch(ctx, runSpec, deps)
		cleanup()
		if err != nil {
			if dsSpec.OnError == "skip" {
				r.logger.WarnContext(ctx, "skipping failed data source",
					"type", dsSpec.Type,
					"alias", dsSpec.Alias,
					"err", err,
				)
				continue
			}
			return nil, pluginTrace, fmt.Errorf("fetch data source %q: %w", dsSpec.Type, err)
		}
		key := dsSpec.Alias
		if key == "" {
			key = dsSpec.Type
		}
		result[key] = data
	}
	return result, pluginTrace, nil
}

func tryParseJSON(content string) map[string]any {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "{") {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err == nil {
			return parsed
		}
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(line), &parsed); err == nil {
				return parsed
			}
		}
	}
	return nil
}
