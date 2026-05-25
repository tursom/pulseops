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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pulseops/internal/appctx"
	"pulseops/internal/config"
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
	RemoveTaskByID(ctx context.Context, taskID string) error
}

func Routes(staticDir string, manager TaskManager, repository store.Repository, artifactStore store.ArtifactStore, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// API routes — register before static/file routes for specificity priority
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		tasks := manager.ListTasks()
		if tasks == nil {
			tasks = []store.TaskState{}
		}
		writeJSON(w, http.StatusOK, tasks)
	})
	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		taskState, ok := manager.GetTask(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		def, err := repository.GetTaskDefinition(r.Context(), r.PathValue("id"))
		if err != nil && err != sql.ErrNoRows {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if def != nil {
			if len(def.LabelsJSON) > 0 {
				json.Unmarshal(def.LabelsJSON, &def.Labels)
			}
			if len(def.ParamsJSON) > 0 {
				json.Unmarshal(def.ParamsJSON, &def.Params)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task_id":            taskState.TaskID,
			"name":               taskState.Name,
			"kind":               taskState.Kind,
			"enabled":            taskState.Enabled,
			"status":             taskState.Status,
			"labels":             taskState.Labels,
			"last_run_at":        taskState.LastRunAt,
			"next_run_at":        taskState.NextRunAt,
			"last_run_status":    taskState.LastRunStatus,
			"last_check_status":  taskState.LastCheckStatus,
			"last_error":         taskState.LastError,
			"last_duration_ms":   taskState.LastDurationMS,
			"last_reload_error":  taskState.LastReloadError,
			"last_sample_seed":   taskState.LastSampleSeed,
			"last_sample_count":  taskState.LastSampleCount,
			"last_mismatch_count": taskState.LastMismatchCount,
			"source_path":        taskState.SourcePath,
			"updated_at":         taskState.UpdatedAt,
			"definition":         def,
		})
	})
	mux.HandleFunc("GET /api/tasks/{id}/sample", func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")
		if source == "" || (source != "payload" && source != "summary" && source != "record") {
			writeError(w, http.StatusBadRequest, `query param "source" required: payload, summary, or record`)
			return
		}
		resp, err := task.FetchSampleData(r.Context(), repository, r.PathValue("id"), source)
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
		records, err := repository.ListRuns(r.Context(), r.PathValue("id"), limit, offset, since)
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
			records = []store.RunRecord{}
		}
		writeJSON(w, http.StatusOK, store.PaginatedRuns{Records: records, Total: total})
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
			writeError(w, http.StatusNotFound, err.Error())
			return
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

	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, err := repository.LoadGlobalSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
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
		writeJSON(w, http.StatusOK, updated)
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
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
