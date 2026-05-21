package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"pulseops/internal/ai"
	"pulseops/internal/api"
	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/runtime"
	"pulseops/internal/store"
	"pulseops/internal/task"
	"pulseops/internal/trace"
	"pulseops/internal/watch"
)

type App struct {
	config        config.Config
	logger        *slog.Logger
	store         *store.PostgresStore
	artifactStore store.ArtifactStore
	manager       *runtime.Manager
	watcher       *watch.Watcher
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
	traceManager := trace.NewManager(logger, artifactStore)
	hasPostgresSink := false
	for name, sinkCfg := range cfg.Trace.Sinks {
		switch sinkCfg.Kind {
		case "postgres":
			if sinkCfg.DSN != "" && sinkCfg.DSN != cfg.State.DSN {
				_ = stateStore.Close()
				return nil, fmt.Errorf("trace sink %s dsn must match state.dsn", name)
			}
			hasPostgresSink = true
			traceManager.Register(trace.NewPostgresSink(name, stateStore))
		case "webhook":
			traceManager.Register(trace.NewWebhookSink(name, sinkCfg.URL, sinkCfg.Timeout))
		default:
			_ = stateStore.Close()
			return nil, fmt.Errorf("unsupported trace sink kind %q", sinkCfg.Kind)
		}
	}
	if !hasPostgresSink {
		_ = stateStore.Close()
		return nil, fmt.Errorf("at least one postgres trace sink is required")
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
	if err := manager.LoadAll(ctx); err != nil {
		logger.ErrorContext(ctx, "load initial task configs failed", "err", err)
	}

	taskWatcher := watch.New(cfg.Task.ConfigDir, cfg.Task.ReloadDebounce.Duration, manager, logger)

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
		watcher:       taskWatcher,
		server:        server,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.watcher.Start(ctx); err != nil {
		return err
	}
	go func() {
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.ErrorContext(ctx, "http server stopped unexpectedly", "err", err)
		}
	}()
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = a.watcher.Close()
	a.manager.Close()
	if err := a.server.Shutdown(ctx); err != nil {
		return err
	}
	return a.store.Close()
}
