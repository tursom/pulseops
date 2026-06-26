package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"pulseops/internal/ai"
	"pulseops/internal/api"
	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/runtime"
	"pulseops/internal/store"
	"pulseops/internal/task"
	"pulseops/internal/trace"
)

type App struct {
	config        config.Config
	platform      config.PlatformConfigSummary
	logger        *slog.Logger
	store         *store.PostgresStore
	artifactStore store.ArtifactStore
	manager       *runtime.Manager
	server        *http.Server
}

func New(ctx context.Context, baseDir, configPath, staticDir string, logger *slog.Logger) (*App, error) {
	cfg, err := config.LoadGlobal(baseDir, configPath)
	if err != nil {
		return nil, err
	}
	stateStore, err := store.OpenPostgres(cfg.State.DSN)
	if err != nil {
		return nil, err
	}
	var savedPlatform *config.PlatformConfigSummary
	if loaded, err := stateStore.LoadPlatformConfig(ctx); err == nil {
		savedPlatform = &loaded
		applyPlatformConfigSummary(&cfg, loaded)
	} else if err != nil && !errors.Is(err, store.ErrMetaNotFound) {
		logger.WarnContext(ctx, "load platform config override failed", "err", err)
	}
	platform := buildPlatformSummary(cfg)
	var artifactStore store.ArtifactStore
	artifactStore, err = store.NewMinIOArtifactStore(cfg.ArtifactStore)
	if err != nil {
		logger.WarnContext(ctx, "artifact store initialization failed, continuing in degraded mode", "err", err)
		platform.Mode = "degraded"
		platform.Applied = false
		platform.ArtifactStore.Status = "config_error"
		platform.ArtifactStore.Error = err.Error()
		platform.Warnings = append(platform.Warnings, "artifact store: "+err.Error())
		artifactStore = store.DisabledArtifactStore{Reason: err.Error()}
	}
	traceManager := trace.NewManager(logger, artifactStore, 4096)
	// Load settings from DB
	settings, err := stateStore.LoadGlobalSettings(ctx)
	if err != nil {
		logger.WarnContext(ctx, "load global settings failed, using defaults", "err", err)
		settings = config.GlobalSettings{
			Sinks:           []config.SinkEntry{{Name: "postgres_main", Kind: "postgres"}},
			MaxPayloadBytes: 4096,
		}
	}
	if settings.MaxPayloadBytes > 0 {
		traceManager.SetMaxPayloadBytes(settings.MaxPayloadBytes)
	}
	// Seed from TOML if DB has no sinks (first run)
	if len(settings.Sinks) == 0 && len(cfg.Trace.Sinks) > 0 {
		for name, sinkCfg := range cfg.Trace.Sinks {
			entry := config.SinkEntry{Name: name, Kind: sinkCfg.Kind}
			if sinkCfg.Kind == "webhook" {
				entry.URL = sinkCfg.URL
				entry.Timeout = sinkCfg.Timeout.String()
			}
			settings.Sinks = append(settings.Sinks, entry)
		}
		// Persist seeded settings to DB
		if err := stateStore.SaveGlobalSettings(ctx, settings); err != nil {
			logger.WarnContext(ctx, "save seeded global settings failed", "err", err)
		}
	}
	// If still no sinks (neither DB nor TOML), create default postgres sink
	if len(settings.Sinks) == 0 {
		settings.Sinks = []config.SinkEntry{{Name: "postgres_main", Kind: "postgres"}}
		if err := stateStore.SaveGlobalSettings(ctx, settings); err != nil {
			logger.WarnContext(ctx, "save default global settings failed", "err", err)
		}
	}
	// Register sinks and enforce postgres requirement
	hasPostgresSink := false
	for _, entry := range settings.Sinks {
		switch entry.Kind {
		case "postgres":
			hasPostgresSink = true
			traceManager.Register(trace.NewPostgresSink(entry.Name, stateStore))
		case "webhook":
			timeout := 3 * time.Second
			if entry.Timeout != "" {
				if d, err := time.ParseDuration(entry.Timeout); err == nil {
					timeout = d
				}
			}
			traceManager.Register(trace.NewWebhookSink(entry.Name, entry.URL, timeout))
		}
	}
	if !hasPostgresSink {
		_ = stateStore.Close()
		return nil, fmt.Errorf("at least one postgres trace sink is required — configure in settings page")
	}

	evaluators := evaluator.NewRegistry()
	if err := evaluators.Register(evaluator.SteamGamePriceConsistency{}); err != nil {
		return nil, err
	}
	drivers := task.NewRegistry()
	driverList := []task.Driver{
		task.HTTPCheckDriver{},
		task.TCPCheckDriver{},
		task.ScriptExecDriver{},
		task.ProcessCheckDriver{},
		task.ScenarioCheckDriver{},
		task.NewUpstreamDataDriver(stateStore, artifactStore, logger),
	}
	if cfg.AI.Enabled {
		aiClient := ai.NewClient(ai.ClientConfig{
			Endpoint:    cfg.AI.Endpoint,
			APIKey:      cfg.AI.APIKey,
			Model:       cfg.AI.Model,
			Timeout:     cfg.AI.DefaultTimeout.Duration,
			MaxTokens:   cfg.AI.MaxTokens,
			Temperature: cfg.AI.Temperature,
		})
		aiDriver := ai.NewDriver(aiClient, stateStore, logger)
		if cfg.AI.PluginDir != "" {
			if err := aiDriver.LoadPlugins(cfg.AI.PluginDir, logger); err != nil {
				logger.WarnContext(ctx, "load AI plugins failed, continuing without plugins", "err", err)
				platform.Mode = "degraded"
				platform.Applied = false
				platform.AI.Status = "config_error"
				platform.AI.Error = err.Error()
				platform.Warnings = append(platform.Warnings, "ai plugins: "+err.Error())
			}
		}
		driverList = append(driverList, aiDriver)
		if err := evaluators.Register(&ai.AIEvaluator{Client: aiClient}); err != nil {
			return nil, err
		}
	}
	for _, driver := range driverList {
		if err := drivers.Register(driver); err != nil {
			return nil, err
		}
	}

	httpClient := &http.Client{Timeout: cfg.Task.DefaultTimeout.Duration}
	manager := runtime.NewManager(ctx, cfg, logger, drivers, task.RunnerDeps{
		BaseDir:    baseDir,
		HTTPClient: httpClient,
		Evaluators: evaluators,
	}, stateStore, traceManager)
	if err := manager.LoadAllFromDB(ctx); err != nil {
		logger.ErrorContext(ctx, "load task definitions from db failed", "err", err)
	}

	if savedPlatform != nil {
		platform.Applied = true
		if len(savedPlatform.Warnings) > 0 {
			platform.Warnings = append(platform.Warnings, savedPlatform.Warnings...)
		}
	}

	handler := api.Routes(staticDir, manager, stateStore, artifactStore, traceManager, platform, logger)
	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout.Duration,
		WriteTimeout: cfg.Server.WriteTimeout.Duration,
		BaseContext: func(_ net.Listener) context.Context {
			return context.WithValue(context.Background(), "logger", logger)
		},
	}
	return &App{
		config:        cfg,
		platform:      platform,
		logger:        logger,
		store:         stateStore,
		artifactStore: artifactStore,
		manager:       manager,
		server:        server,
	}, nil
}

func applyPlatformConfigSummary(cfg *config.Config, summary config.PlatformConfigSummary) {
	if summary.ArtifactStore.Kind != "" {
		cfg.ArtifactStore.Kind = summary.ArtifactStore.Kind
	}
	if summary.ArtifactStore.Provider != "" {
		cfg.ArtifactStore.Provider = summary.ArtifactStore.Provider
	}
	if summary.ArtifactStore.Bucket != "" {
		cfg.ArtifactStore.Bucket = summary.ArtifactStore.Bucket
	}
	if summary.ArtifactStore.Endpoint != "" {
		cfg.ArtifactStore.Endpoint = summary.ArtifactStore.Endpoint
	}
	if summary.ArtifactStore.Region != "" {
		cfg.ArtifactStore.Region = summary.ArtifactStore.Region
	}
	cfg.ArtifactStore.BasePath = summary.ArtifactStore.BasePath
	cfg.ArtifactStore.ForcePathStyle = summary.ArtifactStore.ForcePathStyle
	cfg.ArtifactStore.UseSSL = summary.ArtifactStore.UseSSL
	if summary.ArtifactStore.AccessKey != "" {
		cfg.ArtifactStore.AccessKey = summary.ArtifactStore.AccessKey
	}
	if summary.ArtifactStore.SecretKey != "" {
		cfg.ArtifactStore.SecretKey = summary.ArtifactStore.SecretKey
	}
	if summary.ArtifactStore.PresignTTL != "" {
		_ = cfg.ArtifactStore.PresignTTL.UnmarshalText([]byte(summary.ArtifactStore.PresignTTL))
	}
	cfg.AI.Enabled = summary.AI.Enabled
	if summary.AI.Endpoint != "" {
		cfg.AI.Endpoint = summary.AI.Endpoint
	}
	if summary.AI.Model != "" {
		cfg.AI.Model = summary.AI.Model
	}
	if summary.AI.Timeout != "" {
		_ = cfg.AI.DefaultTimeout.UnmarshalText([]byte(summary.AI.Timeout))
	}
	if summary.AI.MaxTokens > 0 {
		cfg.AI.MaxTokens = summary.AI.MaxTokens
	}
	cfg.AI.Temperature = summary.AI.Temperature
	if summary.AI.PluginDir != "" {
		cfg.AI.PluginDir = config.ResolvePath(cfg.BaseDir, summary.AI.PluginDir)
	}
}

func buildPlatformSummary(cfg config.Config) config.PlatformConfigSummary {
	mode := "active"
	return config.PlatformConfigSummary{
		Mode:     mode,
		Applied:  true,
		Warnings: []string{},
		Server: config.ServerConfigSummary{
			Addr: cfg.Server.Addr,
		},
		Task: config.TaskConfigSummary{
			ConfigDir: cfg.Task.ConfigDir,
		},
		State: config.StateConfigSummary{
			Backend: cfg.State.Backend,
		},
		ArtifactStore: config.ArtifactConfigSummary{
			Kind:           cfg.ArtifactStore.Kind,
			Provider:       cfg.ArtifactStore.Provider,
			Bucket:         cfg.ArtifactStore.Bucket,
			Endpoint:       cfg.ArtifactStore.Endpoint,
			Region:         cfg.ArtifactStore.Region,
			BasePath:       cfg.ArtifactStore.BasePath,
			PresignTTL:     cfg.ArtifactStore.PresignTTL.String(),
			ForcePathStyle: cfg.ArtifactStore.ForcePathStyle,
			UseSSL:         cfg.ArtifactStore.UseSSL,
			Status:         "active",
		},
		AI: config.AIConfigSummary{
			Enabled:     cfg.AI.Enabled,
			Endpoint:    cfg.AI.Endpoint,
			Model:       cfg.AI.Model,
			Timeout:     cfg.AI.DefaultTimeout.String(),
			MaxTokens:   cfg.AI.MaxTokens,
			Temperature: cfg.AI.Temperature,
			PluginDir:   cfg.AI.PluginDir,
			Status:      "active",
		},
	}
}

func (a *App) Start(ctx context.Context) error {
	go func() {
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.ErrorContext(ctx, "http server stopped unexpectedly", "err", err)
		}
	}()
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.manager.Close()
	if err := a.server.Shutdown(ctx); err != nil {
		return err
	}
	return a.store.Close()
}
