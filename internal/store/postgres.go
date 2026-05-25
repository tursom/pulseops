package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"pulseops/internal/config"

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
	Available bool   `json:"available"`
	TaskID    string `json:"task_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Source    string `json:"source,omitempty"`
	Data      any    `json:"data,omitempty"`
	JQResult  any    `json:"jq_result,omitempty"`
}

type RunStat struct {
	RunID      string    `json:"run_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	RunStatus  string    `json:"run_status"`
}

type RunRecord struct {
	RunID        string            `json:"run_id"`
	TaskID       string            `json:"task_id"`
	TaskKind     string            `json:"task_kind"`
	TriggerType  string            `json:"trigger_type"`
	RunStatus    string            `json:"run_status"`
	CheckStatus  string            `json:"check_status"`
	StartedAt    time.Time         `json:"started_at"`
	EndedAt      time.Time         `json:"ended_at"`
	DurationMS   int64             `json:"duration_ms"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Summary      map[string]any    `json:"summary,omitempty"`
	Payload      []byte            `json:"payload,omitempty"`
	ArtifactRefs []ArtifactRef     `json:"artifact_refs,omitempty"`
	Findings     []Finding         `json:"findings,omitempty"`
	Stdout       string            `json:"stdout,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type PaginatedRuns struct {
	Records []RunRecord `json:"records"`
	Total   int         `json:"total"`
}

type Repository interface {
	Close() error
	UpsertTaskState(ctx context.Context, state TaskState) error
	DeleteTaskState(ctx context.Context, taskID string) error
	InsertRun(ctx context.Context, record RunRecord) error
	ListRuns(ctx context.Context, taskID string, limit, offset int, since time.Duration) ([]RunRecord, error)
	CountRuns(ctx context.Context, taskID string, since time.Duration) (int, error)
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
}

type PostgresStore struct {
	db *sql.DB
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
			run_id, task_id, task_kind, trigger_type, run_status, check_status,
			started_at, ended_at, duration_ms, error_message, summary_json, payload,
			stdout, stderr, labels_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15::jsonb)
	`, record.RunID, record.TaskID, record.TaskKind, record.TriggerType, record.RunStatus,
		record.CheckStatus, record.StartedAt, record.EndedAt, record.DurationMS, record.ErrorMessage,
		string(summaryJSON), payload, record.Stdout, record.Stderr, string(labelsJSON))
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
			       stdout, stderr, labels_json
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
		       stdout, stderr, labels_json
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
		       stdout, stderr, labels_json
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
		record     RunRecord
		summaryRaw []byte
		labelsRaw  []byte
		payloadRaw []byte
	)
	if err := scanner.Scan(
		&record.RunID, &record.TaskID, &record.TaskKind, &record.TriggerType, &record.RunStatus,
		&record.CheckStatus, &record.StartedAt, &record.EndedAt, &record.DurationMS, &record.ErrorMessage,
		&summaryRaw, &payloadRaw, &record.Stdout, &record.Stderr, &labelsRaw,
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
	return record, nil
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
	return defs, rows.Err()
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
	return defs, rows.Err()
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
