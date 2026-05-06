package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pulseops/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/pulseops.toml", "pulseops config file path")
	flag.Parse()

	baseDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, baseDir, *configPath, logger)
	if err != nil {
		logger.Error("create pulseops app failed", "err", err)
		os.Exit(1)
	}
	if err := application.Start(ctx); err != nil {
		logger.Error("start pulseops app failed", "err", err)
		os.Exit(1)
	}
	logger.Info("pulseops started", "config", *configPath)
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown pulseops failed", "err", err)
		os.Exit(1)
	}
	logger.Info("pulseops stopped")
}
