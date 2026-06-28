package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pulseops/internal/appctx"
	"pulseops/internal/config"
	pluginmgr "pulseops/internal/plugin"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

type TaskManager interface {
	ListTasks() []store.TaskState
	GetTask(id string) (store.TaskState, bool)
	RunTask(ctx context.Context, id string, trigger task.TriggerType) (store.RunRecord, error)
	ReloadTask(ctx context.Context, id string) error
	SetTaskEnabled(ctx context.Context, id string, enabled bool) error
	UpsertTaskFromDB(ctx context.Context, def config.TaskDefinition) (store.TaskState, error)
	ValidateTaskDefinition(def config.TaskDefinition) (config.TaskSpec, error)
	TestRunTaskDefinition(ctx context.Context, def config.TaskDefinition) (store.RunRecord, error)
	RemoveTaskByID(ctx context.Context, taskID string) error
}

type SettingsReloader interface {
	ReloadSinks(entries []config.SinkEntry, store *store.PostgresStore) error
	SetMaxPayloadBytes(maxPayloadBytes int)
}

type PluginManager interface {
	Catalog(ctx context.Context) (pluginmgr.Catalog, error)
	Plugin(ctx context.Context, pluginID string) (pluginmgr.PluginView, error)
	Releases(ctx context.Context, pluginID string) ([]pluginmgr.ReleaseRecord, error)
	Capabilities(ctx context.Context, typ, kind string) ([]pluginmgr.Capability, error)
	Reload(ctx context.Context) (pluginmgr.Catalog, error)
	ValidateRelease(ctx context.Context, pluginID, version string) (pluginmgr.ReleaseRecord, error)
	ActivateRelease(ctx context.Context, pluginID, version string) (pluginmgr.Catalog, error)
	DisablePlugin(ctx context.Context, pluginID string) (pluginmgr.Catalog, error)
	EnablePlugin(ctx context.Context, pluginID string) (pluginmgr.Catalog, error)
	RollbackPlugin(ctx context.Context, pluginID string) (pluginmgr.Catalog, error)
	GC(ctx context.Context) (pluginmgr.Catalog, error)
	ExportRelease(ctx context.Context, pluginID, version string) (string, []byte, error)
	ImportRelease(ctx context.Context, reader io.Reader) (pluginmgr.ReleaseRecord, error)
}

type PluginStatusRefresher interface {
	RefreshPluginStatus(ctx context.Context)
}

type settingsResponse struct {
	Settings config.GlobalSettings `json:"settings"`
	Applied  bool                  `json:"applied"`
	Warnings []string              `json:"warnings"`
}

type taskRuntimeView struct {
	Status              string     `json:"status"`
	LastRunStatus       string     `json:"last_run_status,omitempty"`
	LastCheckStatus     string     `json:"last_check_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastDurationMS      int64      `json:"last_duration_ms,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

type taskDependencyView struct {
	UpstreamTaskID   string `json:"upstream_task_id,omitempty"`
	DownstreamCount  int    `json:"downstream_count"`
	UpstreamCount    int    `json:"upstream_count"`
	PipelineID       string `json:"pipeline_id,omitempty"`
	WatchCondition   string `json:"watch_condition,omitempty"`
	DependencyStatus string `json:"dependency_status,omitempty"`
}

type taskView struct {
	store.TaskState
	ConfigStatus string                  `json:"config_status"`
	LoadError    string                  `json:"load_error,omitempty"`
	Runtime      taskRuntimeView         `json:"runtime"`
	Dependency   taskDependencyView      `json:"dependency"`
	Definition   *config.TaskDefinition  `json:"definition,omitempty"`
	Dependencies []config.TaskDependency `json:"dependencies,omitempty"`
}

type dashboardSummary struct {
	Counts       dashboardCounts     `json:"counts"`
	Health       dashboardHealth     `json:"health"`
	Anomalies    []taskView          `json:"anomalies"`
	RecentRuns   []store.RunListItem `json:"recent_runs"`
	LabelGroups  []labelAggregate    `json:"label_groups"`
	GeneratedAt  time.Time           `json:"generated_at"`
	RefreshAfter string              `json:"refresh_after"`
}

type dashboardCounts struct {
	Total       int `json:"total"`
	Enabled     int `json:"enabled"`
	Failed      int `json:"failed"`
	CheckFailed int `json:"check_failed"`
	LoadFailed  int `json:"load_failed"`
	Stale       int `json:"stale"`
	Disabled    int `json:"disabled"`
}

type dashboardHealth struct {
	Status string `json:"status"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

type labelAggregate struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Total    int    `json:"total"`
	Abnormal int    `json:"abnormal"`
}

type taskGraph struct {
	Nodes []taskGraphNode `json:"nodes"`
	Edges []taskGraphEdge `json:"edges"`
}

type taskGraphNode struct {
	TaskID       string            `json:"task_id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Enabled      bool              `json:"enabled"`
	Labels       map[string]string `json:"labels"`
	PipelineID   string            `json:"pipeline_id,omitempty"`
	ConfigStatus string            `json:"config_status"`
	Runtime      taskRuntimeView   `json:"runtime"`
}

type taskGraphEdge struct {
	ID               string         `json:"id"`
	UpstreamTaskID   string         `json:"upstream_task_id"`
	DownstreamTaskID string         `json:"downstream_task_id"`
	Condition        string         `json:"condition,omitempty"`
	SourceKey        string         `json:"source_key,omitempty"`
	Params           map[string]any `json:"params,omitempty"`
	Valid            bool           `json:"valid"`
	Error            string         `json:"error,omitempty"`
	Legacy           bool           `json:"legacy,omitempty"`
}

type batchTaskRequest struct {
	Action  string   `json:"action"`
	TaskIDs []string `json:"task_ids"`
}

type batchTaskResult struct {
	TaskID string           `json:"task_id"`
	OK     bool             `json:"ok"`
	Error  string           `json:"error,omitempty"`
	Run    *store.RunRecord `json:"run,omitempty"`
}

type taskValidationResponse struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors"`
	Normalized any      `json:"normalized,omitempty"`
}

func Routes(staticDir string, manager TaskManager, repository store.Repository, artifactStore store.ArtifactStore, settingsReloader SettingsReloader, pluginManager PluginManager, platform config.PlatformConfigSummary, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// API routes — register before static/file routes for specificity priority
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": platform.Mode,
			"time":   time.Now().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /api/platform-config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, platform)
	})
	mux.HandleFunc("PUT /api/platform-config", func(w http.ResponseWriter, r *http.Request) {
		var updated config.PlatformConfigSummary
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		updated.Mode = platform.Mode
		updated.Applied = false
		updated.Warnings = append(updated.Warnings, "AI and artifact store changes are saved and require service restart")
		if err := repository.SavePlatformConfig(r.Context(), updated); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})
	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.Catalog(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("POST /api/plugins/reload", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.Reload(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("POST /api/plugins/install", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.Reload(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("POST /api/plugins/import", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		defer r.Body.Close()
		release, err := pluginManager.ImportRelease(r.Context(), http.MaxBytesReader(w, r.Body, 128<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, release)
	})
	mux.HandleFunc("POST /api/plugins/gc", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.GC(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("GET /api/plugin-capabilities", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		caps, err := pluginManager.Capabilities(r.Context(), r.URL.Query().Get("type"), r.URL.Query().Get("kind"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, caps)
	})
	mux.HandleFunc("GET /api/plugins/{id}", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		item, err := pluginManager.Plugin(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "plugin not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/plugins/{id}/releases", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		releases, err := pluginManager.Releases(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, releases)
	})
	mux.HandleFunc("POST /api/plugins/{id}/releases/{version}/validate", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		release, err := pluginManager.ValidateRelease(r.Context(), r.PathValue("id"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, release)
	})
	mux.HandleFunc("POST /api/plugins/{id}/releases/{version}/activate", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.ActivateRelease(r.Context(), r.PathValue("id"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		refreshPluginStatus(r.Context(), manager)
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("GET /api/plugins/{id}/releases/{version}/export", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		filename, content, err := pluginManager.ExportRelease(r.Context(), r.PathValue("id"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(filename, `"`, "")+`"`)
		_, _ = w.Write(content)
	})
	mux.HandleFunc("POST /api/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.DisablePlugin(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		refreshPluginStatus(r.Context(), manager)
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("POST /api/plugins/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.EnablePlugin(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		refreshPluginStatus(r.Context(), manager)
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("POST /api/plugins/{id}/rollback", func(w http.ResponseWriter, r *http.Request) {
		if pluginManager == nil {
			writeError(w, http.StatusServiceUnavailable, "plugin manager is unavailable")
			return
		}
		catalog, err := pluginManager.RollbackPlugin(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		refreshPluginStatus(r.Context(), manager)
		writeJSON(w, http.StatusOK, catalog)
	})
	mux.HandleFunc("GET /api/dashboard/summary", func(w http.ResponseWriter, r *http.Request) {
		since := parseDurationQuery(r, "since", 24*time.Hour)
		views, err := buildTaskViews(r.Context(), manager, repository, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recentRuns, _, err := repository.ListRunsAcrossTasks(r.Context(), store.RunQuery{Since: since, Limit: 20})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, buildDashboardSummary(views, recentRuns))
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		views, err := buildTaskViews(r.Context(), manager, repository, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		views = filterTaskViews(r, views)
		if views == nil {
			views = []taskView{}
		}
		writeJSON(w, http.StatusOK, views)
	})
	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		views, err := buildTaskViews(r.Context(), manager, repository, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(views) == 0 {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSON(w, http.StatusOK, views[0])
	})
	mux.HandleFunc("GET /api/tasks/{id}/sample", func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")
		if source == "" || (source != "payload" && source != "summary" && source != "record" && !strings.HasPrefix(source, "artifact:")) {
			writeError(w, http.StatusBadRequest, `query param "source" required: payload, summary, record, or artifact:<kind>`)
			return
		}
		jqExpr := r.URL.Query().Get("jq")
		resp, err := task.FetchSampleData(r.Context(), repository, artifactStore, r.PathValue("id"), source, jqExpr)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("GET /api/tasks/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		since, _ := time.ParseDuration(r.URL.Query().Get("since"))
		records, err := repository.ListRunItems(r.Context(), r.PathValue("id"), limit, offset, since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		total, err := repository.CountRuns(r.Context(), r.PathValue("id"), since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if records == nil {
			records = []store.RunListItem{}
		}
		writeJSON(w, http.StatusOK, store.PaginatedRuns{Records: records, Total: total})
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		query := parseRunQuery(r)
		records, total, err := repository.ListRunsAcrossTasks(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if records == nil {
			records = []store.RunListItem{}
		}
		writeJSON(w, http.StatusOK, store.PaginatedRuns{Records: records, Total: total})
	})
	mux.HandleFunc("GET /api/tasks/{id}/runs/stats", func(w http.ResponseWriter, r *http.Request) {
		since, _ := time.ParseDuration(r.URL.Query().Get("since"))
		stats, err := repository.ListRunStats(r.Context(), r.PathValue("id"), since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if stats == nil {
			stats = []store.RunStat{}
		}
		writeJSON(w, http.StatusOK, stats)
	})
	mux.HandleFunc("GET /api/tasks/{id}/runs/{runID}", func(w http.ResponseWriter, r *http.Request) {
		record, err := repository.GetRun(r.Context(), r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "run not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := hydrateRunPayloadFromArtifact(r.Context(), &record, artifactStore); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("GET /api/tasks/{id}/runs/{runID}/ai", func(w http.ResponseWriter, r *http.Request) {
		analysis, err := repository.GetAIAnalysis(r.Context(), r.PathValue("runID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if analysis == nil {
			writeError(w, http.StatusNotFound, "ai analysis not found")
			return
		}
		writeJSON(w, http.StatusOK, analysis)
	})
	mux.HandleFunc("GET /api/tasks/{id}/ai", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		records, err := repository.ListAIAnalyses(r.Context(), r.PathValue("id"), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if records == nil {
			records = []store.AIAnalysisRecord{}
		}
		writeJSON(w, http.StatusOK, records)
	})
	mux.HandleFunc("GET /api/tasks/{id}/runs/{runID}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		artifacts, err := repository.ListArtifactsByRun(r.Context(), r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if artifacts == nil {
			artifacts = []store.ArtifactRef{}
		}
		writeJSON(w, http.StatusOK, artifacts)
	})
	mux.HandleFunc("GET /api/artifacts/{artifactID}", func(w http.ResponseWriter, r *http.Request) {
		artifact, err := repository.GetArtifact(r.Context(), r.PathValue("artifactID"))
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "artifact not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		key, err := store.ObjectKeyFromURI(artifact.URI)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		downloadURL, err := artifactStore.PresignGet(r.Context(), key, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"artifact":     artifact,
			"download_url": downloadURL,
		})
	})
	mux.HandleFunc("GET /api/artifacts/{artifactID}/content", func(w http.ResponseWriter, r *http.Request) {
		artifact, err := repository.GetArtifact(r.Context(), r.PathValue("artifactID"))
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "artifact not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		key, err := store.ObjectKeyFromURI(artifact.URI)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		reader, err := artifactStore.Get(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer reader.Close()
		w.Header().Set("Content-Type", artifact.ContentType)
		w.WriteHeader(http.StatusOK)
		io.Copy(w, reader)
	})
	mux.HandleFunc("POST /api/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		record, err := manager.RunTask(r.Context(), r.PathValue("id"), task.TriggerManual)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("POST /api/tasks/{id}/runs/{runID}/rerun", func(w http.ResponseWriter, r *http.Request) {
		record, err := manager.RunTask(r.Context(), r.PathValue("id"), task.TriggerRerun)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("POST /api/tasks/{id}/reload", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.ReloadTask(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "reloaded"})
	})
	mux.HandleFunc("POST /api/tasks/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.SetTaskEnabled(r.Context(), r.PathValue("id"), true); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "enabled"})
	})
	mux.HandleFunc("POST /api/tasks/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.SetTaskEnabled(r.Context(), r.PathValue("id"), false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "disabled"})
	})
	mux.HandleFunc("POST /api/tasks/batch", func(w http.ResponseWriter, r *http.Request) {
		var req batchTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		results := runBatchTaskAction(r.Context(), manager, req)
		writeJSON(w, http.StatusOK, map[string]any{
			"action":  req.Action,
			"results": results,
		})
	})

	mux.HandleFunc("GET /api/task-defs", func(w http.ResponseWriter, r *http.Request) {
		defs, err := repository.ListTaskDefinitions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if defs == nil {
			defs = []config.TaskDefinition{}
		}
		writeJSON(w, http.StatusOK, defs)
	})

	mux.HandleFunc("GET /api/task-defs/{id}", func(w http.ResponseWriter, r *http.Request) {
		def, err := repository.GetTaskDefinition(r.Context(), r.PathValue("id"))
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "task definition not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, def)
	})

	mux.HandleFunc("POST /api/task-defs/validate", func(w http.ResponseWriter, r *http.Request) {
		var def config.TaskDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeJSON(w, http.StatusBadRequest, taskValidationResponse{Valid: false, Errors: []string{"invalid body: " + err.Error()}})
			return
		}
		resp := validateTaskDefinition(manager, def)
		status := http.StatusOK
		if !resp.Valid {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, resp)
	})

	mux.HandleFunc("POST /api/task-defs/dry-run", func(w http.ResponseWriter, r *http.Request) {
		var def config.TaskDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeJSON(w, http.StatusBadRequest, taskValidationResponse{Valid: false, Errors: []string{"invalid body: " + err.Error()}})
			return
		}
		resp := validateTaskDefinition(manager, def)
		status := http.StatusOK
		if !resp.Valid {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, resp)
	})

	mux.HandleFunc("POST /api/task-defs/test-run", func(w http.ResponseWriter, r *http.Request) {
		var def config.TaskDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		record, err := manager.TestRunTaskDefinition(r.Context(), def)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"record": record, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, record)
	})

	mux.HandleFunc("POST /api/task-defs", func(w http.ResponseWriter, r *http.Request) {
		var def config.TaskDefinition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		if def.TaskID == "" {
			writeError(w, http.StatusBadRequest, "task_id is required")
			return
		}
		if def.Kind == "" {
			writeError(w, http.StatusBadRequest, "kind is required")
			return
		}
		if resp := validateTaskDefinition(manager, def); !resp.Valid {
			writeJSON(w, http.StatusBadRequest, resp)
			return
		}
		populateJSONBytes(&def)
		def.UpdatedAt = time.Now()
		def.CreatedAt = time.Now()
		if err := repository.InsertTaskDefinition(r.Context(), def); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if _, err := manager.UpsertTaskFromDB(r.Context(), def); err != nil {
			writeError(w, http.StatusInternalServerError, "task created but failed to start: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, def)
	})

	mux.HandleFunc("PUT /api/task-defs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		existing, err := repository.GetTaskDefinition(r.Context(), id)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "task definition not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var updated config.TaskDefinition
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		updated.TaskID = existing.TaskID
		updated.CreatedAt = existing.CreatedAt
		updated.UpdatedAt = time.Now()
		if updated.PipelineID == nil {
			updated.PipelineID = existing.PipelineID
		}
		if resp := validateTaskDefinition(manager, updated); !resp.Valid {
			writeJSON(w, http.StatusBadRequest, resp)
			return
		}
		populateJSONBytes(&updated)
		if err := repository.UpdateTaskDefinition(r.Context(), updated); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := manager.UpsertTaskFromDB(r.Context(), updated); err != nil {
			writeError(w, http.StatusInternalServerError, "task updated but failed to reload: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("DELETE /api/task-defs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := manager.RemoveTaskByID(r.Context(), id); err != nil {
			logger.WarnContext(r.Context(), "remove runtime task failed before deleting definition", "task_id", id, "err", err)
		}
		if err := repository.DeleteTaskDefinition(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("GET /api/pipelines", func(w http.ResponseWriter, r *http.Request) {
		pipelines, err := repository.ListPipelines(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if pipelines == nil {
			pipelines = []config.Pipeline{}
		}
		writeJSON(w, http.StatusOK, pipelines)
	})

	mux.HandleFunc("POST /api/pipelines", func(w http.ResponseWriter, r *http.Request) {
		var p config.Pipeline
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		if p.ID == "" {
			writeError(w, http.StatusBadRequest, "id is required")
			return
		}
		if p.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		p.UpdatedAt = time.Now()
		p.CreatedAt = time.Now()
		if err := repository.InsertPipeline(r.Context(), p); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	})

	mux.HandleFunc("GET /api/pipelines/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := repository.GetPipeline(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "pipeline not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	})

	mux.HandleFunc("PUT /api/pipelines/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		existing, err := repository.GetPipeline(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "pipeline not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var updated config.Pipeline
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		updated.ID = existing.ID
		updated.CreatedAt = existing.CreatedAt
		updated.UpdatedAt = time.Now()
		if err := repository.UpdatePipeline(r.Context(), updated); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("DELETE /api/pipelines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := repository.DeletePipeline(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("GET /api/pipelines/{id}/tasks", func(w http.ResponseWriter, r *http.Request) {
		defs, err := repository.ListTaskDefinitionsByPipeline(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if defs == nil {
			defs = []config.TaskDefinition{}
		}
		writeJSON(w, http.StatusOK, defs)
	})

	mux.HandleFunc("PUT /api/pipelines/{id}/tasks/{taskID}", func(w http.ResponseWriter, r *http.Request) {
		pipelineID := r.PathValue("id")
		taskID := r.PathValue("taskID")
		if err := repository.UpdateTaskPipeline(r.Context(), taskID, &pipelineID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "assigned", "pipeline_id": pipelineID, "task_id": taskID})
	})

	mux.HandleFunc("DELETE /api/pipelines/{id}/tasks/{taskID}", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("taskID")
		if err := repository.UpdateTaskPipeline(r.Context(), taskID, nil); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "unassigned", "task_id": taskID})
	})

	mux.HandleFunc("GET /api/task-graph", func(w http.ResponseWriter, r *http.Request) {
		pipelineID := r.URL.Query().Get("pipeline_id")
		graph, err := buildTaskGraph(r.Context(), manager, repository, pipelineID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, graph)
	})

	mux.HandleFunc("POST /api/task-dependencies", func(w http.ResponseWriter, r *http.Request) {
		var dep config.TaskDependency
		if err := json.NewDecoder(r.Body).Decode(&dep); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		if dep.Condition != "" {
			if err := config.ValidateWatchCondition(dep.Condition); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		saved, err := repository.UpsertTaskDependency(r.Context(), dep)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := reloadTaskDefinitionFromDB(r.Context(), manager, repository, saved.DownstreamTaskID); err != nil {
			writeError(w, http.StatusInternalServerError, "dependency saved but failed to reload downstream task: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	})

	mux.HandleFunc("DELETE /api/task-dependencies/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		deps, err := repository.ListTaskDependencies(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		downstreamTaskID := ""
		for _, dep := range deps {
			if dep.ID == id {
				downstreamTaskID = dep.DownstreamTaskID
				break
			}
		}
		if err := repository.DeleteTaskDependency(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if downstreamTaskID != "" {
			if err := reloadTaskDefinitionFromDB(r.Context(), manager, repository, downstreamTaskID); err != nil {
				writeError(w, http.StatusInternalServerError, "dependency deleted but failed to reload downstream task: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, err := repository.LoadGlobalSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settingsResponse{Settings: settings, Applied: true, Warnings: settingsWarnings(settings)})
	})

	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var updated config.GlobalSettings
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
		if err := repository.SaveGlobalSettings(r.Context(), updated); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		warnings := settingsWarnings(updated)
		applied := false
		if settingsReloader != nil {
			if pg, ok := repository.(*store.PostgresStore); ok {
				if err := settingsReloader.ReloadSinks(updated.Sinks, pg); err != nil {
					warnings = append(warnings, err.Error())
				} else {
					settingsReloader.SetMaxPayloadBytes(updated.MaxPayloadBytes)
					applied = true
				}
			} else {
				warnings = append(warnings, "settings saved but live reload is unavailable for this repository")
			}
		} else {
			warnings = append(warnings, "settings saved but live reload is unavailable")
		}
		writeJSON(w, http.StatusOK, settingsResponse{Settings: updated, Applied: applied, Warnings: warnings})
	})

	if staticDir != "" {
		fs := http.FileServer(http.Dir(staticDir))

		mux.Handle("GET /assets/{path...}", fs)

		mux.HandleFunc("GET /{path...}", func(w http.ResponseWriter, r *http.Request) {
			p := filepath.Clean(r.URL.Path)
			full := filepath.Join(staticDir, p)
			if _, err := os.Stat(full); os.IsNotExist(err) {
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
				return
			}
			fs.ServeHTTP(w, r)
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.With("method", r.Method, "path", r.URL.Path)
		mux.ServeHTTP(w, r.WithContext(appctx.WithLogger(r.Context(), reqLogger)))
	})
	return CORSMiddleware(handler)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeJSONCollections(value))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func refreshPluginStatus(ctx context.Context, manager TaskManager) {
	if refresher, ok := manager.(PluginStatusRefresher); ok {
		refresher.RefreshPluginStatus(ctx)
	}
}

var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

func normalizeJSONCollections(value any) any {
	if value == nil {
		return value
	}
	normalized := normalizeJSONValue(reflect.ValueOf(value))
	if !normalized.IsValid() {
		return value
	}
	return normalized.Interface()
}

func normalizeJSONValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Type().Implements(jsonMarshalerType) {
		return value
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		normalized := normalizeJSONValue(value.Elem())
		if normalized.IsValid() && normalized.Type().AssignableTo(value.Type()) {
			return normalized
		}
		return normalized
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		normalized := normalizeJSONValue(value.Elem())
		next := reflect.New(value.Type().Elem())
		if setNormalizedValue(next.Elem(), normalized) {
			return next
		}
		return value
	case reflect.Struct:
		return normalizeJSONStruct(value)
	case reflect.Map:
		return normalizeJSONMap(value)
	case reflect.Slice:
		return normalizeJSONSlice(value)
	case reflect.Array:
		return normalizeJSONArray(value)
	default:
		return value
	}
}

func normalizeJSONStruct(value reflect.Value) reflect.Value {
	next := reflect.New(value.Type()).Elem()
	next.Set(value)
	for i := 0; i < value.NumField(); i++ {
		fieldInfo := value.Type().Field(i)
		if fieldInfo.PkgPath != "" {
			continue
		}
		setNormalizedValue(next.Field(i), normalizeJSONValue(value.Field(i)))
	}
	return next
}

func normalizeJSONMap(value reflect.Value) reflect.Value {
	if value.IsNil() {
		return reflect.MakeMapWithSize(value.Type(), 0)
	}
	next := reflect.MakeMapWithSize(value.Type(), value.Len())
	iter := value.MapRange()
	for iter.Next() {
		normalized := normalizeJSONValue(iter.Value())
		if !normalized.IsValid() {
			next.SetMapIndex(iter.Key(), iter.Value())
			continue
		}
		if normalized.Type().AssignableTo(value.Type().Elem()) {
			next.SetMapIndex(iter.Key(), normalized)
		} else if normalized.Type().ConvertibleTo(value.Type().Elem()) {
			next.SetMapIndex(iter.Key(), normalized.Convert(value.Type().Elem()))
		} else {
			next.SetMapIndex(iter.Key(), iter.Value())
		}
	}
	return next
}

func normalizeJSONSlice(value reflect.Value) reflect.Value {
	if value.Type().Elem().Kind() == reflect.Uint8 {
		return value
	}
	if value.IsNil() {
		return reflect.MakeSlice(value.Type(), 0, 0)
	}
	next := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
	for i := 0; i < value.Len(); i++ {
		setNormalizedValue(next.Index(i), normalizeJSONValue(value.Index(i)))
	}
	return next
}

func normalizeJSONArray(value reflect.Value) reflect.Value {
	next := reflect.New(value.Type()).Elem()
	for i := 0; i < value.Len(); i++ {
		setNormalizedValue(next.Index(i), normalizeJSONValue(value.Index(i)))
	}
	return next
}

func setNormalizedValue(dst, src reflect.Value) bool {
	if !dst.CanSet() || !src.IsValid() {
		return false
	}
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return true
	}
	if src.Type().ConvertibleTo(dst.Type()) {
		dst.Set(src.Convert(dst.Type()))
		return true
	}
	return false
}

func buildTaskViews(ctx context.Context, manager TaskManager, repository store.Repository, taskID string) ([]taskView, error) {
	defs, err := repository.ListTaskDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	deps, err := repository.ListTaskDependencies(ctx)
	if err != nil {
		return nil, err
	}
	stateMap := map[string]store.TaskState{}
	for _, state := range manager.ListTasks() {
		stateMap[state.TaskID] = state
	}
	defMap := map[string]*config.TaskDefinition{}
	for i := range defs {
		defMap[defs[i].TaskID] = &defs[i]
	}
	depIndex := buildDependencyIndex(defs, deps)
	taskIDs := make([]string, 0, len(defs)+len(stateMap))
	for _, def := range defs {
		taskIDs = append(taskIDs, def.TaskID)
	}
	for taskID := range stateMap {
		if _, ok := defMap[taskID]; !ok {
			taskIDs = append(taskIDs, taskID)
		}
	}
	consecutiveFailures, err := repository.ListConsecutiveFailures(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	views := make([]taskView, 0, len(defs)+len(stateMap))
	for i := range defs {
		def := &defs[i]
		if taskID != "" && def.TaskID != taskID {
			continue
		}
		state, ok := stateMap[def.TaskID]
		if !ok {
			state = emptyStateFromDefinition(*def)
		}
		views = append(views, buildTaskView(state, def, depIndex, consecutiveFailures[state.TaskID]))
		delete(stateMap, def.TaskID)
	}
	for _, state := range stateMap {
		if taskID != "" && state.TaskID != taskID {
			continue
		}
		views = append(views, buildTaskView(state, defMap[state.TaskID], depIndex, consecutiveFailures[state.TaskID]))
	}
	sortTaskViews(views)
	return views, nil
}

type dependencyIndex struct {
	upstreamByTask   map[string][]config.TaskDependency
	downstreamByTask map[string][]config.TaskDependency
}

func buildDependencyIndex(defs []config.TaskDefinition, deps []config.TaskDependency) dependencyIndex {
	index := dependencyIndex{
		upstreamByTask:   map[string][]config.TaskDependency{},
		downstreamByTask: map[string][]config.TaskDependency{},
	}
	for _, dep := range deps {
		index.upstreamByTask[dep.DownstreamTaskID] = append(index.upstreamByTask[dep.DownstreamTaskID], dep)
		index.downstreamByTask[dep.UpstreamTaskID] = append(index.downstreamByTask[dep.UpstreamTaskID], dep)
	}
	for _, def := range defs {
		if def.Trigger != "on_run" || def.WatchTaskID == "" {
			continue
		}
		legacy := config.TaskDependency{
			ID:               "legacy:" + def.WatchTaskID + ":" + def.TaskID,
			UpstreamTaskID:   def.WatchTaskID,
			DownstreamTaskID: def.TaskID,
			Condition:        def.WatchCondition,
		}
		index.upstreamByTask[def.TaskID] = append(index.upstreamByTask[def.TaskID], legacy)
		index.downstreamByTask[def.WatchTaskID] = append(index.downstreamByTask[def.WatchTaskID], legacy)
	}
	return index
}

func buildTaskView(state store.TaskState, def *config.TaskDefinition, depIndex dependencyIndex, consecutiveFailures int) taskView {
	if state.Labels == nil {
		state.Labels = map[string]string{}
	}
	view := taskView{
		TaskState:    state,
		ConfigStatus: "valid",
		Runtime: taskRuntimeView{
			Status:          state.Status,
			LastRunStatus:   state.LastRunStatus,
			LastCheckStatus: state.LastCheckStatus,
			LastRunAt:       state.LastRunAt,
			NextRunAt:       state.NextRunAt,
			LastDurationMS:  state.LastDurationMS,
			LastError:       state.LastError,
		},
		Definition: def,
	}
	if def != nil {
		view.Dependencies = depIndex.upstreamByTask[def.TaskID]
		if def.PipelineID != nil {
			view.Dependency.PipelineID = *def.PipelineID
		}
	}
	view.Runtime.ConsecutiveFailures = consecutiveFailures
	upstreams := depIndex.upstreamByTask[state.TaskID]
	downstreams := depIndex.downstreamByTask[state.TaskID]
	view.Dependency.UpstreamCount = len(upstreams)
	view.Dependency.DownstreamCount = len(downstreams)
	if len(upstreams) > 0 {
		view.Dependency.UpstreamTaskID = upstreams[0].UpstreamTaskID
		view.Dependency.WatchCondition = upstreams[0].Condition
		view.Dependency.DependencyStatus = "configured"
	}
	if state.LastReloadError != "" {
		view.ConfigStatus = "load_error"
		view.LoadError = state.LastReloadError
	} else if state.Status == "unloaded" {
		view.ConfigStatus = "missing_runtime"
	}
	return view
}

func emptyStateFromDefinition(def config.TaskDefinition) store.TaskState {
	labels := def.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return store.TaskState{
		TaskID:    def.TaskID,
		Name:      def.Name,
		Kind:      def.Kind,
		Enabled:   def.Enabled,
		Status:    "unloaded",
		Labels:    labels,
		UpdatedAt: def.UpdatedAt,
	}
}

func filterTaskViews(r *http.Request, views []taskView) []taskView {
	query := r.URL.Query()
	search := strings.ToLower(strings.TrimSpace(query.Get("search")))
	kind := query.Get("kind")
	status := query.Get("status")
	runStatus := query.Get("run_status")
	checkStatus := query.Get("check_status")
	enabled := query.Get("enabled")
	result := views[:0]
	for _, view := range views {
		if search != "" {
			haystack := strings.ToLower(strings.Join([]string{
				view.TaskID,
				view.Name,
				view.Kind,
				view.LastError,
				view.LastReloadError,
				view.LoadError,
			}, "\n"))
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		if kind != "" && view.Kind != kind {
			continue
		}
		if status != "" && view.Status != status {
			continue
		}
		if runStatus != "" && view.LastRunStatus != runStatus {
			continue
		}
		if checkStatus != "" && view.LastCheckStatus != checkStatus {
			continue
		}
		if enabled == "true" && !view.Enabled {
			continue
		}
		if enabled == "false" && view.Enabled {
			continue
		}
		matchedLabels := true
		for key, values := range query {
			if !strings.HasPrefix(key, "label.") || len(values) == 0 || values[0] == "" {
				continue
			}
			labelKey := strings.TrimPrefix(key, "label.")
			if view.Labels[labelKey] != values[0] {
				matchedLabels = false
				break
			}
		}
		if !matchedLabels {
			continue
		}
		result = append(result, view)
	}
	return result
}

func sortTaskViews(views []taskView) {
	severity := func(view taskView) int {
		if !view.Enabled || view.Status == "disabled" {
			return 3
		}
		if view.LastReloadError != "" || view.LastRunStatus == "failed" || view.LastRunStatus == "timeout" || view.LastCheckStatus == "fail" {
			return 0
		}
		if view.Status == "unloaded" || isStaleTask(view.TaskState) {
			return 1
		}
		return 2
	}
	latest := func(view taskView) time.Time {
		if view.LastRunAt != nil {
			return *view.LastRunAt
		}
		return view.UpdatedAt
	}
	sort.Slice(views, func(i, j int) bool {
		left, right := severity(views[i]), severity(views[j])
		if left != right {
			return left < right
		}
		return latest(views[i]).After(latest(views[j]))
	})
}

func buildDashboardSummary(views []taskView, recentRuns []store.RunListItem) dashboardSummary {
	counts := dashboardCounts{Total: len(views)}
	anomalies := make([]taskView, 0)
	for _, view := range views {
		if view.Enabled {
			counts.Enabled++
		}
		if !view.Enabled || view.Status == "disabled" {
			counts.Disabled++
		}
		if view.LastRunStatus == "failed" || view.LastRunStatus == "timeout" {
			counts.Failed++
		}
		if view.LastCheckStatus == "fail" {
			counts.CheckFailed++
		}
		if view.LastReloadError != "" {
			counts.LoadFailed++
		}
		if view.Enabled && isStaleTask(view.TaskState) {
			counts.Stale++
		}
		if taskIsAbnormal(view) {
			anomalies = append(anomalies, view)
		}
	}
	if len(anomalies) > 12 {
		anomalies = anomalies[:12]
	}
	if recentRuns == nil {
		recentRuns = []store.RunListItem{}
	}
	return dashboardSummary{
		Counts:       counts,
		Health:       dashboardHealthFromCounts(counts),
		Anomalies:    anomalies,
		RecentRuns:   recentRuns,
		LabelGroups:  aggregateTaskLabels(views),
		GeneratedAt:  time.Now(),
		RefreshAfter: "15s",
	}
}

func dashboardHealthFromCounts(counts dashboardCounts) dashboardHealth {
	if counts.LoadFailed > 0 {
		return dashboardHealth{Status: "config_error", Label: "存在配置加载错误", Detail: "至少一个任务定义无法加载到运行态"}
	}
	if counts.Failed > 0 || counts.CheckFailed > 0 {
		return dashboardHealth{Status: "failed", Label: "存在失败任务", Detail: "需要优先处理失败、超时或检查未通过任务"}
	}
	if counts.Stale > 0 {
		return dashboardHealth{Status: "warning", Label: "存在待确认任务", Detail: "存在长时间未运行任务"}
	}
	return dashboardHealth{Status: "ok", Label: "正常", Detail: "当前未发现需要立即处理的任务"}
}

func aggregateTaskLabels(views []taskView) []labelAggregate {
	keys := []string{"env", "service", "kind"}
	items := map[string]labelAggregate{}
	for _, view := range views {
		for _, key := range keys {
			value := view.Kind
			if key != "kind" {
				value = view.Labels[key]
			}
			if value == "" {
				continue
			}
			mapKey := key + ":" + value
			item := items[mapKey]
			item.Key = key
			item.Value = value
			item.Total++
			if taskIsAbnormal(view) {
				item.Abnormal++
			}
			items[mapKey] = item
		}
	}
	result := make([]labelAggregate, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Abnormal != result[j].Abnormal {
			return result[i].Abnormal > result[j].Abnormal
		}
		return result[i].Total > result[j].Total
	})
	return result
}

func taskIsAbnormal(view taskView) bool {
	return view.LastReloadError != "" ||
		view.LastRunStatus == "failed" ||
		view.LastRunStatus == "timeout" ||
		view.LastCheckStatus == "fail" ||
		view.Status == "unloaded" ||
		(view.Enabled && isStaleTask(view.TaskState))
}

func isStaleTask(state store.TaskState) bool {
	if !state.Enabled || state.LastRunAt == nil {
		return false
	}
	return time.Since(*state.LastRunAt) > 24*time.Hour
}

func parseRunQuery(r *http.Request) store.RunQuery {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	offset, _ := strconv.Atoi(query.Get("offset"))
	runQuery := store.RunQuery{
		TaskID:      query.Get("task_id"),
		Kind:        query.Get("kind"),
		RunStatus:   query.Get("run_status"),
		CheckStatus: query.Get("check_status"),
		Since:       parseDurationQuery(r, "since", 0),
		Limit:       limit,
		Offset:      offset,
		Labels:      map[string]string{},
	}
	for key, values := range query {
		if !strings.HasPrefix(key, "label.") || len(values) == 0 || values[0] == "" {
			continue
		}
		runQuery.Labels[strings.TrimPrefix(key, "label.")] = values[0]
	}
	return runQuery
}

func parseDurationQuery(r *http.Request, key string, fallback time.Duration) time.Duration {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

func runBatchTaskAction(ctx context.Context, manager TaskManager, req batchTaskRequest) []batchTaskResult {
	results := make([]batchTaskResult, 0, len(req.TaskIDs))
	for _, taskID := range req.TaskIDs {
		result := batchTaskResult{TaskID: taskID}
		switch req.Action {
		case "run":
			run, err := manager.RunTask(ctx, taskID, task.TriggerManual)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.OK = true
				result.Run = &run
			}
		case "enable":
			if err := manager.SetTaskEnabled(ctx, taskID, true); err != nil {
				result.Error = err.Error()
			} else {
				result.OK = true
			}
		case "disable":
			if err := manager.SetTaskEnabled(ctx, taskID, false); err != nil {
				result.Error = err.Error()
			} else {
				result.OK = true
			}
		case "reload":
			if err := manager.ReloadTask(ctx, taskID); err != nil {
				result.Error = err.Error()
			} else {
				result.OK = true
			}
		default:
			result.Error = "unsupported action"
		}
		results = append(results, result)
	}
	return results
}

func reloadTaskDefinitionFromDB(ctx context.Context, manager TaskManager, repository store.Repository, taskID string) error {
	def, err := repository.GetTaskDefinition(ctx, taskID)
	if err != nil {
		return err
	}
	_, err = manager.UpsertTaskFromDB(ctx, *def)
	return err
}

func validateTaskDefinition(manager TaskManager, def config.TaskDefinition) taskValidationResponse {
	populateJSONBytes(&def)
	spec, err := manager.ValidateTaskDefinition(def)
	if err != nil {
		return taskValidationResponse{Valid: false, Errors: []string{err.Error()}}
	}
	return taskValidationResponse{Valid: true, Errors: []string{}, Normalized: spec}
}

func buildTaskGraph(ctx context.Context, manager TaskManager, repository store.Repository, pipelineID string) (taskGraph, error) {
	views, err := buildTaskViews(ctx, manager, repository, "")
	if err != nil {
		return taskGraph{}, err
	}
	nodes := make([]taskGraphNode, 0, len(views))
	taskIDs := map[string]struct{}{}
	for _, view := range views {
		if pipelineID != "" && view.Dependency.PipelineID != pipelineID {
			continue
		}
		taskIDs[view.TaskID] = struct{}{}
		nodes = append(nodes, taskGraphNode{
			TaskID:       view.TaskID,
			Name:         view.Name,
			Kind:         view.Kind,
			Enabled:      view.Enabled,
			Labels:       view.Labels,
			PipelineID:   view.Dependency.PipelineID,
			ConfigStatus: view.ConfigStatus,
			Runtime:      view.Runtime,
		})
	}
	deps, err := repository.ListTaskDependencies(ctx)
	if err != nil {
		return taskGraph{}, err
	}
	var defs []config.TaskDefinition
	for _, view := range views {
		if view.Definition != nil {
			defs = append(defs, *view.Definition)
		}
	}
	depIndex := buildDependencyIndex(defs, deps)
	edgeMap := map[string]taskGraphEdge{}
	for _, list := range depIndex.upstreamByTask {
		for _, dep := range list {
			if pipelineID != "" {
				if _, ok := taskIDs[dep.UpstreamTaskID]; !ok {
					if _, ok := taskIDs[dep.DownstreamTaskID]; !ok {
						continue
					}
				}
			}
			edge := dependencyToGraphEdge(dep, taskIDs)
			if strings.HasPrefix(dep.ID, "legacy:") {
				edge.Legacy = true
			}
			edgeMap[edge.ID] = edge
		}
	}
	edges := make([]taskGraphEdge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return taskGraph{Nodes: nodes, Edges: edges}, nil
}

func dependencyToGraphEdge(dep config.TaskDependency, taskIDs map[string]struct{}) taskGraphEdge {
	id := dep.ID
	if id == "" {
		id = dep.UpstreamTaskID + "->" + dep.DownstreamTaskID
	}
	edge := taskGraphEdge{
		ID:               id,
		UpstreamTaskID:   dep.UpstreamTaskID,
		DownstreamTaskID: dep.DownstreamTaskID,
		Condition:        dep.Condition,
		SourceKey:        dep.SourceKey,
		Params:           dep.Params,
		Valid:            true,
	}
	if _, ok := taskIDs[dep.UpstreamTaskID]; !ok && len(taskIDs) > 0 {
		edge.Valid = false
		edge.Error = "upstream task not found"
	}
	if err := config.ValidateWatchCondition(dep.Condition); err != nil {
		edge.Valid = false
		edge.Error = err.Error()
	}
	return edge
}

func settingsWarnings(settings config.GlobalSettings) []string {
	var warnings []string
	hasPostgres := false
	for _, sink := range settings.Sinks {
		if sink.Kind == "postgres" {
			hasPostgres = true
		}
		if sink.Kind == "webhook" && sink.URL == "" {
			warnings = append(warnings, "webhook sink "+sink.Name+" has empty url")
		}
	}
	if !hasPostgres {
		warnings = append(warnings, "at least one postgres sink is required for durable trace records")
	}
	if settings.MaxPayloadBytes <= 0 {
		warnings = append(warnings, "max_payload_bytes should be positive")
	}
	if settings.DefaultRetainDays > 0 && settings.DefaultRetainDays < 7 {
		warnings = append(warnings, "default_retain_days is shorter than 7 days")
	}
	return warnings
}

func populateJSONBytes(def *config.TaskDefinition) {
	def.LabelsJSON = marshalOrDefault(def.Labels)
	def.ParamsJSON = marshalOrDefault(def.Params)
	def.TraceJSON = marshalOrDefault(def.Trace)
	def.AlertJSON = marshalOrDefault(def.Alert)
}

func marshalOrDefault(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return []byte("{}")
	}
	return b
}

func hydrateRunPayloadFromArtifact(ctx context.Context, record *store.RunRecord, artifactStore store.ArtifactStore) error {
	if record == nil || len(record.Payload) > 0 || artifactStore == nil {
		return nil
	}
	for _, artifact := range record.ArtifactRefs {
		if artifact.Kind != "payload" {
			continue
		}
		key, err := store.ObjectKeyFromURI(artifact.URI)
		if err != nil {
			return err
		}
		reader, err := artifactStore.Get(ctx, key)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if json.Valid(raw) {
			record.Payload = raw
			return nil
		}
		encoded, err := json.Marshal(string(raw))
		if err != nil {
			return err
		}
		record.Payload = encoded
		return nil
	}
	return nil
}
