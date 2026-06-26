package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
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

type SinkEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type GlobalSettings struct {
	Sinks             []SinkEntry `json:"sinks"`
	MaxPayloadBytes   int         `json:"max_payload_bytes"`
	DefaultRetainDays int         `json:"default_retain_days"`
}

type PlatformConfigSummary struct {
	Mode          string                `json:"mode"`
	Applied       bool                  `json:"applied"`
	Warnings      []string              `json:"warnings"`
	Server        ServerConfigSummary   `json:"server"`
	Task          TaskConfigSummary     `json:"task"`
	State         StateConfigSummary    `json:"state"`
	ArtifactStore ArtifactConfigSummary `json:"artifact_store"`
	AI            AIConfigSummary       `json:"ai"`
}

type ServerConfigSummary struct {
	Addr string `json:"addr"`
}

type TaskConfigSummary struct {
	ConfigDir string `json:"config_dir"`
}

type StateConfigSummary struct {
	Backend string `json:"backend"`
}

type ArtifactConfigSummary struct {
	Kind           string `json:"kind"`
	Provider       string `json:"provider"`
	Bucket         string `json:"bucket"`
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	BasePath       string `json:"base_path"`
	PresignTTL     string `json:"presign_ttl"`
	ForcePathStyle bool   `json:"force_path_style"`
	UseSSL         bool   `json:"use_ssl"`
	AccessKey      string `json:"access_key,omitempty"`
	SecretKey      string `json:"secret_key,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

type AIConfigSummary struct {
	Enabled     bool    `json:"enabled"`
	Endpoint    string  `json:"endpoint"`
	Model       string  `json:"model"`
	Timeout     string  `json:"timeout"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	PluginDir   string  `json:"plugin_dir"`
	Status      string  `json:"status"`
	Error       string  `json:"error,omitempty"`
}

func ParseGlobalSettings(sinksRaw, maxBytesRaw, retainDaysRaw string) GlobalSettings {
	s := GlobalSettings{
		MaxPayloadBytes:   4096,
		DefaultRetainDays: 30,
	}
	if sinksRaw != "" {
		json.Unmarshal([]byte(sinksRaw), &s.Sinks)
	}
	if maxBytesRaw != "" {
		if n, err := strconv.Atoi(maxBytesRaw); err == nil {
			s.MaxPayloadBytes = n
		}
	}
	if retainDaysRaw != "" {
		if n, err := strconv.Atoi(retainDaysRaw); err == nil {
			s.DefaultRetainDays = n
		}
	}
	return s
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

	Trigger        string           `toml:"trigger" json:"trigger"`                 // "scheduled"|"manual"|"on_run", default "scheduled"
	WatchTaskID    string           `toml:"watch_task" json:"watch_task"`           // 当 trigger=on_run 时监听的源任务 ID
	WatchCondition string           `toml:"watch_condition" json:"watch_condition"` // 可选触发条件表达式，如 "check_status == 'fail'"
	Dependencies   []TaskDependency `toml:"-" json:"dependencies,omitempty"`

	SourcePath string `toml:"-" json:"source_path"`
	SourceHash string `toml:"-" json:"source_hash"`
}

type TracePolicy struct {
	Level      string   `toml:"level" json:"level"`
	RetainDays int      `toml:"retain_days" json:"retain_days"`
	MaskFields []string `toml:"mask_fields" json:"mask_fields"`
}

type AlertPolicy struct {
	ConsecutiveFailures int      `toml:"consecutive_failures" json:"consecutive_failures"`
	Channels            []string `toml:"channels" json:"channels"`
	RecoverNotify       bool     `toml:"recover_notify" json:"recover_notify"`
}

type Pipeline struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type TaskDependency struct {
	ID               string         `json:"id" db:"id"`
	UpstreamTaskID   string         `json:"upstream_task_id" db:"upstream_task_id"`
	DownstreamTaskID string         `json:"downstream_task_id" db:"downstream_task_id"`
	Condition        string         `json:"condition" db:"condition"`
	SourceKey        string         `json:"source_key,omitempty" db:"source_key"`
	Params           map[string]any `json:"params,omitempty" db:"-"`
	ParamsJSON       []byte         `json:"-" db:"params_json"`
	CreatedAt        time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at" db:"updated_at"`
}

type TaskDefinition struct {
	TaskID         string            `json:"task_id" db:"task_id"`
	Name           string            `json:"name" db:"name"`
	Kind           string            `json:"kind" db:"kind"`
	Enabled        bool              `json:"enabled" db:"enabled"`
	Interval       string            `json:"interval" db:"interval"`
	Cron           string            `json:"cron" db:"cron"`
	Timeout        string            `json:"timeout" db:"timeout"`
	Labels         map[string]string `json:"labels" db:"-"`
	LabelsJSON     []byte            `json:"-" db:"labels_json"`
	Params         map[string]any    `json:"params" db:"-"`
	ParamsJSON     []byte            `json:"-" db:"params_json"`
	Trigger        string            `json:"trigger" db:"trigger"`
	WatchTaskID    string            `json:"watch_task_id" db:"watch_task_id"`
	WatchCondition string            `json:"watch_condition" db:"watch_condition"`
	Trace          TracePolicy       `json:"trace,omitempty" db:"-"`
	TraceJSON      []byte            `json:"-" db:"trace_json"`
	Alert          AlertPolicy       `json:"alert,omitempty" db:"-"`
	AlertJSON      []byte            `json:"-" db:"alert_json"`
	PipelineID     *string           `json:"pipeline_id" db:"pipeline_id"`
	Dependencies   []TaskDependency  `json:"dependencies,omitempty" db:"-"`
	CreatedAt      time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at" db:"updated_at"`
}

func (d *TaskDefinition) ToTaskSpec() (TaskSpec, error) {
	spec := TaskSpec{
		ID:             d.TaskID,
		Name:           d.Name,
		Kind:           d.Kind,
		Enabled:        d.Enabled,
		Cron:           d.Cron,
		Trigger:        d.Trigger,
		WatchTaskID:    d.WatchTaskID,
		WatchCondition: d.WatchCondition,
		Dependencies:   d.Dependencies,
	}
	if d.Interval != "" {
		if err := spec.Interval.UnmarshalText([]byte(d.Interval)); err != nil {
			return spec, fmt.Errorf("parse interval: %w", err)
		}
	}
	if d.Timeout != "" {
		if err := spec.Timeout.UnmarshalText([]byte(d.Timeout)); err != nil {
			return spec, fmt.Errorf("parse timeout: %w", err)
		}
	}
	if len(d.LabelsJSON) > 0 {
		spec.Labels = map[string]string{}
		if err := json.Unmarshal(d.LabelsJSON, &spec.Labels); err != nil {
			return spec, fmt.Errorf("unmarshal labels: %w", err)
		}
	}
	if len(d.ParamsJSON) > 0 {
		spec.Params = map[string]any{}
		if err := json.Unmarshal(d.ParamsJSON, &spec.Params); err != nil {
			return spec, fmt.Errorf("unmarshal params: %w", err)
		}
	}
	if len(d.TraceJSON) > 0 {
		if err := json.Unmarshal(d.TraceJSON, &spec.Trace); err != nil {
			return spec, fmt.Errorf("unmarshal trace: %w", err)
		}
	}
	if len(d.AlertJSON) > 0 {
		if err := json.Unmarshal(d.AlertJSON, &spec.Alert); err != nil {
			return spec, fmt.Errorf("unmarshal alert: %w", err)
		}
	}
	return spec, nil
}

func FromTaskSpec(spec TaskSpec) TaskDefinition {
	d := TaskDefinition{
		TaskID:         spec.ID,
		Name:           spec.Name,
		Kind:           spec.Kind,
		Enabled:        spec.Enabled,
		Interval:       spec.Interval.String(),
		Cron:           spec.Cron,
		Timeout:        spec.Timeout.String(),
		Trigger:        spec.Trigger,
		WatchTaskID:    spec.WatchTaskID,
		WatchCondition: spec.WatchCondition,
		Dependencies:   spec.Dependencies,
	}
	if labelsJSON, err := json.Marshal(spec.Labels); err == nil && string(labelsJSON) != "null" {
		d.LabelsJSON = labelsJSON
		d.Labels = spec.Labels
	}
	if paramsJSON, err := json.Marshal(spec.Params); err == nil && string(paramsJSON) != "null" {
		d.ParamsJSON = paramsJSON
		d.Params = spec.Params
	}
	if traceJSON, err := json.Marshal(spec.Trace); err == nil && string(traceJSON) != "null" {
		d.TraceJSON = traceJSON
	}
	if alertJSON, err := json.Marshal(spec.Alert); err == nil && string(alertJSON) != "null" {
		d.AlertJSON = alertJSON
	}
	return d
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
		hasDependency := false
		for _, dep := range spec.Dependencies {
			if dep.UpstreamTaskID != "" {
				hasDependency = true
				break
			}
		}
		if !hasDependency {
			return errors.New("task with trigger on_run must set watch_task or dependencies")
		}
	}
	if spec.Trigger != "on_run" && spec.WatchTaskID != "" {
		return errors.New("watch_task is only valid when trigger is on_run")
	}
	if spec.WatchCondition != "" {
		if err := ValidateWatchCondition(spec.WatchCondition); err != nil {
			return err
		}
	}
	for _, dep := range spec.Dependencies {
		if dep.UpstreamTaskID == "" {
			return errors.New("dependency upstream_task_id is required")
		}
		if dep.DownstreamTaskID != "" && dep.DownstreamTaskID != spec.ID {
			return fmt.Errorf("dependency downstream_task_id %q does not match task id %q", dep.DownstreamTaskID, spec.ID)
		}
		if dep.Condition != "" {
			if err := ValidateWatchCondition(dep.Condition); err != nil {
				return fmt.Errorf("dependency %s: %w", dep.UpstreamTaskID, err)
			}
		}
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

func (spec TaskSpec) DependencyRules() []TaskDependency {
	rules := make([]TaskDependency, 0, len(spec.Dependencies)+1)
	if spec.Trigger == "on_run" && spec.WatchTaskID != "" {
		rules = append(rules, TaskDependency{
			ID:               "legacy:" + spec.WatchTaskID + ":" + spec.ID,
			UpstreamTaskID:   spec.WatchTaskID,
			DownstreamTaskID: spec.ID,
			Condition:        spec.WatchCondition,
		})
	}
	for _, dep := range spec.Dependencies {
		if dep.DownstreamTaskID == "" {
			dep.DownstreamTaskID = spec.ID
		}
		rules = append(rules, dep)
	}
	return rules
}

func ValidateWatchCondition(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return nil
	}
	parts := strings.SplitN(condition, "==", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid watch_condition %q, expected field == value", condition)
	}
	field := strings.TrimSpace(parts[0])
	switch field {
	case "check_status", "run_status":
	default:
		return fmt.Errorf("unsupported watch_condition field %q", field)
	}
	expected := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
	if expected == "" {
		return fmt.Errorf("watch_condition %q has empty expected value", condition)
	}
	switch field {
	case "check_status":
		if expected != "pass" && expected != "fail" && expected != "unknown" {
			return fmt.Errorf("unsupported check_status value %q", expected)
		}
	case "run_status":
		if expected != "success" && expected != "failed" && expected != "timeout" {
			return fmt.Errorf("unsupported run_status value %q", expected)
		}
	}
	return nil
}

func ResolvePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}
