package app

import (
	"context"
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
	artifactStore, err := store.NewMinIOArtifactStore(cfg.ArtifactStore)
	if err != nil {
		_ = stateStore.Close()
		return nil, err
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
				return nil, fmt.Errorf("load AI plugins: %w", err)
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

	handler := api.Routes(staticDir, manager, stateStore, artifactStore, logger)
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
		logger:        logger,
		store:         stateStore,
		artifactStore: artifactStore,
		manager:       manager,
		server:        server,
	}, nil
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
