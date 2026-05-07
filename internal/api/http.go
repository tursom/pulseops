package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
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
}

func NewServer(cfg config.ServerConfig, manager TaskManager, repository store.Repository, artifactStore store.ArtifactStore, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, manager.ListTasks())
	})
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		taskState, ok := manager.GetTask(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSON(w, http.StatusOK, taskState)
	})
	mux.HandleFunc("GET /tasks/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		records, err := repository.ListRuns(r.Context(), r.PathValue("id"), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
	mux.HandleFunc("GET /tasks/{id}/runs/{runID}", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET /tasks/{id}/runs/{runID}/ai", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET /tasks/{id}/ai", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		records, err := repository.ListAIAnalyses(r.Context(), r.PathValue("id"), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
	mux.HandleFunc("GET /tasks/{id}/runs/{runID}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		artifacts, err := repository.ListArtifactsByRun(r.Context(), r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, artifacts)
	})
	mux.HandleFunc("GET /artifacts/{artifactID}", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		record, err := manager.RunTask(r.Context(), r.PathValue("id"), task.TriggerManual)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("POST /tasks/{id}/runs/{runID}/rerun", func(w http.ResponseWriter, r *http.Request) {
		record, err := manager.RunTask(r.Context(), r.PathValue("id"), task.TriggerRerun)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
	mux.HandleFunc("POST /tasks/{id}/reload", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.ReloadTask(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "reloaded"})
	})
	mux.HandleFunc("POST /tasks/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.SetTaskEnabled(r.Context(), r.PathValue("id"), true); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "enabled"})
	})
	mux.HandleFunc("POST /tasks/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.SetTaskEnabled(r.Context(), r.PathValue("id"), false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "disabled"})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.With("method", r.Method, "path", r.URL.Path)
		mux.ServeHTTP(w, r.WithContext(appctx.WithLogger(r.Context(), reqLogger)))
	})
	return &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout.Duration,
		WriteTimeout: cfg.WriteTimeout.Duration,
		BaseContext: func(_ net.Listener) context.Context {
			return appctx.WithLogger(context.Background(), logger)
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
