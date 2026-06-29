package ai

import (
	"context"
	"fmt"

	"pulseops/internal/datasource"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

type pluginDataSourceAdapter struct {
	source        *datasource.PluginSource
	repo          store.Repository
	artifactStore store.ArtifactStore
}

type pluginDataSourceTrace struct {
	ConfigVersions map[string]any
	AssetVersions  map[string]any
	TaskOverrides  map[string]any
}

func newPluginDataSourceTrace() pluginDataSourceTrace {
	return pluginDataSourceTrace{
		ConfigVersions: map[string]any{},
		AssetVersions:  map[string]any{},
		TaskOverrides:  map[string]any{},
	}
}

func (s *pluginDataSourceAdapter) Name() string {
	return s.source.Name()
}

func (s *pluginDataSourceAdapter) Validate(spec DataSourceSpec) error {
	resolved, _, cleanup, err := s.ResolveSpec(context.Background(), spec)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	return s.source.ValidateSpec(datasource.Spec{
		Type:    resolved.Type,
		Config:  resolved.Config,
		Alias:   resolved.Alias,
		OnError: resolved.OnError,
	})
}

func (s *pluginDataSourceAdapter) ResolveSpec(ctx context.Context, spec DataSourceSpec) (DataSourceSpec, pluginDataSourceTrace, func(), error) {
	trace := newPluginDataSourceTrace()
	capability := s.source.Capability()
	out := configSchemaDefaults(capability.Config)
	configInstanceIDs := make([]string, 0, 2)
	reader, _ := s.repo.(pluginconfig.ConfigReader)
	if spec.PluginConfigRef != "" || spec.CapabilityConfigRef != "" {
		if reader == nil {
			return spec, trace, nil, fmt.Errorf("plugin config store is unavailable")
		}
	}
	if spec.PluginConfigRef != "" {
		if capability.Config == nil || !capability.Config.AllowPluginConfigRef {
			return spec, trace, nil, fmt.Errorf("capability %q does not allow plugin_config_ref", capability.ID)
		}
		values, version, err := pluginconfig.ActiveConfigValues(ctx, reader, spec.PluginConfigRef, capability, "plugin")
		if err != nil {
			return spec, trace, nil, err
		}
		trace.ConfigVersions[spec.PluginConfigRef] = version
		configInstanceIDs = append(configInstanceIDs, spec.PluginConfigRef)
		mergeAnyMap(out, values)
	}
	if spec.CapabilityConfigRef != "" {
		values, version, err := pluginconfig.ActiveConfigValues(ctx, reader, spec.CapabilityConfigRef, capability, "capability")
		if err != nil {
			return spec, trace, nil, err
		}
		trace.ConfigVersions[spec.CapabilityConfigRef] = version
		configInstanceIDs = append(configInstanceIDs, spec.CapabilityConfigRef)
		mergeAnyMap(out, values)
	}
	mergeAnyMap(out, spec.Config)
	if err := validatePluginDataSourceOverrides(capability, spec.Overrides); err != nil {
		return spec, trace, nil, err
	}
	if len(spec.Overrides) > 0 {
		key := spec.Alias
		if key == "" {
			key = spec.Type
		}
		trace.TaskOverrides[key] = spec.Overrides
	}
	mergeAnyMap(out, spec.Overrides)
	runtimeStore, _ := s.repo.(pluginconfig.RuntimeStore)
	resolved, assetVersions, _, cleanup, err := pluginconfig.ResolveRuntimeValuesWithOptions(ctx, runtimeStore, s.artifactStore, capability.Config, capability.ConfigClasses, out, pluginconfig.RuntimeResolveOptions{
		PluginID:          capability.PluginID,
		CapabilityID:      capability.ID,
		ConfigInstanceIDs: configInstanceIDs,
	})
	mergeAnyMap(trace.AssetVersions, assetVersions)
	if err != nil {
		return spec, trace, cleanup, err
	}
	spec.Config = resolved
	return spec, trace, cleanup, nil
}

func (s *pluginDataSourceAdapter) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	return s.source.Fetch(ctx, datasource.Spec{
		Type:    spec.Type,
		Config:  spec.Config,
		Alias:   spec.Alias,
		OnError: spec.OnError,
	}, datasource.FetchDeps{
		HTTPClient:    deps.HTTPClient,
		CurrentRunID:  deps.CurrentRunID,
		CurrentTaskID: deps.CurrentTaskID,
		TriggerType:   deps.TriggerType,
	})
}

func configSchemaDefaults(schema *pluginmodel.ConfigSchema) map[string]any {
	out := map[string]any{}
	if schema == nil {
		return out
	}
	for key, field := range schema.Fields {
		if field.Default != nil {
			out[key] = field.Default
		}
	}
	return out
}

func mergeAnyMap(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func validatePluginDataSourceOverrides(capability pluginmodel.Capability, overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}
	if capability.Config == nil {
		return fmt.Errorf("capability %q does not declare config schema", capability.ID)
	}
	if err := pluginconfig.ValidateValues(capability.Config, capability.ConfigClasses, overrides, pluginconfig.ValidationOptions{Overrides: true}); err != nil {
		return err
	}
	return nil
}

type manifestCABIDataSource struct {
	name   string
	source *cSource
}

func (s *manifestCABIDataSource) Name() string {
	return s.name
}

func (s *manifestCABIDataSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	return s.source.Fetch(ctx, spec, deps)
}
