package watch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"pulseops/internal/runtime"
)

type Watcher struct {
	dir      string
	debounce time.Duration
	manager  *runtime.Manager
	logger   *slog.Logger

	mu      sync.Mutex
	timers  map[string]*time.Timer
	watcher *fsnotify.Watcher
}

func New(dir string, debounce time.Duration, manager *runtime.Manager, logger *slog.Logger) *Watcher {
	return &Watcher{
		dir:      dir,
		debounce: debounce,
		manager:  manager,
		logger:   logger,
		timers:   map[string]*time.Timer{},
	}
}

func (w *Watcher) Start(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := fsw.Add(w.dir); err != nil {
		_ = fsw.Close()
		return err
	}
	w.watcher = fsw
	go w.loop(ctx)
	return nil
}

func (w *Watcher) Close() error {
	w.mu.Lock()
	for _, timer := range w.timers {
		timer.Stop()
	}
	w.timers = map[string]*time.Timer{}
	watcher := w.watcher
	w.mu.Unlock()
	if watcher != nil {
		return watcher.Close()
	}
	return nil
}

func (w *Watcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(event.Name, ".toml") {
				continue
			}
			w.schedule(filepath.Clean(event.Name))
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.ErrorContext(ctx, "task watcher error", "err", err)
		}
	}
}

func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer := w.timers[path]; timer != nil {
		timer.Reset(w.debounce)
		return
	}
	w.timers[path] = time.AfterFunc(w.debounce, func() {
		defer func() {
			w.mu.Lock()
			delete(w.timers, path)
			w.mu.Unlock()
		}()
		if !strings.HasSuffix(path, ".toml") {
			return
		}
		if _, err := os.Stat(path); err == nil {
			if upsertErr := w.manager.UpsertTaskFromPath(context.Background(), path); upsertErr != nil {
				w.logger.Error("reload task config failed", "path", path, "err", upsertErr)
			}
			return
		}
		if removeErr := w.manager.RemoveTaskByPath(context.Background(), path); removeErr != nil {
			w.logger.Error("remove task config failed", "path", path, "err", removeErr)
		}
	})
}
