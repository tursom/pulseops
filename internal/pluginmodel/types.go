package pluginmodel

import "time"

const (
	SchemaVersionV1 = "pulseops.plugin/v1"

	CapabilityTaskTemplate = "task_template"
	CapabilityTaskDriver   = "task_driver"
	CapabilityDataSource   = "data_source"
	CapabilityAIDataSource = "ai_data_source"
	CapabilityOutputWriter = "output_writer"
	CapabilityEvaluator    = "evaluator"
	CapabilityTraceSink    = "trace_sink"
	CapabilityHook         = "hook"
	CapabilityUIExtension  = "ui_extension"

	PackageStatusEnabled      = "enabled"
	PackageStatusDisabled     = "disabled"
	PackageStatusDegraded     = "degraded"
	PackageStatusNotInstalled = "not_installed"

	ReleaseStatusStaged     = "staged"
	ReleaseStatusValidating = "validating"
	ReleaseStatusValidated  = "validated"
	ReleaseStatusActive     = "active"
	ReleaseStatusDraining   = "draining"
	ReleaseStatusRetired    = "retired"
	ReleaseStatusDeleted    = "deleted"
	ReleaseStatusFailed     = "failed"
)

type Manifest struct {
	SchemaVersion string   `toml:"schema_version" json:"schema_version"`
	ID            string   `toml:"id" json:"id"`
	Name          string   `toml:"name" json:"name"`
	Version       string   `toml:"version" json:"version"`
	Description   string   `toml:"description" json:"description,omitempty"`
	Author        string   `toml:"author" json:"author,omitempty"`
	Homepage      string   `toml:"homepage" json:"homepage,omitempty"`
	Enabled       bool     `toml:"enabled" json:"enabled"`
	Permissions   []string `toml:"permissions" json:"permissions"`

	TaskTemplates []TaskTemplateManifest `toml:"task_templates" json:"task_templates,omitempty"`
	TaskDrivers   []NamedCapability      `toml:"task_drivers" json:"task_drivers,omitempty"`
	DataSources   []DataSourceManifest   `toml:"data_sources" json:"data_sources,omitempty"`
	AIDataSources []RuntimeCapability    `toml:"ai_data_sources" json:"ai_data_sources,omitempty"`
	OutputWriters []NamedCapability      `toml:"output_writers" json:"output_writers,omitempty"`
	Evaluators    []NamedCapability      `toml:"evaluators" json:"evaluators,omitempty"`
	TraceSinks    []NamedCapability      `toml:"trace_sinks" json:"trace_sinks,omitempty"`
	Hooks         []NamedCapability      `toml:"hooks" json:"hooks,omitempty"`
	UIExtensions  []UIExtensionManifest  `toml:"ui_extensions" json:"ui_extensions,omitempty"`
}

type TaskTemplateManifest struct {
	ID          string         `toml:"id" json:"id"`
	Kind        string         `toml:"kind" json:"kind"`
	Title       string         `toml:"title" json:"title"`
	Description string         `toml:"description" json:"description,omitempty"`
	Permissions []string       `toml:"permissions" json:"permissions,omitempty"`
	Defaults    map[string]any `toml:"defaults" json:"defaults,omitempty"`
	Params      map[string]any `toml:"params" json:"params,omitempty"`
	Schema      Schema         `toml:"schema" json:"schema,omitempty"`
}

type NamedCapability struct {
	Name        string         `toml:"name" json:"name"`
	Title       string         `toml:"title" json:"title,omitempty"`
	Description string         `toml:"description" json:"description,omitempty"`
	Runtime     string         `toml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" json:"defaults,omitempty"`
}

type RuntimeCapability struct {
	Name        string         `toml:"name" json:"name"`
	Title       string         `toml:"title" json:"title,omitempty"`
	Description string         `toml:"description" json:"description,omitempty"`
	Runtime     string         `toml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" json:"defaults,omitempty"`
}

type DataSourceManifest struct {
	Name        string         `toml:"name" json:"name"`
	Title       string         `toml:"title" json:"title,omitempty"`
	Description string         `toml:"description" json:"description,omitempty"`
	Protocol    string         `toml:"protocol" json:"protocol,omitempty"`
	Runtime     string         `toml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" json:"defaults,omitempty"`
}

type UIExtensionManifest struct {
	ID          string   `toml:"id" json:"id"`
	Title       string   `toml:"title" json:"title"`
	Path        string   `toml:"path" json:"path"`
	Description string   `toml:"description" json:"description,omitempty"`
	Permissions []string `toml:"permissions" json:"permissions,omitempty"`
}

type Schema map[string]SchemaField

type SchemaField struct {
	Type        string `toml:"type" json:"type"`
	Required    bool   `toml:"required" json:"required,omitempty"`
	Description string `toml:"description" json:"description,omitempty"`
}

type Capability struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	Name          string         `json:"name"`
	Title         string         `json:"title,omitempty"`
	Description   string         `json:"description,omitempty"`
	PluginID      string         `json:"plugin_id"`
	PluginName    string         `json:"plugin_name"`
	PluginVersion string         `json:"plugin_version"`
	Kind          string         `json:"kind,omitempty"`
	Protocol      string         `json:"protocol,omitempty"`
	Runtime       string         `json:"runtime,omitempty"`
	Entrypoint    string         `json:"entrypoint,omitempty"`
	Endpoint      string         `json:"endpoint,omitempty"`
	Path          string         `json:"path,omitempty"`
	ReleasePath   string         `json:"release_path,omitempty"`
	Status        string         `json:"status"`
	Enabled       bool           `json:"enabled"`
	Official      bool           `json:"official"`
	Bundled       bool           `json:"bundled"`
	Permissions   []string       `json:"permissions,omitempty"`
	Defaults      map[string]any `json:"defaults,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	Schema        Schema         `json:"schema,omitempty"`
}

type PackageRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	Official    bool      `json:"official"`
	Bundled     bool      `json:"bundled"`
	Status      string    `json:"status"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ReleaseRecord struct {
	PluginID        string     `json:"plugin_id"`
	Version         string     `json:"version"`
	SchemaVersion   string     `json:"schema_version"`
	Manifest        Manifest   `json:"manifest"`
	Path            string     `json:"path,omitempty"`
	Status          string     `json:"status"`
	Checksum        string     `json:"checksum,omitempty"`
	ValidationError string     `json:"validation_error,omitempty"`
	Official        bool       `json:"official"`
	Bundled         bool       `json:"bundled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ValidatedAt     *time.Time `json:"validated_at,omitempty"`
	ActivatedAt     *time.Time `json:"activated_at,omitempty"`
}

type GenerationRecord struct {
	ID             string            `json:"generation_id"`
	Status         string            `json:"status"`
	ActiveVersions map[string]string `json:"active_versions"`
	Capabilities   []Capability      `json:"capabilities"`
	CreatedAt      time.Time         `json:"created_at"`
	RetiredAt      *time.Time        `json:"retired_at,omitempty"`
}

type EventRecord struct {
	ID           int64     `json:"id"`
	PluginID     string    `json:"plugin_id,omitempty"`
	Version      string    `json:"version,omitempty"`
	Action       string    `json:"action"`
	Status       string    `json:"status"`
	Message      string    `json:"message,omitempty"`
	GenerationID string    `json:"generation_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type GenerationCommit struct {
	PackageID             string
	PackageStatus         string
	PackageLastError      string
	SetActiveVersion      bool
	ExpectedActiveVersion string
	ActiveVersion         string
	ActiveReleaseVersion  string
	DrainingVersion       string
	Generation            GenerationRecord
	Event                 EventRecord
}

type Catalog struct {
	GeneratedAt        time.Time    `json:"generated_at"`
	PluginDir          string       `json:"plugin_dir"`
	Status             string       `json:"status"`
	ActiveGenerationID string       `json:"active_generation_id,omitempty"`
	Stats              CatalogStats `json:"stats"`
	Plugins            []PluginView `json:"plugins"`
	Errors             []string     `json:"errors,omitempty"`
}

type CatalogStats struct {
	Total        int `json:"total"`
	Enabled      int `json:"enabled"`
	Disabled     int `json:"disabled"`
	Errors       int `json:"errors"`
	Capabilities int `json:"capabilities"`
}

type PluginView struct {
	Package       PackageRecord   `json:"package"`
	ActiveVersion string          `json:"active_version,omitempty"`
	Release       *ReleaseRecord  `json:"release,omitempty"`
	Releases      []ReleaseRecord `json:"releases,omitempty"`
	Capabilities  []Capability    `json:"capabilities"`
	Permissions   []string        `json:"permissions,omitempty"`
}
