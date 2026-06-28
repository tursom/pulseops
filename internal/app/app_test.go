package app

import (
	"path/filepath"
	"testing"
	"time"

	"pulseops/internal/config"
)

func TestApplyPlatformConfigSummaryAppliesPluginConfig(t *testing.T) {
	baseDir := t.TempDir()
	cfg := config.Config{BaseDir: baseDir}
	cfg.Plugins.DefaultTimeout.Duration = 5 * time.Second
	cfg.Plugins.MaxOutputBytes = 64
	cfg.Plugins.MaxConcurrentCalls = 2

	applyPlatformConfigSummary(&cfg, config.PlatformConfigSummary{
		Plugins: config.PluginsConfigSummary{
			Enabled:             false,
			Dir:                 "custom-plugins",
			Strict:              true,
			AllowProcess:        false,
			AllowHTTP:           true,
			AllowGRPC:           false,
			DefaultTimeout:      "45s",
			MaxOutputBytes:      2048,
			MaxConcurrentCalls:  7,
			GenerationRetention: "20m",
			AllowedPermissions:  []string{"runs:read", "network:outbound"},
			EnvAllowlist:        []string{"HTTP_PROXY", "NO_PROXY"},
			Status:              "active",
		},
	})

	if cfg.Plugins.IsEnabled() {
		t.Fatalf("expected plugins disabled")
	}
	if cfg.Plugins.Dir != filepath.Join(baseDir, "custom-plugins") {
		t.Fatalf("unexpected plugin dir: %s", cfg.Plugins.Dir)
	}
	if !cfg.Plugins.Strict {
		t.Fatalf("expected strict mode")
	}
	if cfg.Plugins.ProcessAllowed() {
		t.Fatalf("expected process runtime disabled")
	}
	if !cfg.Plugins.HTTPAllowed() {
		t.Fatalf("expected http runtime enabled")
	}
	if cfg.Plugins.GRPCAllowed() {
		t.Fatalf("expected grpc runtime disabled")
	}
	if cfg.Plugins.DefaultTimeout.Duration != 45*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.Plugins.DefaultTimeout.Duration)
	}
	if cfg.Plugins.MaxOutputBytes != 2048 {
		t.Fatalf("unexpected max output bytes: %d", cfg.Plugins.MaxOutputBytes)
	}
	if cfg.Plugins.MaxConcurrentCalls != 7 {
		t.Fatalf("unexpected max concurrent calls: %d", cfg.Plugins.MaxConcurrentCalls)
	}
	if cfg.Plugins.GenerationRetention.Duration != 20*time.Minute {
		t.Fatalf("unexpected generation retention: %s", cfg.Plugins.GenerationRetention.Duration)
	}
	if !cfg.Plugins.PermissionAllowed("network:outbound") || cfg.Plugins.PermissionAllowed("tasks:write") {
		t.Fatalf("unexpected allowed permissions: %#v", cfg.Plugins.AllowedPermissions)
	}
	if len(cfg.Plugins.EnvAllowlist) != 2 || cfg.Plugins.EnvAllowlist[0] != "HTTP_PROXY" || cfg.Plugins.EnvAllowlist[1] != "NO_PROXY" {
		t.Fatalf("unexpected env allowlist: %#v", cfg.Plugins.EnvAllowlist)
	}
}
