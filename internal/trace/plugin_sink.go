package trace

import (
	"context"
	"net/http"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/store"
)

type PluginSink struct {
	cap        pluginmodel.Capability
	cfg        config.PluginsConfig
	httpClient *http.Client
}

func NewPluginSink(cap pluginmodel.Capability, cfg config.PluginsConfig, httpClient *http.Client) *PluginSink {
	return &PluginSink{cap: cap, cfg: cfg, httpClient: httpClient}
}

func (s *PluginSink) Name() string {
	return s.cap.Name
}

func (s *PluginSink) Kind() string {
	return "plugin"
}

func (s *PluginSink) Write(ctx context.Context, record store.RunRecord) error {
	_, err := pluginruntime.NewClient(s.cap, s.cfg).Call(ctx, pluginruntime.Request{
		Action: "write_trace",
		Config: clonePluginConfig(s.cap.Defaults),
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

func clonePluginConfig(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
