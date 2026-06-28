package ai

import (
	"context"

	"pulseops/internal/datasource"
)

type pluginDataSourceAdapter struct {
	source *datasource.PluginSource
}

func (s *pluginDataSourceAdapter) Name() string {
	return s.source.Name()
}

func (s *pluginDataSourceAdapter) Validate(spec DataSourceSpec) error {
	return s.source.ValidateSpec(datasource.Spec{
		Type:    spec.Type,
		Config:  spec.Config,
		Alias:   spec.Alias,
		OnError: spec.OnError,
	})
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
