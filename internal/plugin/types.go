package plugin

import (
	"context"
	"time"

	"pulseops/internal/evaluator"
	"pulseops/internal/pluginmodel"
	"pulseops/internal/task"
)

const (
	SchemaVersionV1 = pluginmodel.SchemaVersionV1

	CapabilityTaskTemplate = pluginmodel.CapabilityTaskTemplate
	CapabilityTaskDriver   = pluginmodel.CapabilityTaskDriver
	CapabilityDataSource   = pluginmodel.CapabilityDataSource
	CapabilityAIDataSource = pluginmodel.CapabilityAIDataSource
	CapabilityOutputWriter = pluginmodel.CapabilityOutputWriter
	CapabilityEvaluator    = pluginmodel.CapabilityEvaluator
	CapabilityTraceSink    = pluginmodel.CapabilityTraceSink
	CapabilityHook         = pluginmodel.CapabilityHook
	CapabilityUIExtension  = pluginmodel.CapabilityUIExtension

	PackageStatusEnabled      = pluginmodel.PackageStatusEnabled
	PackageStatusDisabled     = pluginmodel.PackageStatusDisabled
	PackageStatusDegraded     = pluginmodel.PackageStatusDegraded
	PackageStatusNotInstalled = pluginmodel.PackageStatusNotInstalled

	ReleaseStatusStaged     = pluginmodel.ReleaseStatusStaged
	ReleaseStatusValidating = pluginmodel.ReleaseStatusValidating
	ReleaseStatusValidated  = pluginmodel.ReleaseStatusValidated
	ReleaseStatusActive     = pluginmodel.ReleaseStatusActive
	ReleaseStatusDraining   = pluginmodel.ReleaseStatusDraining
	ReleaseStatusRetired    = pluginmodel.ReleaseStatusRetired
	ReleaseStatusDeleted    = pluginmodel.ReleaseStatusDeleted
	ReleaseStatusFailed     = pluginmodel.ReleaseStatusFailed

	AssetScopePluginShared     = pluginmodel.AssetScopePluginShared
	AssetScopeCapabilityShared = pluginmodel.AssetScopeCapabilityShared
	AssetScopeConfigInstance   = pluginmodel.AssetScopeConfigInstance
)

type Manifest = pluginmodel.Manifest
type TaskTemplateManifest = pluginmodel.TaskTemplateManifest
type NamedCapability = pluginmodel.NamedCapability
type RuntimeCapability = pluginmodel.RuntimeCapability
type DataSourceManifest = pluginmodel.DataSourceManifest
type UIExtensionManifest = pluginmodel.UIExtensionManifest
type Schema = pluginmodel.Schema
type SchemaField = pluginmodel.SchemaField
type ConfigClass = pluginmodel.ConfigClass
type ConfigSchema = pluginmodel.ConfigSchema
type ConfigField = pluginmodel.ConfigField
type ConfigOption = pluginmodel.ConfigOption
type ConfigValidation = pluginmodel.ConfigValidation
type ConfigUI = pluginmodel.ConfigUI
type ConfigCondition = pluginmodel.ConfigCondition
type Capability = pluginmodel.Capability
type PackageRecord = pluginmodel.PackageRecord
type ReleaseRecord = pluginmodel.ReleaseRecord
type GenerationRecord = pluginmodel.GenerationRecord
type EventRecord = pluginmodel.EventRecord
type ConfigInstanceRecord = pluginmodel.ConfigInstanceRecord
type ConfigVersionRecord = pluginmodel.ConfigVersionRecord
type AssetRecord = pluginmodel.AssetRecord
type AssetVersionRecord = pluginmodel.AssetVersionRecord
type SecretRecord = pluginmodel.SecretRecord
type SecretValueRecord = pluginmodel.SecretValueRecord
type ConfigEventRecord = pluginmodel.ConfigEventRecord
type GenerationCommit = pluginmodel.GenerationCommit
type Catalog = pluginmodel.Catalog
type CatalogStats = pluginmodel.CatalogStats
type PluginView = pluginmodel.PluginView

type ConfigValidationRequest struct {
	PluginID       string
	CapabilityID   string
	CapabilityName string
	Scope          string
	Action         string
	Config         map[string]any
	Input          map[string]any
}

type RuntimeRegistration struct {
	Drivers    []task.Driver
	Evaluators []evaluator.ScenarioEvaluator
}

type BundledPlugin struct {
	Manifest           Manifest
	DefaultEnabled     bool
	Disableable        bool
	ForceDefaultStatus bool
	Build              func() RuntimeRegistration
}

type Generation struct {
	ID                string              `json:"generation_id"`
	ActiveVersions    map[string]string   `json:"active_versions"`
	Capabilities      []Capability        `json:"capabilities"`
	CreatedAt         time.Time           `json:"created_at"`
	DriverRegistry    *task.Registry      `json:"-"`
	EvaluatorRegistry *evaluator.Registry `json:"-"`
}

type Store interface {
	EnsurePluginPackage(ctx context.Context, record PackageRecord) error
	UpsertPluginRelease(ctx context.Context, record ReleaseRecord) error
	ListPluginPackages(ctx context.Context) ([]PackageRecord, error)
	GetPluginPackage(ctx context.Context, pluginID string) (PackageRecord, error)
	UpdatePluginPackageStatus(ctx context.Context, pluginID, status, lastError string) error
	ListPluginReleases(ctx context.Context, pluginID string) ([]ReleaseRecord, error)
	GetPluginRelease(ctx context.Context, pluginID, version string) (ReleaseRecord, error)
	UpdatePluginReleaseStatus(ctx context.Context, pluginID, version, status, validationError string) error
	SetActivePluginVersion(ctx context.Context, pluginID, version, generationID string) error
	GetActivePluginVersions(ctx context.Context) (map[string]string, error)
	InsertPluginGeneration(ctx context.Context, record GenerationRecord) error
	InsertPluginEvent(ctx context.Context, record EventRecord) error
	CommitPluginGeneration(ctx context.Context, commit GenerationCommit) error
}
