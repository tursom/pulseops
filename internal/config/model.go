package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	raw := strings.TrimSpace(string(text))
	if raw == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

func (d Duration) String() string {
	if d.Duration == 0 {
		return ""
	}
	return d.Duration.String()
}

type Config struct {
	BaseDir       string              `toml:"-"`
	Server        ServerConfig        `toml:"server"`
	Task          TaskConfig          `toml:"task"`
	State         StateConfig         `toml:"state"`
	ArtifactStore ArtifactStoreConfig `toml:"artifact_store"`
	Trace         TraceConfig         `toml:"trace"`
	AI            AIConfig            `toml:"ai"`
}

type AIConfig struct {
	Enabled        bool     `toml:"enabled"`
	Endpoint       string   `toml:"endpoint"`
	APIKey         string   `toml:"api_key"`
	Model          string   `toml:"model"`
	DefaultTimeout Duration `toml:"default_timeout"`
	MaxTokens      int      `toml:"max_tokens"`
	Temperature    float64  `toml:"temperature"`
	PluginDir      string   `toml:"plugin_dir" json:"plugin_dir"`
}

type ServerConfig struct {
	Addr         string   `toml:"addr"`
	ReadTimeout  Duration `toml:"read_timeout"`
	WriteTimeout Duration `toml:"write_timeout"`
}

type TaskConfig struct {
	ConfigDir         string   `toml:"config_dir"`
	ReloadDebounce    Duration `toml:"reload_debounce"`
	DefaultTimeout    Duration `toml:"default_timeout"`
	DefaultTraceLevel string   `toml:"default_trace_level"`
}

type StateConfig struct {
	Backend string `toml:"backend"`
	DSN     string `toml:"dsn"`
}

type ArtifactStoreConfig struct {
	Kind           string   `toml:"kind"`
	Provider       string   `toml:"provider"`
	Bucket         string   `toml:"bucket"`
	Endpoint       string   `toml:"endpoint"`
	Region         string   `toml:"region"`
	BasePath       string   `toml:"base_path"`
	ForcePathStyle bool     `toml:"force_path_style"`
	PresignTTL     Duration `toml:"presign_ttl"`
	AccessKey      string   `toml:"access_key"`
	SecretKey      string   `toml:"secret_key"`
	UseSSL         bool     `toml:"use_ssl"`
}

type TraceConfig struct {
	Sinks map[string]SinkConfig `toml:"sinks"`
}

type SinkConfig struct {
	Kind    string   `toml:"kind"`
	DSN     string   `toml:"dsn"`
	URL     string   `toml:"url"`
	Timeout Duration `toml:"timeout"`
}

type TaskSpec struct {
	ID       string            `toml:"id" json:"id"`
	Name     string            `toml:"name" json:"name"`
	Kind     string            `toml:"kind" json:"kind"`
	Enabled  bool              `toml:"enabled" json:"enabled"`
	Interval Duration          `toml:"interval" json:"interval"`
	Cron     string            `toml:"cron" json:"cron"`
	Timeout  Duration          `toml:"timeout" json:"timeout"`
	Labels   map[string]string `toml:"labels" json:"labels"`
	Params   map[string]any    `toml:"params" json:"params"`
	Trace    TracePolicy       `toml:"trace" json:"trace"`
	Alert    AlertPolicy       `toml:"alert" json:"alert"`

	Trigger        string `toml:"trigger" json:"trigger"`                 // "scheduled"|"manual"|"on_run", default "scheduled"
	WatchTaskID    string `toml:"watch_task" json:"watch_task"`           // 当 trigger=on_run 时监听的源任务 ID
	WatchCondition string `toml:"watch_condition" json:"watch_condition"` // 可选触发条件表达式，如 "check_status == 'fail'"

	SourcePath string `toml:"-" json:"source_path"`
	SourceHash string `toml:"-" json:"source_hash"`
}

type TracePolicy struct {
	Enabled            bool     `toml:"enabled" json:"enabled"`
	Level              string   `toml:"level" json:"level"`
	Sinks              []string `toml:"sinks" json:"sinks"`
	RetainDays         int      `toml:"retain_days" json:"retain_days"`
	StoreStdout        bool     `toml:"store_stdout" json:"store_stdout"`
	StoreStderr        bool     `toml:"store_stderr" json:"store_stderr"`
	StoreResultPayload bool     `toml:"store_result_payload" json:"store_result_payload"`
	MaxPayloadBytes    int      `toml:"max_payload_bytes" json:"max_payload_bytes"`
	MaskFields         []string `toml:"mask_fields" json:"mask_fields"`
}

type AlertPolicy struct {
	ConsecutiveFailures int      `toml:"consecutive_failures" json:"consecutive_failures"`
	Channels            []string `toml:"channels" json:"channels"`
	RecoverNotify       bool     `toml:"recover_notify" json:"recover_notify"`
}

func (cfg *Config) Normalize() {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.ReadTimeout.Duration == 0 {
		cfg.Server.ReadTimeout.Duration = 5 * time.Second
	}
	if cfg.Server.WriteTimeout.Duration == 0 {
		cfg.Server.WriteTimeout.Duration = 10 * time.Second
	}
	if cfg.Task.ConfigDir == "" {
		cfg.Task.ConfigDir = filepath.Join("configs", "tasks")
	}
	if cfg.Task.ReloadDebounce.Duration == 0 {
		cfg.Task.ReloadDebounce.Duration = 500 * time.Millisecond
	}
	if cfg.Task.DefaultTimeout.Duration == 0 {
		cfg.Task.DefaultTimeout.Duration = 10 * time.Second
	}
	if cfg.Task.DefaultTraceLevel == "" {
		cfg.Task.DefaultTraceLevel = "summary"
	}
	if cfg.State.Backend == "" {
		cfg.State.Backend = "postgres"
	}
	if cfg.State.DSN == "" {
		cfg.State.DSN = "postgres://pulseops:secret@127.0.0.1:5432/pulseops?sslmode=disable"
	}
	if cfg.ArtifactStore.Kind == "" {
		cfg.ArtifactStore.Kind = "s3"
	}
	if cfg.ArtifactStore.Provider == "" {
		cfg.ArtifactStore.Provider = "minio"
	}
	if cfg.ArtifactStore.Region == "" {
		cfg.ArtifactStore.Region = "auto"
	}
	if cfg.ArtifactStore.PresignTTL.Duration == 0 {
		cfg.ArtifactStore.PresignTTL.Duration = 15 * time.Minute
	}
	cfg.Task.ConfigDir = ResolvePath(cfg.BaseDir, cfg.Task.ConfigDir)
	for name, sink := range cfg.Trace.Sinks {
		cfg.Trace.Sinks[name] = sink
	}
	if cfg.AI.Endpoint != "" && cfg.AI.Enabled {
		if cfg.AI.Model == "" {
			cfg.AI.Model = "deepseek-chat"
		}
		if cfg.AI.DefaultTimeout.Duration == 0 {
			cfg.AI.DefaultTimeout.Duration = 30 * time.Second
		}
		if cfg.AI.MaxTokens == 0 {
			cfg.AI.MaxTokens = 4096
		}
		if cfg.AI.PluginDir == "" {
			cfg.AI.PluginDir = "plugins"
		}
		cfg.AI.PluginDir = ResolvePath(cfg.BaseDir, cfg.AI.PluginDir)
	}
}

func (cfg Config) Validate() error {
	if cfg.Task.ConfigDir == "" {
		return errors.New("task.config_dir is required")
	}
	if cfg.State.Backend != "postgres" {
		return fmt.Errorf("unsupported state backend %q", cfg.State.Backend)
	}
	if cfg.State.DSN == "" {
		return errors.New("state.dsn is required")
	}
	if cfg.ArtifactStore.Kind != "s3" {
		return fmt.Errorf("unsupported artifact store kind %q", cfg.ArtifactStore.Kind)
	}
	if cfg.ArtifactStore.Bucket == "" {
		return errors.New("artifact_store.bucket is required")
	}
	if cfg.ArtifactStore.Endpoint == "" {
		return errors.New("artifact_store.endpoint is required")
	}
	return nil
}

func (spec *TaskSpec) Normalize(global Config) {
	if spec.Name == "" {
		spec.Name = spec.ID
	}
	if spec.Timeout.Duration == 0 {
		spec.Timeout = global.Task.DefaultTimeout
	}
	if spec.Trace.Level == "" {
		spec.Trace.Level = global.Task.DefaultTraceLevel
	}
	if spec.Trace.MaxPayloadBytes == 0 {
		spec.Trace.MaxPayloadBytes = 4096
	}
	if spec.Trace.Level != "" || len(spec.Trace.Sinks) > 0 || spec.Trace.StoreResultPayload || spec.Trace.StoreStdout || spec.Trace.StoreStderr {
		spec.Trace.Enabled = true
	}
	if spec.Trace.Enabled && len(spec.Trace.Sinks) == 0 {
		for name := range global.Trace.Sinks {
			spec.Trace.Sinks = append(spec.Trace.Sinks, name)
			break
		}
	}
	if spec.Trigger == "" {
		spec.Trigger = "scheduled"
	}
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	if spec.Params == nil {
		spec.Params = map[string]any{}
	}
}

func (spec TaskSpec) ValidateBasic() error {
	if spec.ID == "" {
		return errors.New("task id is required")
	}
	if spec.Kind == "" {
		return errors.New("task kind is required")
	}
	switch spec.Trigger {
	case "", "scheduled", "manual", "on_run":
	default:
		return fmt.Errorf("invalid trigger %q, must be scheduled, manual, or on_run", spec.Trigger)
	}
	if spec.Trigger == "on_run" && spec.WatchTaskID == "" {
		return errors.New("task with trigger on_run must set watch_task")
	}
	if spec.Trigger != "on_run" && spec.WatchTaskID != "" {
		return errors.New("watch_task is only valid when trigger is on_run")
	}
	if spec.Interval.Duration > 0 && spec.Cron != "" {
		return errors.New("task cannot set both interval and cron")
	}
	if spec.Interval.Duration < 0 {
		return errors.New("task interval must be positive")
	}
	if spec.Timeout.Duration < 0 {
		return errors.New("task timeout must be positive")
	}
	return nil
}

func ResolvePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}
