package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrMetaNotFound = errors.New("metadata key not found")

type TaskState struct {
	TaskID            string            `json:"task_id"`
	Name              string            `json:"name"`
	Kind              string            `json:"kind"`
	Enabled           bool              `json:"enabled"`
	Status            string            `json:"status"`
	Labels            map[string]string `json:"labels"`
	LastRunAt         *time.Time        `json:"last_run_at,omitempty"`
	NextRunAt         *time.Time        `json:"next_run_at,omitempty"`
	LastRunStatus     string            `json:"last_run_status,omitempty"`
	LastCheckStatus   string            `json:"last_check_status,omitempty"`
	LastError         string            `json:"last_error,omitempty"`
	LastDurationMS    int64             `json:"last_duration_ms,omitempty"`
	LastReloadError   string            `json:"last_reload_error,omitempty"`
	LastSampleSeed    int64             `json:"last_sample_seed,omitempty"`
	LastSampleCount   int               `json:"last_sample_count,omitempty"`
	LastMismatchCount int               `json:"last_mismatch_count,omitempty"`
	SourcePath        string            `json:"source_path"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type ArtifactRef struct {
	ArtifactID  string `json:"artifact_id"`
	Kind        string `json:"kind"`
	StorageKind string `json:"storage_kind"`
	URI         string `json:"uri"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	PreviewText string `json:"preview_text"`
}

type Finding struct {
	FindingID string         `json:"finding_id"`
	RunID     string         `json:"run_id"`
	TaskID    string         `json:"task_id"`
	SampleID  string         `json:"sample_id"`
	Reason    string         `json:"reason"`
	Data      map[string]any `json:"data"`
}

type SampleResponse struct {
	Available   bool   `json:"available"`
	TaskID      string `json:"task_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Source      string `json:"source,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
	Data        any    `json:"data,omitempty"`
	DisplayData any    `json:"display_data,omitempty"`
	JQPrefix    string `json:"jq_prefix,omitempty"`
	JQResult    any    `json:"jq_result,omitempty"`
}

type RunStat struct {
	RunID      string    `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	RunStatus  string    `json:"run_status"`
}

type RunRecord struct {
	RunID                string            `json:"run_id"`
	TaskID               string            `json:"task_id"`
	TaskKind             string            `json:"task_kind"`
	PluginGenerationID   string            `json:"plugin_generation_id,omitempty"`
	TriggerType          string            `json:"trigger_type"`
	RunStatus            string            `json:"run_status"`
	CheckStatus          string            `json:"check_status"`
	StartedAt            time.Time         `json:"started_at"`
	EndedAt              time.Time         `json:"ended_at"`
	DurationMS           int64             `json:"duration_ms"`
	ErrorMessage         string            `json:"error_message,omitempty"`
	Summary              map[string]any    `json:"summary,omitempty"`
	Payload              json.RawMessage   `json:"payload,omitempty"`
	ArtifactRefs         []ArtifactRef     `json:"artifact_refs,omitempty"`
	PluginConfigVersions map[string]any    `json:"plugin_config_versions,omitempty"`
	PluginAssetVersions  map[string]any    `json:"plugin_asset_versions,omitempty"`
	PluginTaskOverrides  map[string]any    `json:"plugin_task_overrides,omitempty"`
	Findings             []Finding         `json:"findings,omitempty"`
	Stdout               string            `json:"stdout,omitempty"`
	Stderr               string            `json:"stderr,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
}

type RunListItem struct {
	RunID         string            `json:"run_id"`
	TaskID        string            `json:"task_id"`
	TaskName      string            `json:"task_name,omitempty"`
	TaskKind      string            `json:"task_kind"`
	TriggerType   string            `json:"trigger_type"`
	RunStatus     string            `json:"run_status"`
	CheckStatus   string            `json:"check_status"`
	StartedAt     time.Time         `json:"started_at"`
	EndedAt       time.Time         `json:"ended_at"`
	DurationMS    int64             `json:"duration_ms"`
	ErrorMessage  string            `json:"error_message,omitempty"`
	Summary       map[string]any    `json:"summary,omitempty"`
	HasPayload    bool              `json:"has_payload"`
	ArtifactCount int               `json:"artifact_count"`
	FindingCount  int               `json:"finding_count"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type PaginatedRuns struct {
	Records []RunListItem `json:"records"`
	Total   int           `json:"total"`
}

type RunQuery struct {
	TaskID      string
	Kind        string
	RunStatus   string
	CheckStatus string
	Since       time.Duration
	Limit       int
	Offset      int
	Labels      map[string]string
}

type Repository interface {
	Close() error
	UpsertTaskState(ctx context.Context, state TaskState) error
	DeleteTaskState(ctx context.Context, taskID string) error
	InsertRun(ctx context.Context, record RunRecord) error
	ListRuns(ctx context.Context, taskID string, limit, offset int, since time.Duration) ([]RunRecord, error)
	CountRuns(ctx context.Context, taskID string, since time.Duration) (int, error)
	ListRunItems(ctx context.Context, taskID string, limit, offset int, since time.Duration) ([]RunListItem, error)
	ListRunsAcrossTasks(ctx context.Context, query RunQuery) ([]RunListItem, int, error)
	ListConsecutiveFailures(ctx context.Context, taskIDs []string) (map[string]int, error)
	ListRunStats(ctx context.Context, taskID string, since time.Duration) ([]RunStat, error)
	GetRun(ctx context.Context, taskID, runID string) (RunRecord, error)
	ListArtifactsByRun(ctx context.Context, taskID, runID string) ([]ArtifactRef, error)
	GetArtifact(ctx context.Context, artifactID string) (ArtifactRef, error)
	InsertReloadFailure(ctx context.Context, taskID, sourcePath, message string) error
	InsertAIAnalysis(ctx context.Context, record AIAnalysisRecord) error
	GetAIAnalysis(ctx context.Context, runID string) (*AIAnalysisRecord, error)
	ListAIAnalyses(ctx context.Context, taskID string, limit int) ([]AIAnalysisRecord, error)
	GetMeta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error
	LoadGlobalSettings(ctx context.Context) (config.GlobalSettings, error)
	SaveGlobalSettings(ctx context.Context, s config.GlobalSettings) error
	LoadPlatformConfig(ctx context.Context) (config.PlatformConfigSummary, error)
	SavePlatformConfig(ctx context.Context, summary config.PlatformConfigSummary) error
	ListTaskDefinitions(ctx context.Context) ([]config.TaskDefinition, error)
	GetTaskDefinition(ctx context.Context, taskID string) (*config.TaskDefinition, error)
	InsertTaskDefinition(ctx context.Context, def config.TaskDefinition) error
	UpdateTaskDefinition(ctx context.Context, def config.TaskDefinition) error
	DeleteTaskDefinition(ctx context.Context, taskID string) error

	ListPipelines(ctx context.Context) ([]config.Pipeline, error)
	GetPipeline(ctx context.Context, id string) (*config.Pipeline, error)
	InsertPipeline(ctx context.Context, p config.Pipeline) error
	UpdatePipeline(ctx context.Context, p config.Pipeline) error
	DeletePipeline(ctx context.Context, id string) error
	ListTaskDefinitionsByPipeline(ctx context.Context, pipelineID string) ([]config.TaskDefinition, error)
	UpdateTaskPipeline(ctx context.Context, taskID string, pipelineID *string) error
	ListTaskDependencies(ctx context.Context) ([]config.TaskDependency, error)
	ListTaskDependenciesByPipeline(ctx context.Context, pipelineID string) ([]config.TaskDependency, error)
	ReplaceTaskDependencies(ctx context.Context, taskID string, dependencies []config.TaskDependency) error
	UpsertTaskDependency(ctx context.Context, dependency config.TaskDependency) (config.TaskDependency, error)
	DeleteTaskDependency(ctx context.Context, id string) error
}

type PostgresStore struct {
	db *sql.DB
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func OpenPostgres(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	store := &PostgresStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return store, nil
}

func (s *PostgresStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PostgresStore) EnsurePluginPackage(ctx context.Context, record pluginmodel.PackageRecord) error {
	if record.Status == "" {
		record.Status = pluginmodel.PackageStatusDisabled
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_packages (
			id, name, description, author, homepage, official, bundled, status, last_error, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			description=excluded.description,
			author=excluded.author,
			homepage=excluded.homepage,
			official=excluded.official,
			bundled=excluded.bundled,
			updated_at=NOW()
	`, record.ID, record.Name, record.Description, record.Author, record.Homepage,
		record.Official, record.Bundled, record.Status, record.LastError)
	if err != nil {
		return fmt.Errorf("ensure plugin package: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertPluginRelease(ctx context.Context, record pluginmodel.ReleaseRecord) error {
	manifestJSON, err := json.Marshal(record.Manifest)
	if err != nil {
		return fmt.Errorf("marshal plugin manifest: %w", err)
	}
	if record.SchemaVersion == "" {
		record.SchemaVersion = record.Manifest.SchemaVersion
	}
	if record.Status == "" {
		record.Status = pluginmodel.ReleaseStatusStaged
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO plugin_releases (
			plugin_id, version, schema_version, manifest_json, path, status, checksum,
			validation_error, official, bundled, created_at, updated_at
		) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		ON CONFLICT(plugin_id, version) DO UPDATE SET
			schema_version=excluded.schema_version,
			manifest_json=excluded.manifest_json,
			path=excluded.path,
			status=CASE
				WHEN plugin_releases.status IN ('active', 'draining', 'retired') THEN plugin_releases.status
				ELSE excluded.status
			END,
			checksum=excluded.checksum,
			validation_error=excluded.validation_error,
			official=excluded.official,
			bundled=excluded.bundled,
			updated_at=NOW()
	`, record.PluginID, record.Version, record.SchemaVersion, string(manifestJSON), record.Path,
		record.Status, record.Checksum, record.ValidationError, record.Official, record.Bundled)
	if err != nil {
		return fmt.Errorf("upsert plugin release: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginPackages(ctx context.Context) ([]pluginmodel.PackageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, author, homepage, official, bundled, status, last_error, created_at, updated_at
		FROM plugin_packages
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list plugin packages: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.PackageRecord
	for rows.Next() {
		record, err := scanPluginPackage(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginPackage(ctx context.Context, pluginID string) (pluginmodel.PackageRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, author, homepage, official, bundled, status, last_error, created_at, updated_at
		FROM plugin_packages
		WHERE id = $1
	`, pluginID)
	record, err := scanPluginPackage(row)
	if err != nil {
		return pluginmodel.PackageRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) UpdatePluginPackageStatus(ctx context.Context, pluginID, status, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plugin_packages
		SET status = $2, last_error = $3, updated_at = NOW()
		WHERE id = $1
	`, pluginID, status, lastError)
	if err != nil {
		return fmt.Errorf("update plugin package status: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginReleases(ctx context.Context, pluginID string) ([]pluginmodel.ReleaseRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plugin_id, version, schema_version, manifest_json, path, status, checksum,
		       validation_error, official, bundled, created_at, updated_at, validated_at, activated_at
		FROM plugin_releases
		WHERE plugin_id = $1
		ORDER BY created_at DESC, version DESC
	`, pluginID)
	if err != nil {
		return nil, fmt.Errorf("list plugin releases: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.ReleaseRecord
	for rows.Next() {
		record, err := scanPluginRelease(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginRelease(ctx context.Context, pluginID, version string) (pluginmodel.ReleaseRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT plugin_id, version, schema_version, manifest_json, path, status, checksum,
		       validation_error, official, bundled, created_at, updated_at, validated_at, activated_at
		FROM plugin_releases
		WHERE plugin_id = $1 AND version = $2
	`, pluginID, version)
	record, err := scanPluginRelease(row)
	if err != nil {
		return pluginmodel.ReleaseRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) UpdatePluginReleaseStatus(ctx context.Context, pluginID, version, status, validationError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plugin_releases
		SET status = $3,
		    validation_error = $4,
		    validated_at = CASE WHEN $3 = 'validated' THEN NOW() ELSE validated_at END,
		    activated_at = CASE WHEN $3 = 'active' THEN NOW() ELSE activated_at END,
		    updated_at = NOW()
		WHERE plugin_id = $1 AND version = $2
	`, pluginID, version, status, validationError)
	if err != nil {
		return fmt.Errorf("update plugin release status: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetActivePluginVersion(ctx context.Context, pluginID, version, generationID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_active_versions(plugin_id, version, generation_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT(plugin_id) DO UPDATE SET
			version=excluded.version,
			generation_id=excluded.generation_id,
			updated_at=excluded.updated_at
	`, pluginID, version, generationID)
	if err != nil {
		return fmt.Errorf("set active plugin version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetActivePluginVersions(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT plugin_id, version FROM plugin_active_versions`)
	if err != nil {
		return nil, fmt.Errorf("get active plugin versions: %w", err)
	}
	defer rows.Close()
	versions := map[string]string{}
	for rows.Next() {
		var pluginID, version string
		if err := rows.Scan(&pluginID, &version); err != nil {
			return nil, err
		}
		versions[pluginID] = version
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func (s *PostgresStore) InsertPluginGeneration(ctx context.Context, record pluginmodel.GenerationRecord) error {
	return insertPluginGeneration(ctx, s.db, record)
}

func insertPluginGeneration(ctx context.Context, exec sqlExecutor, record pluginmodel.GenerationRecord) error {
	activeJSON, err := json.Marshal(record.ActiveVersions)
	if err != nil {
		return fmt.Errorf("marshal plugin generation active versions: %w", err)
	}
	capabilitiesJSON, err := json.Marshal(record.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal plugin generation capabilities: %w", err)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	_, err = exec.ExecContext(ctx, `
			INSERT INTO plugin_generations (
				generation_id, status, active_versions_json, capabilities_json, created_at, retired_at
			) VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6)
		ON CONFLICT(generation_id) DO NOTHING
	`, record.ID, record.Status, string(activeJSON), string(capabilitiesJSON), record.CreatedAt, record.RetiredAt)
	if err != nil {
		return fmt.Errorf("insert plugin generation: %w", err)
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO plugin_generation_refs(generation_id, ref_count, last_released_at, updated_at)
		VALUES ($1, 0, NOW(), NOW())
		ON CONFLICT(generation_id) DO NOTHING
	`, record.ID)
	if err != nil {
		return fmt.Errorf("insert plugin generation refs: %w", err)
	}
	return nil
}

func (s *PostgresStore) AcquirePluginGeneration(ctx context.Context, generationID string) error {
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_generation_refs(generation_id, ref_count, updated_at)
		VALUES ($1, 1, NOW())
		ON CONFLICT(generation_id) DO UPDATE
		SET ref_count = plugin_generation_refs.ref_count + 1,
		    updated_at = NOW()
	`, generationID)
	if err != nil {
		return fmt.Errorf("acquire plugin generation %s: %w", generationID, err)
	}
	return nil
}

func (s *PostgresStore) ReleasePluginGeneration(ctx context.Context, generationID string) error {
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE plugin_generation_refs
		SET ref_count = GREATEST(ref_count - 1, 0),
		    last_released_at = CASE WHEN ref_count <= 1 THEN NOW() ELSE last_released_at END,
		    updated_at = NOW()
		WHERE generation_id = $1
	`, generationID)
	if err != nil {
		return fmt.Errorf("release plugin generation %s: %w", generationID, err)
	}
	return nil
}

func (s *PostgresStore) PluginReleaseProtected(ctx context.Context, pluginID, version string, retention time.Duration) (bool, error) {
	if strings.TrimSpace(pluginID) == "" || strings.TrimSpace(version) == "" {
		return true, nil
	}
	var protected bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM plugin_generations g
			WHERE g.active_versions_json ->> $1 = $2
		)
	`, pluginID, version).Scan(&protected)
	if err != nil {
		return false, fmt.Errorf("check plugin release protection %s@%s: %w", pluginID, version, err)
	}
	return protected, nil
}

func (s *PostgresStore) DeleteExpiredPluginGenerations(ctx context.Context, activeGenerationID string, retention time.Duration) (int, error) {
	if retention < 0 {
		retention = 0
	}
	cutoff := time.Now().Add(-retention)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM plugin_generations g
		USING plugin_generation_refs r
		WHERE r.generation_id = g.generation_id
		  AND g.generation_id <> $1
		  AND r.ref_count = 0
		  AND r.last_released_at IS NOT NULL
		  AND r.last_released_at <= $2
	`, activeGenerationID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired plugin generations: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted plugin generations: %w", err)
	}
	return int(affected), nil
}

func (s *PostgresStore) InsertPluginEvent(ctx context.Context, record pluginmodel.EventRecord) error {
	return insertPluginEvent(ctx, s.db, record)
}

func (s *PostgresStore) UpsertPluginConfigInstance(ctx context.Context, record pluginmodel.ConfigInstanceRecord) error {
	if record.Scope == "" {
		record.Scope = "plugin"
	}
	if record.Status == "" {
		record.Status = "draft"
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_config_instances (
			id, plugin_id, capability_id, capability_type, capability_name,
			scope, title, status, active_version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT(id) DO UPDATE SET
			plugin_id=excluded.plugin_id,
			capability_id=excluded.capability_id,
			capability_type=excluded.capability_type,
			capability_name=excluded.capability_name,
			scope=excluded.scope,
			title=excluded.title,
			status=excluded.status,
			active_version=excluded.active_version,
			updated_at=NOW()
	`, record.ID, record.PluginID, record.CapabilityID, record.CapabilityType, record.CapabilityName,
		record.Scope, record.Title, record.Status, record.ActiveVersion); err != nil {
		return fmt.Errorf("upsert plugin config instance: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdatePluginConfigInstanceStatus(ctx context.Context, instanceID, status string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE plugin_config_instances
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`, instanceID, status)
	if err != nil {
		return fmt.Errorf("update plugin config instance status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PostgresStore) UpsertPluginConfigVersion(ctx context.Context, record pluginmodel.ConfigVersionRecord) error {
	if record.Status == "" {
		record.Status = "draft"
	}
	valuesJSON, err := marshalJSONBytes(record.Values)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_config_versions (
			instance_id, version, status, values_json, validation_error,
			created_at, updated_at, validated_at, activated_at, retired_at
		) VALUES ($1, $2, $3, $4::jsonb, $5, NOW(), NOW(), $6, $7, $8)
		ON CONFLICT(instance_id, version) DO UPDATE SET
			status=excluded.status,
			values_json=excluded.values_json,
			validation_error=excluded.validation_error,
			updated_at=NOW(),
			validated_at=excluded.validated_at,
			activated_at=excluded.activated_at,
			retired_at=excluded.retired_at
	`, record.InstanceID, record.Version, record.Status, string(valuesJSON), record.ValidationError,
		record.ValidatedAt, record.ActivatedAt, record.RetiredAt); err != nil {
		return fmt.Errorf("upsert plugin config version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ActivatePluginConfigVersion(ctx context.Context, instanceID string, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin activate plugin config version: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE plugin_config_versions
		SET status = 'retired', retired_at = NOW(), updated_at = NOW()
		WHERE instance_id = $1 AND status = 'active' AND version <> $2
	`, instanceID, version); err != nil {
		return fmt.Errorf("retire previous plugin config versions: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE plugin_config_versions
		SET status = 'active', activated_at = NOW(), updated_at = NOW()
		WHERE instance_id = $1 AND version = $2
	`, instanceID, version)
	if err != nil {
		return fmt.Errorf("activate plugin config version: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE plugin_config_instances
		SET status = 'active', active_version = $2, updated_at = NOW()
		WHERE id = $1
	`, instanceID, version); err != nil {
		return fmt.Errorf("update plugin config instance active version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit activate plugin config version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginConfigInstances(ctx context.Context, pluginID, capabilityID string) ([]pluginmodel.ConfigInstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plugin_id, capability_id, capability_type, capability_name,
		       scope, title, status, active_version, created_at, updated_at
		FROM plugin_config_instances
		WHERE ($1 = '' OR plugin_id = $1)
		  AND ($2 = '' OR capability_id = $2)
		ORDER BY plugin_id ASC, capability_id ASC, title ASC, id ASC
	`, pluginID, capabilityID)
	if err != nil {
		return nil, fmt.Errorf("list plugin config instances: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.ConfigInstanceRecord
	for rows.Next() {
		record, err := scanPluginConfigInstance(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginConfigInstance(ctx context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plugin_id, capability_id, capability_type, capability_name,
		       scope, title, status, active_version, created_at, updated_at
		FROM plugin_config_instances
		WHERE id = $1
	`, instanceID)
	record, err := scanPluginConfigInstance(row)
	if err != nil {
		return pluginmodel.ConfigInstanceRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) ListPluginConfigVersions(ctx context.Context, instanceID string) ([]pluginmodel.ConfigVersionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT instance_id, version, status, values_json, validation_error,
		       created_at, updated_at, validated_at, activated_at, retired_at
		FROM plugin_config_versions
		WHERE instance_id = $1
		ORDER BY version DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list plugin config versions: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.ConfigVersionRecord
	for rows.Next() {
		record, err := scanPluginConfigVersion(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginConfigVersion(ctx context.Context, instanceID string, version int) (pluginmodel.ConfigVersionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT instance_id, version, status, values_json, validation_error,
		       created_at, updated_at, validated_at, activated_at, retired_at
		FROM plugin_config_versions
		WHERE instance_id = $1 AND version = $2
	`, instanceID, version)
	record, err := scanPluginConfigVersion(row)
	if err != nil {
		return pluginmodel.ConfigVersionRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) GetActivePluginConfigVersion(ctx context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT v.instance_id, v.version, v.status, v.values_json, v.validation_error,
		       v.created_at, v.updated_at, v.validated_at, v.activated_at, v.retired_at
		FROM plugin_config_instances i
		JOIN plugin_config_versions v ON v.instance_id = i.id AND v.version = i.active_version
		WHERE i.id = $1
	`, instanceID)
	record, err := scanPluginConfigVersion(row)
	if err != nil {
		return pluginmodel.ConfigVersionRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) UpsertPluginAsset(ctx context.Context, record pluginmodel.AssetRecord) error {
	if record.Status == "" {
		record.Status = "draft"
	}
	if record.Scope == "" {
		record.Scope = pluginmodel.AssetScopeCapabilityShared
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_assets (
			id, plugin_id, capability_id, config_instance_id, scope, kind, title, status, active_version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT(id) DO UPDATE SET
			plugin_id=excluded.plugin_id,
			capability_id=excluded.capability_id,
			config_instance_id=excluded.config_instance_id,
			scope=excluded.scope,
			kind=excluded.kind,
			title=excluded.title,
			status=excluded.status,
			active_version=excluded.active_version,
			updated_at=NOW()
	`, record.ID, record.PluginID, record.CapabilityID, record.ConfigInstanceID, record.Scope, record.Kind, record.Title, record.Status, record.ActiveVersion); err != nil {
		return fmt.Errorf("upsert plugin asset: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertPluginAssetVersion(ctx context.Context, record pluginmodel.AssetVersionRecord) error {
	if record.Status == "" {
		record.Status = "draft"
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_asset_versions (
			asset_id, version, status, filename, content_type, storage_uri, content, size_bytes,
			checksum, validation_error, created_at, updated_at, validated_at, activated_at, retired_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW(), $11, $12, $13)
		ON CONFLICT(asset_id, version) DO UPDATE SET
			status=excluded.status,
			filename=excluded.filename,
			content_type=excluded.content_type,
			storage_uri=excluded.storage_uri,
			content=excluded.content,
			size_bytes=excluded.size_bytes,
			checksum=excluded.checksum,
			validation_error=excluded.validation_error,
			updated_at=NOW(),
			validated_at=excluded.validated_at,
			activated_at=excluded.activated_at,
			retired_at=excluded.retired_at
	`, record.AssetID, record.Version, record.Status, record.Filename, record.ContentType,
		record.StorageURI, record.Content, record.SizeBytes, record.Checksum, record.ValidationError,
		record.ValidatedAt, record.ActivatedAt, record.RetiredAt); err != nil {
		return fmt.Errorf("upsert plugin asset version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ActivatePluginAssetVersion(ctx context.Context, assetID string, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin activate plugin asset version: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE plugin_asset_versions
		SET status = 'retired', retired_at = NOW(), updated_at = NOW()
		WHERE asset_id = $1 AND status = 'active' AND version <> $2
	`, assetID, version); err != nil {
		return fmt.Errorf("retire previous plugin asset versions: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE plugin_asset_versions
		SET status = 'active', activated_at = NOW(), updated_at = NOW()
		WHERE asset_id = $1 AND version = $2
	`, assetID, version)
	if err != nil {
		return fmt.Errorf("activate plugin asset version: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE plugin_assets
		SET status = 'active', active_version = $2, updated_at = NOW()
		WHERE id = $1
	`, assetID, version); err != nil {
		return fmt.Errorf("update plugin asset active version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit activate plugin asset version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginAssets(ctx context.Context, pluginID, capabilityID, configInstanceID, scope, kind string) ([]pluginmodel.AssetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plugin_id, capability_id, config_instance_id, scope, kind, title, status, active_version, created_at, updated_at
		FROM plugin_assets
		WHERE ($1 = '' OR plugin_id = $1)
		  AND ($2 = '' OR capability_id = $2)
		  AND ($3 = '' OR config_instance_id = $3)
		  AND ($4 = '' OR scope = $4)
		  AND ($5 = '' OR kind = $5)
		ORDER BY plugin_id ASC, capability_id ASC, config_instance_id ASC, scope ASC, kind ASC, title ASC, id ASC
	`, pluginID, capabilityID, configInstanceID, scope, kind)
	if err != nil {
		return nil, fmt.Errorf("list plugin assets: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.AssetRecord
	for rows.Next() {
		record, err := scanPluginAsset(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginAsset(ctx context.Context, assetID string) (pluginmodel.AssetRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plugin_id, capability_id, config_instance_id, scope, kind, title, status, active_version, created_at, updated_at
		FROM plugin_assets
		WHERE id = $1
	`, assetID)
	record, err := scanPluginAsset(row)
	if err != nil {
		return pluginmodel.AssetRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) ListPluginAssetVersions(ctx context.Context, assetID string) ([]pluginmodel.AssetVersionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT asset_id, version, status, filename, content_type, storage_uri, content, size_bytes,
		       checksum, validation_error, created_at, updated_at, validated_at, activated_at, retired_at
		FROM plugin_asset_versions
		WHERE asset_id = $1
		ORDER BY version DESC
	`, assetID)
	if err != nil {
		return nil, fmt.Errorf("list plugin asset versions: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.AssetVersionRecord
	for rows.Next() {
		record, err := scanPluginAssetVersion(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginAssetVersion(ctx context.Context, assetID string, version int) (pluginmodel.AssetVersionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT asset_id, version, status, filename, content_type, storage_uri, content, size_bytes,
		       checksum, validation_error, created_at, updated_at, validated_at, activated_at, retired_at
		FROM plugin_asset_versions
		WHERE asset_id = $1 AND version = $2
	`, assetID, version)
	record, err := scanPluginAssetVersion(row)
	if err != nil {
		return pluginmodel.AssetVersionRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) GetActivePluginAssetVersion(ctx context.Context, assetID string) (pluginmodel.AssetVersionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT v.asset_id, v.version, v.status, v.filename, v.content_type, v.storage_uri, v.content, v.size_bytes,
		       v.checksum, v.validation_error, v.created_at, v.updated_at, v.validated_at, v.activated_at, v.retired_at
		FROM plugin_assets a
		JOIN plugin_asset_versions v ON v.asset_id = a.id AND v.version = a.active_version
		WHERE a.id = $1
	`, assetID)
	record, err := scanPluginAssetVersion(row)
	if err != nil {
		return pluginmodel.AssetVersionRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) UpsertPluginSecret(ctx context.Context, secret pluginmodel.SecretRecord, value pluginmodel.SecretValueRecord) error {
	if secret.Status == "" {
		secret.Status = "active"
	}
	metaJSON, err := marshalJSONBytes(value.EncryptionMeta)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert plugin secret: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plugin_secrets (
			id, plugin_id, scope, title, masked, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT(id) DO UPDATE SET
			plugin_id=excluded.plugin_id,
			scope=excluded.scope,
			title=excluded.title,
			masked=excluded.masked,
			status=excluded.status,
			updated_at=NOW()
	`, secret.ID, secret.PluginID, secret.Scope, secret.Title, secret.Masked, secret.Status); err != nil {
		return fmt.Errorf("upsert plugin secret: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO plugin_secret_values(secret_id, ciphertext, encryption_meta_json, updated_at)
		VALUES ($1, $2, $3::jsonb, NOW())
		ON CONFLICT(secret_id) DO UPDATE SET
			ciphertext=excluded.ciphertext,
			encryption_meta_json=excluded.encryption_meta_json,
			updated_at=NOW()
	`, secret.ID, value.Ciphertext, string(metaJSON)); err != nil {
		return fmt.Errorf("upsert plugin secret value: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert plugin secret: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginSecrets(ctx context.Context, pluginID, scope string) ([]pluginmodel.SecretRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plugin_id, scope, title, masked, status, created_at, updated_at
		FROM plugin_secrets
		WHERE ($1 = '' OR plugin_id = $1)
		  AND ($2 = '' OR scope = $2)
		ORDER BY plugin_id ASC, scope ASC, title ASC, id ASC
	`, pluginID, scope)
	if err != nil {
		return nil, fmt.Errorf("list plugin secrets: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.SecretRecord
	for rows.Next() {
		record, err := scanPluginSecret(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetPluginSecret(ctx context.Context, secretID string) (pluginmodel.SecretRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plugin_id, scope, title, masked, status, created_at, updated_at
		FROM plugin_secrets
		WHERE id = $1
	`, secretID)
	record, err := scanPluginSecret(row)
	if err != nil {
		return pluginmodel.SecretRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) GetPluginSecretValue(ctx context.Context, secretID string) (pluginmodel.SecretValueRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT secret_id, ciphertext, encryption_meta_json, updated_at
		FROM plugin_secret_values
		WHERE secret_id = $1
	`, secretID)
	record, err := scanPluginSecretValue(row)
	if err != nil {
		return pluginmodel.SecretValueRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) InsertPluginConfigEvent(ctx context.Context, record pluginmodel.ConfigEventRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_config_events(resource_type, resource_id, plugin_id, action, status, message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, record.ResourceType, record.ResourceID, record.PluginID, record.Action, record.Status, record.Message, record.CreatedAt); err != nil {
		return fmt.Errorf("insert plugin config event: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListPluginConfigEvents(ctx context.Context, pluginID, resourceType, resourceID string, limit int) ([]pluginmodel.ConfigEventRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, resource_type, resource_id, plugin_id, action, status, message, created_at
		FROM plugin_config_events
		WHERE ($1 = '' OR plugin_id = $1)
		  AND ($2 = '' OR resource_type = $2)
		  AND ($3 = '' OR resource_id = $3)
		ORDER BY created_at DESC, id DESC
		LIMIT $4
	`, pluginID, resourceType, resourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list plugin config events: %w", err)
	}
	defer rows.Close()
	var records []pluginmodel.ConfigEventRecord
	for rows.Next() {
		record, err := scanPluginConfigEvent(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plugin config events: %w", err)
	}
	return records, nil
}

func insertPluginEvent(ctx context.Context, exec sqlExecutor, record pluginmodel.EventRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	_, err := exec.ExecContext(ctx, `
			INSERT INTO plugin_events(plugin_id, version, action, status, message, generation_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, record.PluginID, record.Version, record.Action, record.Status, record.Message, record.GenerationID, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert plugin event: %w", err)
	}
	return nil
}

func (s *PostgresStore) CommitPluginGeneration(ctx context.Context, commit pluginmodel.GenerationCommit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin plugin generation commit: %w", err)
	}
	defer tx.Rollback()

	if commit.SetActiveVersion {
		if err := setActivePluginVersionCAS(ctx, tx, commit.PackageID, commit.ExpectedActiveVersion, commit.ActiveVersion, commit.Generation.ID); err != nil {
			return err
		}
	}
	if commit.PackageID != "" && commit.PackageStatus != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE plugin_packages
			SET status = $2, last_error = $3, updated_at = NOW()
			WHERE id = $1
		`, commit.PackageID, commit.PackageStatus, commit.PackageLastError); err != nil {
			return fmt.Errorf("update plugin package status: %w", err)
		}
	}
	if commit.DrainingVersion != "" && commit.DrainingVersion != commit.ActiveReleaseVersion {
		if _, err := tx.ExecContext(ctx, `
			UPDATE plugin_releases
			SET status = 'draining', validation_error = '', updated_at = NOW()
			WHERE plugin_id = $1 AND version = $2
		`, commit.PackageID, commit.DrainingVersion); err != nil {
			return fmt.Errorf("mark plugin release draining: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE plugin_generation_refs r
			SET last_released_at = NOW(),
			    updated_at = NOW()
			FROM plugin_generations g
			WHERE g.generation_id = r.generation_id
			  AND g.active_versions_json ->> $1 = $2
			  AND g.generation_id <> $3
			  AND r.ref_count = 0
		`, commit.PackageID, commit.DrainingVersion, commit.Generation.ID); err != nil {
			return fmt.Errorf("mark draining plugin generation release time: %w", err)
		}
	}
	if commit.ActiveReleaseVersion != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE plugin_releases
			SET status = 'active', validation_error = '', activated_at = NOW(), updated_at = NOW()
			WHERE plugin_id = $1 AND version = $2
		`, commit.PackageID, commit.ActiveReleaseVersion); err != nil {
			return fmt.Errorf("mark plugin release active: %w", err)
		}
	}
	if err := insertPluginGeneration(ctx, tx, commit.Generation); err != nil {
		return err
	}
	if commit.Event.Action != "" {
		if commit.Event.GenerationID == "" {
			commit.Event.GenerationID = commit.Generation.ID
		}
		if err := insertPluginEvent(ctx, tx, commit.Event); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit plugin generation: %w", err)
	}
	return nil
}

func setActivePluginVersionCAS(ctx context.Context, exec sqlExecutor, pluginID, expectedVersion, version, generationID string) error {
	var affected int
	err := exec.QueryRowContext(ctx, `
		WITH changed AS (
			INSERT INTO plugin_active_versions(plugin_id, version, generation_id, updated_at)
			VALUES ($1, $3, $4, NOW())
			ON CONFLICT(plugin_id) DO UPDATE SET
				version = EXCLUDED.version,
				generation_id = EXCLUDED.generation_id,
				updated_at = EXCLUDED.updated_at
			WHERE plugin_active_versions.version = $2
			RETURNING 1
		)
		SELECT COUNT(*) FROM changed
	`, pluginID, expectedVersion, version, generationID).Scan(&affected)
	if err != nil {
		return fmt.Errorf("cas active plugin version: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("active plugin version changed for %s", pluginID)
	}
	return nil
}

func (s *PostgresStore) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv_metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init postgres schema: %w", err)
		}
	}
	return runMigrations(s.db)
}

func (s *PostgresStore) UpsertTaskState(ctx context.Context, state TaskState) error {
	state.UpdatedAt = time.Now()
	labelsJSON, err := marshalJSONBytes(state.Labels)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO task_runtime_state (
			task_id, name, kind, enabled, status, labels_json, last_run_at, next_run_at,
			last_run_status, last_check_status, last_error, last_duration_ms, last_reload_error,
			last_sample_seed, last_sample_count, last_mismatch_count, source_path, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT(task_id) DO UPDATE SET
			name=excluded.name,
			kind=excluded.kind,
			enabled=excluded.enabled,
			status=excluded.status,
			labels_json=excluded.labels_json,
			last_run_at=excluded.last_run_at,
			next_run_at=excluded.next_run_at,
			last_run_status=excluded.last_run_status,
			last_check_status=excluded.last_check_status,
			last_error=excluded.last_error,
			last_duration_ms=excluded.last_duration_ms,
			last_reload_error=excluded.last_reload_error,
			last_sample_seed=excluded.last_sample_seed,
			last_sample_count=excluded.last_sample_count,
			last_mismatch_count=excluded.last_mismatch_count,
			source_path=excluded.source_path,
			updated_at=excluded.updated_at
	`, state.TaskID, state.Name, state.Kind, state.Enabled, state.Status, string(labelsJSON),
		state.LastRunAt, state.NextRunAt,
		state.LastRunStatus, state.LastCheckStatus, state.LastError, state.LastDurationMS,
		state.LastReloadError, state.LastSampleSeed, state.LastSampleCount, state.LastMismatchCount,
		state.SourcePath, state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert task state: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteTaskState(ctx context.Context, taskID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM task_runtime_state WHERE task_id = $1`, taskID); err != nil {
		return fmt.Errorf("delete task state: %w", err)
	}
	return nil
}

func (s *PostgresStore) InsertRun(ctx context.Context, record RunRecord) error {
	summaryJSON, err := marshalJSONBytes(record.Summary)
	if err != nil {
		return err
	}
	labelsJSON, err := marshalJSONBytes(record.Labels)
	if err != nil {
		return err
	}
	configVersionsJSON, err := marshalJSONBytes(record.PluginConfigVersions)
	if err != nil {
		return err
	}
	assetVersionsJSON, err := marshalJSONBytes(record.PluginAssetVersions)
	if err != nil {
		return err
	}
	taskOverridesJSON, err := marshalJSONBytes(record.PluginTaskOverrides)
	if err != nil {
		return err
	}
	var payload any
	if len(record.Payload) > 0 {
		payload = string(record.Payload)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin run transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (
			run_id, task_id, task_kind, plugin_generation_id, trigger_type, run_status, check_status,
			started_at, ended_at, duration_ms, error_message, summary_json, payload,
			stdout, stderr, labels_json, plugin_config_versions_json, plugin_asset_versions_json,
			plugin_task_overrides_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15, $16::jsonb, $17::jsonb, $18::jsonb, $19::jsonb)
	`, record.RunID, record.TaskID, record.TaskKind, record.PluginGenerationID, record.TriggerType, record.RunStatus,
		record.CheckStatus, record.StartedAt, record.EndedAt, record.DurationMS, record.ErrorMessage,
		string(summaryJSON), payload, record.Stdout, record.Stderr, string(labelsJSON),
		string(configVersionsJSON), string(assetVersionsJSON), string(taskOverridesJSON))
	if err != nil {
		return fmt.Errorf("insert run record: %w", err)
	}
	for _, finding := range record.Findings {
		if finding.FindingID == "" {
			finding.FindingID = uuid.NewString()
		}
		if finding.RunID == "" {
			finding.RunID = record.RunID
		}
		if finding.TaskID == "" {
			finding.TaskID = record.TaskID
		}
		dataJSON, marshalErr := marshalJSONBytes(finding.Data)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO findings (finding_id, run_id, task_id, sample_id, reason, data_json, created_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		`, finding.FindingID, finding.RunID, finding.TaskID, finding.SampleID, finding.Reason, string(dataJSON), time.Now()); err != nil {
			return fmt.Errorf("insert finding: %w", err)
		}
	}
	for _, artifact := range record.ArtifactRefs {
		if artifact.ArtifactID == "" {
			artifact.ArtifactID = uuid.NewString()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO artifacts (
				artifact_id, run_id, task_id, kind, storage_kind, uri, content_type,
				size_bytes, sha256, preview_text, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, artifact.ArtifactID, record.RunID, record.TaskID, artifact.Kind, artifact.StorageKind,
			artifact.URI, artifact.ContentType, artifact.SizeBytes, artifact.SHA256, artifact.PreviewText, time.Now()); err != nil {
			return fmt.Errorf("insert artifact: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run transaction: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListRuns(ctx context.Context, taskID string, limit, offset int, since time.Duration) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	if since > 0 {
		cutoff := time.Now().UTC().Add(-since)
		rows, err := s.db.QueryContext(ctx, `
			SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
			       started_at, ended_at, duration_ms, error_message, summary_json, payload,
			       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
			       plugin_asset_versions_json, plugin_task_overrides_json
			FROM runs
			WHERE task_id = $1 AND started_at >= $3
			ORDER BY started_at DESC
			LIMIT $2 OFFSET $4
		`, taskID, limit, cutoff, offset)
		if err != nil {
			return nil, fmt.Errorf("list runs: %w", err)
		}
		defer rows.Close()
		var records []RunRecord
		for rows.Next() {
			record, err := scanRunRecord(rows)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := range records {
			artifacts, err := s.ListArtifactsByRun(ctx, taskID, records[i].RunID)
			if err != nil {
				return nil, err
			}
			records[i].ArtifactRefs = artifacts
		}
		return records, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
		       plugin_asset_versions_json, plugin_task_overrides_json
		FROM runs
		WHERE task_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3
	`, taskID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var records []RunRecord
	for rows.Next() {
		record, err := scanRunRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range records {
		artifacts, err := s.ListArtifactsByRun(ctx, taskID, records[i].RunID)
		if err != nil {
			return nil, err
		}
		records[i].ArtifactRefs = artifacts
	}
	return records, nil
}

func (s *PostgresStore) ListRunItems(ctx context.Context, taskID string, limit, offset int, since time.Duration) ([]RunListItem, error) {
	query := RunQuery{TaskID: taskID, Limit: limit, Offset: offset, Since: since}
	items, _, err := s.ListRunsAcrossTasks(ctx, query)
	return items, err
}

func (s *PostgresStore) ListRunsAcrossTasks(ctx context.Context, query RunQuery) ([]RunListItem, int, error) {
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Offset < 0 {
		query.Offset = 0
	}

	where, args := buildRunQueryWhere(query)
	countSQL := `SELECT COUNT(*) FROM runs r ` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs across tasks: %w", err)
	}

	args = append(args, query.Limit, query.Offset)
	limitIndex := len(args) - 1
	offsetIndex := len(args)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT r.run_id, r.task_id, COALESCE(td.name, ''), r.task_kind, r.trigger_type,
		       r.run_status, r.check_status, r.started_at, r.ended_at, r.duration_ms,
		       r.error_message, r.summary_json, r.payload IS NOT NULL,
		       COALESCE(a.artifact_count, 0), COALESCE(f.finding_count, 0), r.labels_json
		FROM runs r
		LEFT JOIN task_definitions td ON td.task_id = r.task_id
		LEFT JOIN (
			SELECT run_id, COUNT(*) AS artifact_count
			FROM artifacts
			GROUP BY run_id
		) a ON a.run_id = r.run_id
		LEFT JOIN (
			SELECT run_id, COUNT(*) AS finding_count
			FROM findings
			GROUP BY run_id
		) f ON f.run_id = r.run_id
		%s
		ORDER BY r.started_at DESC
		LIMIT $%d OFFSET $%d
	`, where, limitIndex, offsetIndex), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs across tasks: %w", err)
	}
	defer rows.Close()
	var items []RunListItem
	for rows.Next() {
		item, err := scanRunListItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStore) CountRuns(ctx context.Context, taskID string, since time.Duration) (int, error) {
	if since > 0 {
		cutoff := time.Now().UTC().Add(-since)
		var n int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM runs WHERE task_id = $1 AND started_at >= $2`,
			taskID, cutoff).Scan(&n)
		return n, err
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM runs WHERE task_id = $1`,
		taskID).Scan(&n)
	return n, err
}

func (s *PostgresStore) ListConsecutiveFailures(ctx context.Context, taskIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(taskIDs))
	for _, taskID := range taskIDs {
		result[taskID] = 0
	}
	if len(taskIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, 0, len(taskIDs))
	args := make([]any, 0, len(taskIDs))
	for i, taskID := range taskIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, taskID)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT task_id, run_status, check_status
		FROM runs
		WHERE task_id IN (%s)
		ORDER BY task_id ASC, started_at DESC
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("list consecutive failures: %w", err)
	}
	defer rows.Close()
	seenSuccess := map[string]bool{}
	for rows.Next() {
		var taskID, runStatus, checkStatus string
		if err := rows.Scan(&taskID, &runStatus, &checkStatus); err != nil {
			return nil, fmt.Errorf("scan consecutive failures: %w", err)
		}
		if seenSuccess[taskID] {
			continue
		}
		if runStatus == "failed" || runStatus == "timeout" || checkStatus == "fail" {
			result[taskID]++
			continue
		}
		seenSuccess[taskID] = true
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListRunStats(ctx context.Context, taskID string, since time.Duration) ([]RunStat, error) {
	if since > 0 {
		cutoff := time.Now().UTC().Add(-since)
		rows, err := s.db.QueryContext(ctx,
			`SELECT run_id, started_at, duration_ms, run_status FROM runs
			 WHERE task_id = $1 AND started_at >= $2
			 ORDER BY started_at DESC`,
			taskID, cutoff)
		if err != nil {
			return nil, fmt.Errorf("list run stats: %w", err)
		}
		defer rows.Close()
		var stats []RunStat
		for rows.Next() {
			var s RunStat
			if err := rows.Scan(&s.RunID, &s.StartedAt, &s.DurationMS, &s.RunStatus); err != nil {
				return nil, err
			}
			stats = append(stats, s)
		}
		return stats, rows.Err()
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT run_id, started_at, duration_ms, run_status FROM runs
		 WHERE task_id = $1
		 ORDER BY started_at DESC`,
		taskID)
	if err != nil {
		return nil, fmt.Errorf("list run stats: %w", err)
	}
	defer rows.Close()
	var stats []RunStat
	for rows.Next() {
		var s RunStat
		if err := rows.Scan(&s.RunID, &s.StartedAt, &s.DurationMS, &s.RunStatus); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (s *PostgresStore) GetRun(ctx context.Context, taskID, runID string) (RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, task_id, task_kind, trigger_type, run_status, check_status,
		       started_at, ended_at, duration_ms, error_message, summary_json, payload,
		       stdout, stderr, labels_json, plugin_generation_id, plugin_config_versions_json,
		       plugin_asset_versions_json, plugin_task_overrides_json
		FROM runs
		WHERE task_id = $1 AND run_id = $2
	`, taskID, runID)
	record, err := scanRunRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return RunRecord{}, err
		}
		return RunRecord{}, fmt.Errorf("get run: %w", err)
	}
	record.Findings, err = s.listFindings(ctx, runID)
	if err != nil {
		return RunRecord{}, err
	}
	record.ArtifactRefs, err = s.ListArtifactsByRun(ctx, taskID, runID)
	if err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func (s *PostgresStore) ListArtifactsByRun(ctx context.Context, taskID, runID string) ([]ArtifactRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT artifact_id, kind, storage_kind, uri, content_type, size_bytes, sha256, preview_text
		FROM artifacts
		WHERE task_id = $1 AND run_id = $2
		ORDER BY created_at ASC
	`, taskID, runID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	var artifacts []ArtifactRef
	for rows.Next() {
		var ref ArtifactRef
		if err := rows.Scan(
			&ref.ArtifactID, &ref.Kind, &ref.StorageKind, &ref.URI, &ref.ContentType,
			&ref.SizeBytes, &ref.SHA256, &ref.PreviewText,
		); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		artifacts = append(artifacts, ref)
	}
	return artifacts, rows.Err()
}

func (s *PostgresStore) GetArtifact(ctx context.Context, artifactID string) (ArtifactRef, error) {
	var ref ArtifactRef
	err := s.db.QueryRowContext(ctx, `
		SELECT artifact_id, kind, storage_kind, uri, content_type, size_bytes, sha256, preview_text
		FROM artifacts
		WHERE artifact_id = $1
	`, artifactID).Scan(
		&ref.ArtifactID, &ref.Kind, &ref.StorageKind, &ref.URI, &ref.ContentType,
		&ref.SizeBytes, &ref.SHA256, &ref.PreviewText,
	)
	if err != nil {
		return ArtifactRef{}, err
	}
	return ref, nil
}

func (s *PostgresStore) InsertReloadFailure(ctx context.Context, taskID, sourcePath, message string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_reload_failures(task_id, source_path, error_message, created_at)
		VALUES ($1, $2, $3, $4)
	`, taskID, sourcePath, message, time.Now())
	if err != nil {
		return fmt.Errorf("insert reload failure: %w", err)
	}
	return nil
}

type AIAnalysisRecord struct {
	ID           int64     `json:"id"`
	RunID        string    `json:"run_id"`
	TaskID       string    `json:"task_id"`
	AnalysisType string    `json:"analysis_type"`
	Model        string    `json:"model"`
	Prompt       string    `json:"prompt"`
	Response     string    `json:"response"`
	TokensIn     int       `json:"tokens_in"`
	TokensOut    int       `json:"tokens_out"`
	DurationMS   int64     `json:"duration_ms"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *PostgresStore) InsertAIAnalysis(ctx context.Context, record AIAnalysisRecord) error {
	if record.RunID == "" {
		record.RunID = fmt.Sprintf("ai-%d", time.Now().UnixNano())
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_analyses (
			run_id, task_id, analysis_type, model, prompt, response,
			tokens_in, tokens_out, duration_ms, status, error_message, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, record.RunID, record.TaskID, record.AnalysisType, record.Model,
		record.Prompt, record.Response, record.TokensIn, record.TokensOut,
		record.DurationMS, record.Status, record.ErrorMessage, time.Now())
	if err != nil {
		return fmt.Errorf("insert ai analysis: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAIAnalysis(ctx context.Context, runID string) (*AIAnalysisRecord, error) {
	var record AIAnalysisRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, run_id, task_id, analysis_type, model, prompt, response,
		       tokens_in, tokens_out, duration_ms, status, error_message, created_at
		FROM ai_analyses
		WHERE run_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, runID).Scan(
		&record.ID, &record.RunID, &record.TaskID, &record.AnalysisType,
		&record.Model, &record.Prompt, &record.Response,
		&record.TokensIn, &record.TokensOut, &record.DurationMS,
		&record.Status, &record.ErrorMessage, &record.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get ai analysis: %w", err)
	}
	return &record, nil
}

func (s *PostgresStore) ListAIAnalyses(ctx context.Context, taskID string, limit int) ([]AIAnalysisRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, task_id, analysis_type, model, prompt, response,
		       tokens_in, tokens_out, duration_ms, status, error_message, created_at
		FROM ai_analyses
		WHERE task_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list ai analyses: %w", err)
	}
	defer rows.Close()
	var records []AIAnalysisRecord
	for rows.Next() {
		var record AIAnalysisRecord
		if err := rows.Scan(
			&record.ID, &record.RunID, &record.TaskID, &record.AnalysisType,
			&record.Model, &record.Prompt, &record.Response,
			&record.TokensIn, &record.TokensOut, &record.DurationMS,
			&record.Status, &record.ErrorMessage, &record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ai analysis: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRunRecord(scanner scanner) (RunRecord, error) {
	var (
		record            RunRecord
		summaryRaw        []byte
		labelsRaw         []byte
		payloadRaw        []byte
		configVersionsRaw []byte
		assetVersionsRaw  []byte
		taskOverridesRaw  []byte
	)
	if err := scanner.Scan(
		&record.RunID, &record.TaskID, &record.TaskKind, &record.TriggerType, &record.RunStatus,
		&record.CheckStatus, &record.StartedAt, &record.EndedAt, &record.DurationMS, &record.ErrorMessage,
		&summaryRaw, &payloadRaw, &record.Stdout, &record.Stderr, &labelsRaw,
		&record.PluginGenerationID, &configVersionsRaw, &assetVersionsRaw, &taskOverridesRaw,
	); err != nil {
		return RunRecord{}, err
	}
	record.Payload = payloadRaw
	if err := unmarshalMapBytes(summaryRaw, &record.Summary); err != nil {
		return RunRecord{}, err
	}
	if err := unmarshalStringMapBytes(labelsRaw, &record.Labels); err != nil {
		return RunRecord{}, err
	}
	if err := unmarshalMapBytes(configVersionsRaw, &record.PluginConfigVersions); err != nil {
		return RunRecord{}, err
	}
	if err := unmarshalMapBytes(assetVersionsRaw, &record.PluginAssetVersions); err != nil {
		return RunRecord{}, err
	}
	if err := unmarshalMapBytes(taskOverridesRaw, &record.PluginTaskOverrides); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func scanRunListItem(scanner scanner) (RunListItem, error) {
	var (
		item       RunListItem
		summaryRaw []byte
		labelsRaw  []byte
	)
	if err := scanner.Scan(
		&item.RunID, &item.TaskID, &item.TaskName, &item.TaskKind, &item.TriggerType,
		&item.RunStatus, &item.CheckStatus, &item.StartedAt, &item.EndedAt, &item.DurationMS,
		&item.ErrorMessage, &summaryRaw, &item.HasPayload, &item.ArtifactCount, &item.FindingCount,
		&labelsRaw,
	); err != nil {
		return RunListItem{}, err
	}
	if err := unmarshalMapBytes(summaryRaw, &item.Summary); err != nil {
		return RunListItem{}, err
	}
	if err := unmarshalStringMapBytes(labelsRaw, &item.Labels); err != nil {
		return RunListItem{}, err
	}
	return item, nil
}

func buildRunQueryWhere(query RunQuery) (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if query.TaskID != "" {
		add("r.task_id = $%d", query.TaskID)
	}
	if query.Kind != "" {
		add("r.task_kind = $%d", query.Kind)
	}
	if query.RunStatus != "" {
		add("r.run_status = $%d", query.RunStatus)
	}
	if query.CheckStatus != "" {
		add("r.check_status = $%d", query.CheckStatus)
	}
	if query.Since > 0 {
		add("r.started_at >= $%d", time.Now().UTC().Add(-query.Since))
	}
	for key, value := range query.Labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		args = append(args, key, value)
		clauses = append(clauses, fmt.Sprintf("r.labels_json ->> $%d = $%d", len(args)-1, len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (s *PostgresStore) listFindings(ctx context.Context, runID string) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT finding_id, run_id, task_id, sample_id, reason, data_json
		FROM findings
		WHERE run_id = $1
		ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	var findings []Finding
	for rows.Next() {
		var (
			finding Finding
			dataRaw []byte
		)
		if err := rows.Scan(&finding.FindingID, &finding.RunID, &finding.TaskID, &finding.SampleID, &finding.Reason, &dataRaw); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		if err := unmarshalMapBytes(dataRaw, &finding.Data); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func (s *PostgresStore) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM kv_metadata WHERE key = $1`, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrMetaNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get meta %s: %w", key, err)
	}
	return value, nil
}

func (s *PostgresStore) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kv_metadata (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set meta %s: %w", key, err)
	}
	return nil
}

const (
	settingsKeySinks      = "trace_sinks"
	settingsKeyMaxBytes   = "trace_max_payload_bytes"
	settingsKeyRetainDays = "trace_default_retain_days"
	platformConfigKey     = "platform_config"
)

func (s *PostgresStore) LoadGlobalSettings(ctx context.Context) (config.GlobalSettings, error) {
	sinksRaw, _ := s.GetMeta(ctx, settingsKeySinks)
	maxBytesRaw, _ := s.GetMeta(ctx, settingsKeyMaxBytes)
	retainDaysRaw, _ := s.GetMeta(ctx, settingsKeyRetainDays)
	return config.ParseGlobalSettings(sinksRaw, maxBytesRaw, retainDaysRaw), nil
}

func (s *PostgresStore) SaveGlobalSettings(ctx context.Context, gs config.GlobalSettings) error {
	sinksJSON, err := json.Marshal(gs.Sinks)
	if err != nil {
		return err
	}
	if err := s.SetMeta(ctx, settingsKeySinks, string(sinksJSON)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, settingsKeyMaxBytes, strconv.Itoa(gs.MaxPayloadBytes)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, settingsKeyRetainDays, strconv.Itoa(gs.DefaultRetainDays)); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) LoadPlatformConfig(ctx context.Context) (config.PlatformConfigSummary, error) {
	raw, err := s.GetMeta(ctx, platformConfigKey)
	if err != nil {
		if errors.Is(err, ErrMetaNotFound) {
			return config.PlatformConfigSummary{}, ErrMetaNotFound
		}
		return config.PlatformConfigSummary{}, err
	}
	var summary config.PlatformConfigSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return config.PlatformConfigSummary{}, fmt.Errorf("decode platform config: %w", err)
	}
	return summary, nil
}

func (s *PostgresStore) SavePlatformConfig(ctx context.Context, summary config.PlatformConfigSummary) error {
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return s.SetMeta(ctx, platformConfigKey, string(raw))
}

func (s *PostgresStore) ListTaskDefinitions(ctx context.Context) ([]config.TaskDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, name, kind, enabled, interval, cron, timeout,
		       labels_json, params_json, trigger, watch_task_id, watch_condition,
		       trace_json, alert_json, pipeline_id, created_at, updated_at
		FROM task_definitions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list task definitions: %w", err)
	}
	defer rows.Close()
	var defs []config.TaskDefinition
	for rows.Next() {
		var d config.TaskDefinition
		if err := rows.Scan(&d.TaskID, &d.Name, &d.Kind, &d.Enabled,
			&d.Interval, &d.Cron, &d.Timeout,
			&d.LabelsJSON, &d.ParamsJSON, &d.Trigger, &d.WatchTaskID, &d.WatchCondition,
			&d.TraceJSON, &d.AlertJSON, &d.PipelineID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task definition: %w", err)
		}
		if len(d.LabelsJSON) > 0 {
			_ = json.Unmarshal(d.LabelsJSON, &d.Labels)
		}
		if len(d.ParamsJSON) > 0 {
			_ = json.Unmarshal(d.ParamsJSON, &d.Params)
		}
		if len(d.TraceJSON) > 0 {
			_ = json.Unmarshal(d.TraceJSON, &d.Trace)
		}
		if len(d.AlertJSON) > 0 {
			_ = json.Unmarshal(d.AlertJSON, &d.Alert)
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachDependencies(ctx, defs); err != nil {
		return nil, err
	}
	return defs, nil
}

func (s *PostgresStore) GetTaskDefinition(ctx context.Context, taskID string) (*config.TaskDefinition, error) {
	var d config.TaskDefinition
	err := s.db.QueryRowContext(ctx, `
		SELECT task_id, name, kind, enabled, interval, cron, timeout,
		       labels_json, params_json, trigger, watch_task_id, watch_condition,
		       trace_json, alert_json, pipeline_id, created_at, updated_at
		FROM task_definitions
		WHERE task_id = $1
	`, taskID).Scan(&d.TaskID, &d.Name, &d.Kind, &d.Enabled,
		&d.Interval, &d.Cron, &d.Timeout,
		&d.LabelsJSON, &d.ParamsJSON, &d.Trigger, &d.WatchTaskID, &d.WatchCondition,
		&d.TraceJSON, &d.AlertJSON, &d.PipelineID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task definition %s: %w", taskID, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("get task definition %s: %w", taskID, err)
	}
	if len(d.LabelsJSON) > 0 {
		_ = json.Unmarshal(d.LabelsJSON, &d.Labels)
	}
	if len(d.ParamsJSON) > 0 {
		_ = json.Unmarshal(d.ParamsJSON, &d.Params)
	}
	if len(d.TraceJSON) > 0 {
		_ = json.Unmarshal(d.TraceJSON, &d.Trace)
	}
	if len(d.AlertJSON) > 0 {
		_ = json.Unmarshal(d.AlertJSON, &d.Alert)
	}
	deps, err := s.ListTaskDependencies(ctx)
	if err != nil {
		return nil, err
	}
	for _, dep := range deps {
		if dep.DownstreamTaskID == d.TaskID {
			d.Dependencies = append(d.Dependencies, dep)
		}
	}
	return &d, nil
}

func (s *PostgresStore) InsertTaskDefinition(ctx context.Context, def config.TaskDefinition) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_definitions (
			task_id, name, kind, enabled, interval, cron, timeout,
			labels_json, params_json, trigger, watch_task_id, watch_condition,
			trace_json, alert_json, pipeline_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12, $13::jsonb, $14::jsonb, $15, $16, $16)
	`, def.TaskID, def.Name, def.Kind, def.Enabled,
		def.Interval, def.Cron, def.Timeout,
		string(def.LabelsJSON), string(def.ParamsJSON), def.Trigger, def.WatchTaskID, def.WatchCondition,
		string(def.TraceJSON), string(def.AlertJSON), def.PipelineID, now)
	if err != nil {
		return fmt.Errorf("insert task definition %s: %w", def.TaskID, err)
	}
	if def.Dependencies != nil {
		if err := s.ReplaceTaskDependencies(ctx, def.TaskID, def.Dependencies); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) UpdateTaskDefinition(ctx context.Context, def config.TaskDefinition) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_definitions SET
			name = $2, kind = $3, enabled = $4, interval = $5, cron = $6, timeout = $7,
			labels_json = $8::jsonb, params_json = $9::jsonb, trigger = $10,
			watch_task_id = $11, watch_condition = $12,
			trace_json = $13::jsonb, alert_json = $14::jsonb,
			pipeline_id = $15, updated_at = $16
		WHERE task_id = $1
	`, def.TaskID, def.Name, def.Kind, def.Enabled,
		def.Interval, def.Cron, def.Timeout,
		string(def.LabelsJSON), string(def.ParamsJSON), def.Trigger,
		def.WatchTaskID, def.WatchCondition,
		string(def.TraceJSON), string(def.AlertJSON), def.PipelineID, now)
	if err != nil {
		return fmt.Errorf("update task definition %s: %w", def.TaskID, err)
	}
	if def.Dependencies != nil {
		if err := s.ReplaceTaskDependencies(ctx, def.TaskID, def.Dependencies); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) DeleteTaskDefinition(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM task_definitions WHERE task_id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("delete task definition %s: %w", taskID, err)
	}
	return nil
}

func (s *PostgresStore) ListPipelines(ctx context.Context) ([]config.Pipeline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM pipelines ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()
	var pipelines []config.Pipeline
	for rows.Next() {
		var p config.Pipeline
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline: %w", err)
		}
		pipelines = append(pipelines, p)
	}
	return pipelines, rows.Err()
}

func (s *PostgresStore) GetPipeline(ctx context.Context, id string) (*config.Pipeline, error) {
	var p config.Pipeline
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM pipelines WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("pipeline %s: %w", id, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("get pipeline %s: %w", id, err)
	}
	return &p, nil
}

func (s *PostgresStore) InsertPipeline(ctx context.Context, p config.Pipeline) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipelines (id, name, description)
		VALUES ($1, $2, $3)
	`, p.ID, p.Name, p.Description)
	if err != nil {
		return fmt.Errorf("insert pipeline %s: %w", p.ID, err)
	}
	return nil
}

func (s *PostgresStore) UpdatePipeline(ctx context.Context, p config.Pipeline) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE pipelines SET name = $2, description = $3, updated_at = NOW()
		WHERE id = $1
	`, p.ID, p.Name, p.Description)
	if err != nil {
		return fmt.Errorf("update pipeline %s: %w", p.ID, err)
	}
	return nil
}

func (s *PostgresStore) DeletePipeline(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete pipeline %s: %w", id, err)
	}
	return nil
}

func (s *PostgresStore) ListTaskDefinitionsByPipeline(ctx context.Context, pipelineID string) ([]config.TaskDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, name, kind, enabled, interval, cron, timeout,
		       labels_json, params_json, trigger, watch_task_id, watch_condition,
		       trace_json, alert_json, pipeline_id, created_at, updated_at
		FROM task_definitions
		WHERE pipeline_id = $1
		ORDER BY name`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list task definitions by pipeline: %w", err)
	}
	defer rows.Close()
	var defs []config.TaskDefinition
	for rows.Next() {
		var d config.TaskDefinition
		if err := rows.Scan(&d.TaskID, &d.Name, &d.Kind, &d.Enabled,
			&d.Interval, &d.Cron, &d.Timeout,
			&d.LabelsJSON, &d.ParamsJSON, &d.Trigger, &d.WatchTaskID, &d.WatchCondition,
			&d.TraceJSON, &d.AlertJSON, &d.PipelineID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task definition: %w", err)
		}
		if len(d.LabelsJSON) > 0 {
			_ = json.Unmarshal(d.LabelsJSON, &d.Labels)
		}
		if len(d.ParamsJSON) > 0 {
			_ = json.Unmarshal(d.ParamsJSON, &d.Params)
		}
		if len(d.TraceJSON) > 0 {
			_ = json.Unmarshal(d.TraceJSON, &d.Trace)
		}
		if len(d.AlertJSON) > 0 {
			_ = json.Unmarshal(d.AlertJSON, &d.Alert)
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachDependencies(ctx, defs); err != nil {
		return nil, err
	}
	return defs, nil
}

func (s *PostgresStore) UpdateTaskPipeline(ctx context.Context, taskID string, pipelineID *string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_definitions SET pipeline_id = $2, updated_at = NOW()
		WHERE task_id = $1
	`, taskID, pipelineID)
	if err != nil {
		return fmt.Errorf("update task pipeline %s: %w", taskID, err)
	}
	return nil
}

func (s *PostgresStore) ListTaskDependencies(ctx context.Context) ([]config.TaskDependency, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at
		FROM task_dependencies
		ORDER BY upstream_task_id, downstream_task_id, source_key`)
	if err != nil {
		return nil, fmt.Errorf("list task dependencies: %w", err)
	}
	defer rows.Close()
	var deps []config.TaskDependency
	for rows.Next() {
		dep, err := scanTaskDependency(rows)
		if err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

func (s *PostgresStore) ListTaskDependenciesByPipeline(ctx context.Context, pipelineID string) ([]config.TaskDependency, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.upstream_task_id, d.downstream_task_id, d.condition, d.source_key,
		       d.params_json, d.created_at, d.updated_at
		FROM task_dependencies d
		JOIN task_definitions td ON td.task_id = d.downstream_task_id
		WHERE td.pipeline_id = $1
		ORDER BY d.upstream_task_id, d.downstream_task_id, d.source_key`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list task dependencies by pipeline: %w", err)
	}
	defer rows.Close()
	var deps []config.TaskDependency
	for rows.Next() {
		dep, err := scanTaskDependency(rows)
		if err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

func (s *PostgresStore) ReplaceTaskDependencies(ctx context.Context, taskID string, dependencies []config.TaskDependency) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace task dependencies: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE downstream_task_id = $1`, taskID); err != nil {
		return fmt.Errorf("delete task dependencies %s: %w", taskID, err)
	}
	for _, dep := range dependencies {
		if dep.UpstreamTaskID == "" {
			return fmt.Errorf("dependency upstream_task_id is required")
		}
		dep.DownstreamTaskID = taskID
		if dep.ID == "" {
			dep.ID = uuid.NewString()
		}
		paramsJSON, err := marshalJSONBytes(dep.Params)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO task_dependencies (
				id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6::jsonb, NOW(), NOW())
		`, dep.ID, dep.UpstreamTaskID, dep.DownstreamTaskID, dep.Condition, dep.SourceKey, string(paramsJSON)); err != nil {
			return fmt.Errorf("insert task dependency %s -> %s: %w", dep.UpstreamTaskID, taskID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace task dependencies: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertTaskDependency(ctx context.Context, dependency config.TaskDependency) (config.TaskDependency, error) {
	if dependency.UpstreamTaskID == "" {
		return config.TaskDependency{}, fmt.Errorf("upstream_task_id is required")
	}
	if dependency.DownstreamTaskID == "" {
		return config.TaskDependency{}, fmt.Errorf("downstream_task_id is required")
	}
	paramsJSON, err := marshalJSONBytes(dependency.Params)
	if err != nil {
		return config.TaskDependency{}, err
	}
	if dependency.ID != "" {
		err = s.db.QueryRowContext(ctx, `
			UPDATE task_dependencies
			SET upstream_task_id = $2,
			    downstream_task_id = $3,
			    condition = $4,
			    source_key = $5,
			    params_json = $6::jsonb,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at
		`, dependency.ID, dependency.UpstreamTaskID, dependency.DownstreamTaskID, dependency.Condition, dependency.SourceKey, string(paramsJSON)).
			Scan(
				&dependency.ID, &dependency.UpstreamTaskID, &dependency.DownstreamTaskID,
				&dependency.Condition, &dependency.SourceKey, &dependency.ParamsJSON,
				&dependency.CreatedAt, &dependency.UpdatedAt,
			)
		if err == nil {
			if len(dependency.ParamsJSON) > 0 {
				_ = json.Unmarshal(dependency.ParamsJSON, &dependency.Params)
			}
			return dependency, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return config.TaskDependency{}, fmt.Errorf("update task dependency: %w", err)
		}
	} else {
		dependency.ID = uuid.NewString()
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO task_dependencies (
			id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, NOW(), NOW())
		ON CONFLICT (upstream_task_id, downstream_task_id, source_key) DO UPDATE SET
			condition = EXCLUDED.condition,
			params_json = EXCLUDED.params_json,
			updated_at = NOW()
		RETURNING id, upstream_task_id, downstream_task_id, condition, source_key, params_json, created_at, updated_at
	`, dependency.ID, dependency.UpstreamTaskID, dependency.DownstreamTaskID, dependency.Condition, dependency.SourceKey, string(paramsJSON)).
		Scan(
			&dependency.ID, &dependency.UpstreamTaskID, &dependency.DownstreamTaskID,
			&dependency.Condition, &dependency.SourceKey, &dependency.ParamsJSON,
			&dependency.CreatedAt, &dependency.UpdatedAt,
		)
	if err != nil {
		return config.TaskDependency{}, fmt.Errorf("upsert task dependency: %w", err)
	}
	if len(dependency.ParamsJSON) > 0 {
		_ = json.Unmarshal(dependency.ParamsJSON, &dependency.Params)
	}
	return dependency, nil
}

func (s *PostgresStore) DeleteTaskDependency(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM task_dependencies WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete task dependency %s: %w", id, err)
	}
	return nil
}

func (s *PostgresStore) attachDependencies(ctx context.Context, defs []config.TaskDefinition) error {
	if len(defs) == 0 {
		return nil
	}
	deps, err := s.ListTaskDependencies(ctx)
	if err != nil {
		return err
	}
	byTask := map[string][]config.TaskDependency{}
	for _, dep := range deps {
		byTask[dep.DownstreamTaskID] = append(byTask[dep.DownstreamTaskID], dep)
	}
	for i := range defs {
		defs[i].Dependencies = byTask[defs[i].TaskID]
	}
	return nil
}

func scanTaskDependency(scanner scanner) (config.TaskDependency, error) {
	var dep config.TaskDependency
	if err := scanner.Scan(
		&dep.ID, &dep.UpstreamTaskID, &dep.DownstreamTaskID,
		&dep.Condition, &dep.SourceKey, &dep.ParamsJSON,
		&dep.CreatedAt, &dep.UpdatedAt,
	); err != nil {
		return config.TaskDependency{}, fmt.Errorf("scan task dependency: %w", err)
	}
	if len(dep.ParamsJSON) > 0 {
		if err := json.Unmarshal(dep.ParamsJSON, &dep.Params); err != nil {
			return config.TaskDependency{}, fmt.Errorf("unmarshal dependency params: %w", err)
		}
	}
	return dep, nil
}

func scanPluginPackage(scanner scanner) (pluginmodel.PackageRecord, error) {
	var record pluginmodel.PackageRecord
	if err := scanner.Scan(
		&record.ID, &record.Name, &record.Description, &record.Author, &record.Homepage,
		&record.Official, &record.Bundled, &record.Status, &record.LastError,
		&record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return pluginmodel.PackageRecord{}, fmt.Errorf("scan plugin package: %w", err)
	}
	return record, nil
}

func scanPluginRelease(scanner scanner) (pluginmodel.ReleaseRecord, error) {
	var record pluginmodel.ReleaseRecord
	var manifestJSON []byte
	if err := scanner.Scan(
		&record.PluginID, &record.Version, &record.SchemaVersion, &manifestJSON,
		&record.Path, &record.Status, &record.Checksum, &record.ValidationError,
		&record.Official, &record.Bundled, &record.CreatedAt, &record.UpdatedAt,
		&record.ValidatedAt, &record.ActivatedAt,
	); err != nil {
		return pluginmodel.ReleaseRecord{}, fmt.Errorf("scan plugin release: %w", err)
	}
	if len(manifestJSON) > 0 {
		if err := json.Unmarshal(manifestJSON, &record.Manifest); err != nil {
			return pluginmodel.ReleaseRecord{}, fmt.Errorf("unmarshal plugin manifest: %w", err)
		}
	}
	return record, nil
}

func scanPluginConfigInstance(scanner scanner) (pluginmodel.ConfigInstanceRecord, error) {
	var record pluginmodel.ConfigInstanceRecord
	if err := scanner.Scan(
		&record.ID, &record.PluginID, &record.CapabilityID, &record.CapabilityType, &record.CapabilityName,
		&record.Scope, &record.Title, &record.Status, &record.ActiveVersion,
		&record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return pluginmodel.ConfigInstanceRecord{}, fmt.Errorf("scan plugin config instance: %w", err)
	}
	return record, nil
}

func scanPluginConfigVersion(scanner scanner) (pluginmodel.ConfigVersionRecord, error) {
	var record pluginmodel.ConfigVersionRecord
	var valuesJSON []byte
	if err := scanner.Scan(
		&record.InstanceID, &record.Version, &record.Status, &valuesJSON, &record.ValidationError,
		&record.CreatedAt, &record.UpdatedAt, &record.ValidatedAt, &record.ActivatedAt, &record.RetiredAt,
	); err != nil {
		return pluginmodel.ConfigVersionRecord{}, fmt.Errorf("scan plugin config version: %w", err)
	}
	if err := unmarshalMapBytes(valuesJSON, &record.Values); err != nil {
		return pluginmodel.ConfigVersionRecord{}, err
	}
	return record, nil
}

func scanPluginAsset(scanner scanner) (pluginmodel.AssetRecord, error) {
	var record pluginmodel.AssetRecord
	if err := scanner.Scan(
		&record.ID, &record.PluginID, &record.CapabilityID, &record.ConfigInstanceID, &record.Scope,
		&record.Kind, &record.Title, &record.Status, &record.ActiveVersion, &record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return pluginmodel.AssetRecord{}, fmt.Errorf("scan plugin asset: %w", err)
	}
	return record, nil
}

func scanPluginAssetVersion(scanner scanner) (pluginmodel.AssetVersionRecord, error) {
	var record pluginmodel.AssetVersionRecord
	if err := scanner.Scan(
		&record.AssetID, &record.Version, &record.Status, &record.Filename, &record.ContentType,
		&record.StorageURI, &record.Content, &record.SizeBytes, &record.Checksum, &record.ValidationError,
		&record.CreatedAt, &record.UpdatedAt, &record.ValidatedAt, &record.ActivatedAt, &record.RetiredAt,
	); err != nil {
		return pluginmodel.AssetVersionRecord{}, fmt.Errorf("scan plugin asset version: %w", err)
	}
	return record, nil
}

func scanPluginSecret(scanner scanner) (pluginmodel.SecretRecord, error) {
	var record pluginmodel.SecretRecord
	if err := scanner.Scan(
		&record.ID, &record.PluginID, &record.Scope, &record.Title, &record.Masked,
		&record.Status, &record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return pluginmodel.SecretRecord{}, fmt.Errorf("scan plugin secret: %w", err)
	}
	return record, nil
}

func scanPluginSecretValue(scanner scanner) (pluginmodel.SecretValueRecord, error) {
	var record pluginmodel.SecretValueRecord
	var metaJSON []byte
	if err := scanner.Scan(&record.SecretID, &record.Ciphertext, &metaJSON, &record.UpdatedAt); err != nil {
		return pluginmodel.SecretValueRecord{}, fmt.Errorf("scan plugin secret value: %w", err)
	}
	if err := unmarshalMapBytes(metaJSON, &record.EncryptionMeta); err != nil {
		return pluginmodel.SecretValueRecord{}, err
	}
	return record, nil
}

func scanPluginConfigEvent(scanner scanner) (pluginmodel.ConfigEventRecord, error) {
	var record pluginmodel.ConfigEventRecord
	if err := scanner.Scan(
		&record.ID, &record.ResourceType, &record.ResourceID, &record.PluginID,
		&record.Action, &record.Status, &record.Message, &record.CreatedAt,
	); err != nil {
		return pluginmodel.ConfigEventRecord{}, fmt.Errorf("scan plugin config event: %w", err)
	}
	return record, nil
}

func marshalJSONBytes(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return raw, nil
}

func unmarshalMapBytes(raw []byte, target *map[string]any) error {
	if len(raw) == 0 {
		*target = map[string]any{}
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("unmarshal map: %w", err)
	}
	return nil
}

func unmarshalStringMapBytes(raw []byte, target *map[string]string) error {
	if len(raw) == 0 {
		*target = map[string]string{}
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("unmarshal string map: %w", err)
	}
	return nil
}
