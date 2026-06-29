package trace

import (
	"context"
	"net/http"

	"pulseops/internal/config"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

type PluginSink struct {
	cap           pluginmodel.Capability
	cfg           config.PluginsConfig
	httpClient    *http.Client
	configReader  pluginconfig.ConfigReader
	runtimeStore  pluginconfig.RuntimeStore
	artifactStore store.ArtifactStore
}

func NewPluginSink(cap pluginmodel.Capability, cfg config.PluginsConfig, httpClient *http.Client, configStore any, artifactStore store.ArtifactStore) *PluginSink {
	reader, _ := configStore.(pluginconfig.ConfigReader)
	runtimeStore, _ := configStore.(pluginconfig.RuntimeStore)
	return &PluginSink{cap: cap, cfg: cfg, httpClient: httpClient, configReader: reader, runtimeStore: runtimeStore, artifactStore: artifactStore}
}

func (s *PluginSink) Name() string {
	return s.cap.Name
}

func (s *PluginSink) Kind() string {
	return "plugin"
}

func (s *PluginSink) Write(ctx context.Context, record store.RunRecord) error {
	resolved, err := pluginconfig.ResolveCapabilityConfig(ctx, s.configReader, s.cap, pluginconfig.ResolveCapabilityOptions{
		RuntimeStore:  s.runtimeStore,
		ArtifactStore: s.artifactStore,
	})
	if resolved.Cleanup != nil {
		defer resolved.Cleanup()
	}
	if err != nil {
		return err
	}
	_, err = pluginruntime.NewClient(s.cap, s.cfg).Call(ctx, pluginruntime.Request{
		Action: "write_trace",
		Config: resolved.Config,
		Input: map[string]any{
			"record": record,
		},
	}, pluginruntime.Deps{
		HTTPClient:    s.httpClient,
		CurrentRunID:  record.RunID,
		CurrentTaskID: record.TaskID,
		TriggerType:   record.TriggerType,
	})
	return err
}
