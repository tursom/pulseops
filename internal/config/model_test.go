package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigNormalizeSetsPostgresAndArtifactDefaults(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	cfg := Config{
		BaseDir: baseDir,
		Trace: TraceConfig{
			Sinks: map[string]SinkConfig{
				"postgres_main": {Kind: "postgres", DSN: "postgres://local"},
				"webhook_audit": {Kind: "webhook", URL: "http://audit.local"},
			},
		},
		ArtifactStore: ArtifactStoreConfig{
			Bucket:   "pulseops-artifacts",
			Endpoint: "https://r2.local",
		},
	}

	cfg.Normalize()

	if cfg.State.Backend != "postgres" {
		t.Fatalf("expected default postgres backend, got %q", cfg.State.Backend)
	}
	if cfg.Task.ConfigDir != filepath.Join(baseDir, "configs", "tasks") {
		t.Fatalf("expected resolved config dir, got %q", cfg.Task.ConfigDir)
	}
	if cfg.ArtifactStore.Kind != "s3" {
		t.Fatalf("expected default artifact kind s3, got %q", cfg.ArtifactStore.Kind)
	}
	if cfg.ArtifactStore.Provider != "minio" {
		t.Fatalf("expected default provider minio, got %q", cfg.ArtifactStore.Provider)
	}
	if cfg.ArtifactStore.Region != "auto" {
		t.Fatalf("expected default region auto, got %q", cfg.ArtifactStore.Region)
	}
	if cfg.ArtifactStore.PresignTTL.Duration != 15*time.Minute {
		t.Fatalf("expected default presign ttl 15m, got %s", cfg.ArtifactStore.PresignTTL.Duration)
	}
}

func TestConfigValidateRejectsLegacySQLiteAndMissingArtifactConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Task: TaskConfig{ConfigDir: "configs/tasks"},
		State: StateConfig{
			Backend: "sqlite",
			DSN:     "data/pulseops.db",
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected sqlite backend to be rejected")
	}

	cfg.State.Backend = "postgres"
	cfg.State.DSN = "postgres://pulseops:secret@127.0.0.1:5432/pulseops?sslmode=disable"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing artifact store config to fail validation")
	}
}

func TestTaskSpecNormalizeInjectsPostgresSink(t *testing.T) {
	t.Parallel()

	global := Config{
		Task: TaskConfig{
			DefaultTimeout:    Duration{Duration: 7 * time.Second},
			DefaultTraceLevel: "summary",
		},
		Trace: TraceConfig{
			Sinks: map[string]SinkConfig{
				"postgres_main": {Kind: "postgres"},
			},
		},
	}
	spec := TaskSpec{
		ID:   "task-a",
		Kind: "http_check",
		Trace: TracePolicy{
			Level: "detail",
		},
	}

	spec.Normalize(global)

	if spec.Timeout.Duration != 7*time.Second {
		t.Fatalf("expected inherited timeout, got %s", spec.Timeout.Duration)
	}
	if spec.Trace.Level != "detail" {
		t.Fatalf("expected trace level detail, got %q", spec.Trace.Level)
	}
}

func TestLoadTaskSpecComputesHashAndDefaults(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	taskPath := filepath.Join(baseDir, "task.toml")
	content := `
id = "http-a"
kind = "http_check"
enabled = true

[params]
url = "http://127.0.0.1/healthz"
`
	if err := os.WriteFile(taskPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}

	global := Config{
		BaseDir: baseDir,
		Task: TaskConfig{
			DefaultTimeout:    Duration{Duration: 3 * time.Second},
			DefaultTraceLevel: "summary",
		},
		ArtifactStore: ArtifactStoreConfig{
			Kind:     "s3",
			Bucket:   "pulseops-artifacts",
			Endpoint: "https://r2.local",
		},
		Trace: TraceConfig{
			Sinks: map[string]SinkConfig{
				"postgres_main": {Kind: "postgres"},
			},
		},
	}

	spec, err := LoadTaskSpec(global, taskPath)
	if err != nil {
		t.Fatalf("load task spec: %v", err)
	}
	if spec.SourceHash == "" {
		t.Fatalf("expected source hash to be populated")
	}
	if spec.Timeout.Duration != 3*time.Second {
		t.Fatalf("expected default timeout, got %s", spec.Timeout.Duration)
	}
	if spec.Trigger != "scheduled" {
		t.Fatalf("expected default trigger scheduled, got %q", spec.Trigger)
	}
}

func TestTaskSpecValidateTrigger(t *testing.T) {
	t.Parallel()

	t.Run("default trigger is valid", func(t *testing.T) {
		spec := TaskSpec{ID: "a", Kind: "http_check"}
		if err := spec.ValidateBasic(); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("invalid trigger rejected", func(t *testing.T) {
		spec := TaskSpec{ID: "a", Kind: "http_check", Trigger: "cron"}
		if err := spec.ValidateBasic(); err == nil {
			t.Fatalf("expected invalid trigger to be rejected")
		}
	})

	t.Run("on_run requires watch_task", func(t *testing.T) {
		spec := TaskSpec{ID: "a", Kind: "http_check", Trigger: "on_run"}
		if err := spec.ValidateBasic(); err == nil {
			t.Fatalf("expected on_run without watch_task to fail")
		}
	})

	t.Run("on_run with watch_task is valid", func(t *testing.T) {
		spec := TaskSpec{ID: "a", Kind: "http_check", Trigger: "on_run", WatchTaskID: "source-task"}
		if err := spec.ValidateBasic(); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("watch_task without on_run rejected", func(t *testing.T) {
		spec := TaskSpec{ID: "a", Kind: "http_check", Trigger: "scheduled", WatchTaskID: "source-task"}
		if err := spec.ValidateBasic(); err == nil {
			t.Fatalf("expected watch_task without on_run to fail")
		}
	})
}
