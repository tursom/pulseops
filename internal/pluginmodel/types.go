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

	AssetScopePluginShared     = "plugin_shared"
	AssetScopeCapabilityShared = "capability_shared"
	AssetScopeConfigInstance   = "config_instance"
)

type Manifest struct {
	SchemaVersion string   `toml:"schema_version" yaml:"schema_version" json:"schema_version"`
	ID            string   `toml:"id" yaml:"id" json:"id"`
	Name          string   `toml:"name" yaml:"name" json:"name"`
	Version       string   `toml:"version" yaml:"version" json:"version"`
	Description   string   `toml:"description" yaml:"description" json:"description,omitempty"`
	Author        string   `toml:"author" yaml:"author" json:"author,omitempty"`
	Homepage      string   `toml:"homepage" yaml:"homepage" json:"homepage,omitempty"`
	Enabled       bool     `toml:"enabled" yaml:"enabled" json:"enabled"`
	Permissions   []string `toml:"permissions" yaml:"permissions" json:"permissions"`

	ConfigClasses map[string]ConfigClass `toml:"config_classes" yaml:"config_classes" json:"config_classes,omitempty"`
	Config        *ConfigSchema          `toml:"config" yaml:"config" json:"config,omitempty"`

	TaskTemplates []TaskTemplateManifest `toml:"task_templates" yaml:"task_templates" json:"task_templates,omitempty"`
	TaskDrivers   []NamedCapability      `toml:"task_drivers" yaml:"task_drivers" json:"task_drivers,omitempty"`
	DataSources   []DataSourceManifest   `toml:"data_sources" yaml:"data_sources" json:"data_sources,omitempty"`
	AIDataSources []RuntimeCapability    `toml:"ai_data_sources" yaml:"ai_data_sources" json:"ai_data_sources,omitempty"`
	OutputWriters []NamedCapability      `toml:"output_writers" yaml:"output_writers" json:"output_writers,omitempty"`
	Evaluators    []NamedCapability      `toml:"evaluators" yaml:"evaluators" json:"evaluators,omitempty"`
	TraceSinks    []NamedCapability      `toml:"trace_sinks" yaml:"trace_sinks" json:"trace_sinks,omitempty"`
	Hooks         []NamedCapability      `toml:"hooks" yaml:"hooks" json:"hooks,omitempty"`
	UIExtensions  []UIExtensionManifest  `toml:"ui_extensions" yaml:"ui_extensions" json:"ui_extensions,omitempty"`
}

type TaskTemplateManifest struct {
	ID          string         `toml:"id" yaml:"id" json:"id"`
	Kind        string         `toml:"kind" yaml:"kind" json:"kind"`
	Title       string         `toml:"title" yaml:"title" json:"title"`
	Description string         `toml:"description" yaml:"description" json:"description,omitempty"`
	Permissions []string       `toml:"permissions" yaml:"permissions" json:"permissions,omitempty"`
	Defaults    map[string]any `toml:"defaults" yaml:"defaults" json:"defaults,omitempty"`
	Params      map[string]any `toml:"params" yaml:"params" json:"params,omitempty"`
	Schema      Schema         `toml:"schema" yaml:"schema" json:"schema,omitempty"`
	Config      *ConfigSchema  `toml:"config" yaml:"config" json:"config,omitempty"`
}

type NamedCapability struct {
	Name        string         `toml:"name" yaml:"name" json:"name"`
	Title       string         `toml:"title" yaml:"title" json:"title,omitempty"`
	Description string         `toml:"description" yaml:"description" json:"description,omitempty"`
	Runtime     string         `toml:"runtime" yaml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" yaml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" yaml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" yaml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" yaml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" yaml:"defaults" json:"defaults,omitempty"`
	Config      *ConfigSchema  `toml:"config" yaml:"config" json:"config,omitempty"`
}

type RuntimeCapability struct {
	Name        string         `toml:"name" yaml:"name" json:"name"`
	Title       string         `toml:"title" yaml:"title" json:"title,omitempty"`
	Description string         `toml:"description" yaml:"description" json:"description,omitempty"`
	Runtime     string         `toml:"runtime" yaml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" yaml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" yaml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" yaml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" yaml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" yaml:"defaults" json:"defaults,omitempty"`
	Config      *ConfigSchema  `toml:"config" yaml:"config" json:"config,omitempty"`
}

type DataSourceManifest struct {
	Name        string         `toml:"name" yaml:"name" json:"name"`
	Title       string         `toml:"title" yaml:"title" json:"title,omitempty"`
	Description string         `toml:"description" yaml:"description" json:"description,omitempty"`
	Protocol    string         `toml:"protocol" yaml:"protocol" json:"protocol,omitempty"`
	Runtime     string         `toml:"runtime" yaml:"runtime" json:"runtime,omitempty"`
	Entrypoint  string         `toml:"entrypoint" yaml:"entrypoint" json:"entrypoint,omitempty"`
	Endpoint    string         `toml:"endpoint" yaml:"endpoint" json:"endpoint,omitempty"`
	Permissions []string       `toml:"permissions" yaml:"permissions" json:"permissions,omitempty"`
	Schema      Schema         `toml:"schema" yaml:"schema" json:"schema,omitempty"`
	Defaults    map[string]any `toml:"defaults" yaml:"defaults" json:"defaults,omitempty"`
	Config      *ConfigSchema  `toml:"config" yaml:"config" json:"config,omitempty"`
}

type UIExtensionManifest struct {
	ID          string   `toml:"id" yaml:"id" json:"id"`
	Title       string   `toml:"title" yaml:"title" json:"title"`
	Path        string   `toml:"path" yaml:"path" json:"path"`
	Description string   `toml:"description" yaml:"description" json:"description,omitempty"`
	Permissions []string `toml:"permissions" yaml:"permissions" json:"permissions,omitempty"`
}

type Schema map[string]SchemaField

type SchemaField struct {
	Type        string `toml:"type" yaml:"type" json:"type"`
	Required    bool   `toml:"required" yaml:"required" json:"required,omitempty"`
	Description string `toml:"description" yaml:"description" json:"description,omitempty"`
}

type ConfigClass struct {
	Title       string                 `toml:"title" yaml:"title" json:"title,omitempty"`
	Description string                 `toml:"description" yaml:"description" json:"description,omitempty"`
	Fields      map[string]ConfigField `toml:"fields" yaml:"fields" json:"fields,omitempty"`
}

type ConfigSchema struct {
	Title                string                 `toml:"title" yaml:"title" json:"title,omitempty"`
	Description          string                 `toml:"description" yaml:"description" json:"description,omitempty"`
	ValidateAction       string                 `toml:"validate_action" yaml:"validate_action" json:"validate_action,omitempty"`
	AllowPluginConfigRef bool                   `toml:"allow_plugin_config_ref" yaml:"allow_plugin_config_ref" json:"allow_plugin_config_ref,omitempty"`
	Fields               map[string]ConfigField `toml:"fields" yaml:"fields" json:"fields,omitempty"`
}

type ConfigField struct {
	Type        string           `toml:"type" yaml:"type" json:"type"`
	Class       string           `toml:"class" yaml:"class" json:"class,omitempty"`
	Required    bool             `toml:"required" yaml:"required" json:"required,omitempty"`
	Default     any              `toml:"default" yaml:"default" json:"default,omitempty"`
	Overridable bool             `toml:"overridable" yaml:"overridable" json:"overridable,omitempty"`
	Description string           `toml:"description" yaml:"description" json:"description,omitempty"`
	Options     []ConfigOption   `toml:"options" yaml:"options" json:"options,omitempty"`
	Items       *ConfigField     `toml:"items" yaml:"items" json:"items,omitempty"`
	AssetKind   string           `toml:"asset_kind" yaml:"asset_kind" json:"asset_kind,omitempty"`
	AssetScope  string           `toml:"asset_scope" yaml:"asset_scope" json:"asset_scope,omitempty"`
	Accept      []string         `toml:"accept" yaml:"accept" json:"accept,omitempty"`
	Validation  ConfigValidation `toml:"validation" yaml:"validation" json:"validation,omitempty"`
	UI          ConfigUI         `toml:"ui" yaml:"ui" json:"ui,omitempty"`
}

type ConfigOption struct {
	Value any    `toml:"value" yaml:"value" json:"value"`
	Label string `toml:"label" yaml:"label" json:"label,omitempty"`
}

type ConfigValidation struct {
	Min     *float64 `toml:"min" yaml:"min" json:"min,omitempty"`
	Max     *float64 `toml:"max" yaml:"max" json:"max,omitempty"`
	Step    *float64 `toml:"step" yaml:"step" json:"step,omitempty"`
	MinLen  int      `toml:"min_len" yaml:"min_len" json:"min_len,omitempty"`
	MaxLen  int      `toml:"max_len" yaml:"max_len" json:"max_len,omitempty"`
	Pattern string   `toml:"pattern" yaml:"pattern" json:"pattern,omitempty"`
}

type ConfigUI struct {
	Group       string           `toml:"group" yaml:"group" json:"group,omitempty"`
	Label       string           `toml:"label" yaml:"label" json:"label,omitempty"`
	Widget      string           `toml:"widget" yaml:"widget" json:"widget,omitempty"`
	Order       int              `toml:"order" yaml:"order" json:"order,omitempty"`
	Placeholder string           `toml:"placeholder" yaml:"placeholder" json:"placeholder,omitempty"`
	Help        string           `toml:"help" yaml:"help" json:"help,omitempty"`
	Advanced    bool             `toml:"advanced" yaml:"advanced" json:"advanced,omitempty"`
	Collapsed   bool             `toml:"collapsed" yaml:"collapsed" json:"collapsed,omitempty"`
	VisibleWhen *ConfigCondition `toml:"visible_when" yaml:"visible_when" json:"visible_when,omitempty"`
}

type ConfigCondition struct {
	Field string `toml:"field" yaml:"field" json:"field"`
	Op    string `toml:"op" yaml:"op" json:"op"`
	Value any    `toml:"value" yaml:"value" json:"value,omitempty"`
}

type Capability struct {
	ID            string                 `json:"id"`
	GenerationID  string                 `json:"plugin_generation_id,omitempty"`
	Type          string                 `json:"type"`
	Name          string                 `json:"name"`
	Title         string                 `json:"title,omitempty"`
	Description   string                 `json:"description,omitempty"`
	PluginID      string                 `json:"plugin_id"`
	PluginName    string                 `json:"plugin_name"`
	PluginVersion string                 `json:"plugin_version"`
	Kind          string                 `json:"kind,omitempty"`
	Protocol      string                 `json:"protocol,omitempty"`
	Runtime       string                 `json:"runtime,omitempty"`
	Entrypoint    string                 `json:"entrypoint,omitempty"`
	Endpoint      string                 `json:"endpoint,omitempty"`
	Path          string                 `json:"path,omitempty"`
	ReleasePath   string                 `json:"release_path,omitempty"`
	Status        string                 `json:"status"`
	Enabled       bool                   `json:"enabled"`
	Official      bool                   `json:"official"`
	Bundled       bool                   `json:"bundled"`
	Permissions   []string               `json:"permissions,omitempty"`
	Defaults      map[string]any         `json:"defaults,omitempty"`
	Params        map[string]any         `json:"params,omitempty"`
	Schema        Schema                 `json:"schema,omitempty"`
	ConfigClasses map[string]ConfigClass `json:"config_classes,omitempty"`
	Config        *ConfigSchema          `json:"config,omitempty"`
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

type ConfigInstanceRecord struct {
	ID             string    `json:"id"`
	PluginID       string    `json:"plugin_id"`
	CapabilityID   string    `json:"capability_id,omitempty"`
	CapabilityType string    `json:"capability_type,omitempty"`
	CapabilityName string    `json:"capability_name,omitempty"`
	Scope          string    `json:"scope"`
	Title          string    `json:"title,omitempty"`
	Status         string    `json:"status"`
	ActiveVersion  int       `json:"active_version,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ConfigVersionRecord struct {
	InstanceID      string         `json:"instance_id"`
	Version         int            `json:"version"`
	Status          string         `json:"status"`
	Values          map[string]any `json:"values,omitempty"`
	ValidationError string         `json:"validation_error,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ValidatedAt     *time.Time     `json:"validated_at,omitempty"`
	ActivatedAt     *time.Time     `json:"activated_at,omitempty"`
	RetiredAt       *time.Time     `json:"retired_at,omitempty"`
}

type AssetRecord struct {
	ID               string    `json:"id"`
	PluginID         string    `json:"plugin_id"`
	CapabilityID     string    `json:"capability_id,omitempty"`
	ConfigInstanceID string    `json:"config_instance_id,omitempty"`
	Scope            string    `json:"scope"`
	Kind             string    `json:"kind"`
	Title            string    `json:"title,omitempty"`
	Status           string    `json:"status"`
	ActiveVersion    int       `json:"active_version,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AssetVersionRecord struct {
	AssetID         string     `json:"asset_id"`
	Version         int        `json:"version"`
	Status          string     `json:"status"`
	Filename        string     `json:"filename,omitempty"`
	ContentType     string     `json:"content_type,omitempty"`
	StorageURI      string     `json:"storage_uri,omitempty"`
	Content         []byte     `json:"-"`
	SizeBytes       int64      `json:"size_bytes,omitempty"`
	Checksum        string     `json:"checksum,omitempty"`
	ValidationError string     `json:"validation_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ValidatedAt     *time.Time `json:"validated_at,omitempty"`
	ActivatedAt     *time.Time `json:"activated_at,omitempty"`
	RetiredAt       *time.Time `json:"retired_at,omitempty"`
}

type SecretRecord struct {
	ID        string    `json:"id"`
	PluginID  string    `json:"plugin_id"`
	Scope     string    `json:"scope,omitempty"`
	Title     string    `json:"title,omitempty"`
	Masked    string    `json:"masked"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SecretValueRecord struct {
	SecretID       string         `json:"secret_id"`
	Ciphertext     string         `json:"-"`
	EncryptionMeta map[string]any `json:"encryption_meta,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type ConfigEventRecord struct {
	ID           int64     `json:"id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	PluginID     string    `json:"plugin_id,omitempty"`
	Action       string    `json:"action"`
	Status       string    `json:"status"`
	Message      string    `json:"message,omitempty"`
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
