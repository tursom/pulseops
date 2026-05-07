package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

// C ABI contract for data source plugins.
//
// Every .so plugin must export exactly three C symbols:
//
//	char* plugin_name(void)
//	  Returns the data source name. Never returns NULL.
//
//	char* plugin_fetch(const char* config_json)
//	  Receives JSON config, returns JSON result:
//	    {"data": <any>}       on success
//	    {"error": "message"}  on failure
//	  Returns NULL only on fatal error (OOM).
//
//	void  plugin_free(void* ptr)
//	  Frees memory returned by plugin_name or plugin_fetch.
//	  Safe to call with NULL (no-op).
//
// All strings are null-terminated UTF-8. Memory returned by the plugin
// is owned by the plugin; call plugin_free to release it.

type cSource struct {
	name    string
	handle  uintptr
	fetchFn func(configPtr uintptr) uintptr
	freeFn  func(ptr uintptr)
}

func (s *cSource) Name() string { return s.name }

func (s *cSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	configJSON, err := json.Marshal(spec.Config)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: marshal config: %w", s.name, err)
	}

	configPtr := goBytesToC(string(configJSON))
	defer runtime.KeepAlive(configJSON)

	resultPtr := s.fetchFn(configPtr)
	if resultPtr == 0 {
		return nil, fmt.Errorf("plugin %s: plugin_fetch returned NULL (fatal)", s.name)
	}

	resultJSON := cstring(resultPtr)
	s.freeFn(resultPtr)

	var outcome struct {
		Data  any    `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &outcome); err != nil {
		return nil, fmt.Errorf("plugin %s: parse response: %w (raw: %s)", s.name, err, resultJSON)
	}
	if outcome.Error != "" {
		return nil, fmt.Errorf("plugin %s: %s", s.name, outcome.Error)
	}
	return outcome.Data, nil
}

type PluginManager struct {
	pluginDir string
	logger    *slog.Logger
}

func NewPluginManager(pluginDir string, logger *slog.Logger) *PluginManager {
	return &PluginManager{pluginDir: pluginDir, logger: logger}
}

func (m *PluginManager) LoadPlugins(registry *DataSourceRegistry) error {
	info, err := os.Stat(m.pluginDir)
	if err != nil {
		return fmt.Errorf("plugin directory not accessible: %s: %w", m.pluginDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin path is not a directory: %s", m.pluginDir)
	}

	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugin directory %s: %w", m.pluginDir, err)
	}

	soCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}
		soCount++

		soPath := filepath.Join(m.pluginDir, entry.Name())
		source, err := m.loadOne(soPath)
		if err != nil {
			m.logger.Warn("failed to load plugin, skipping", "path", soPath, "error", err)
			continue
		}

		registry.Register(source.Name(), source)
		m.logger.Info("loaded plugin data source", "name", source.Name(), "path", soPath)
	}

	if soCount == 0 {
		m.logger.Debug("no .so plugins found in directory", "dir", m.pluginDir)
	}

	return nil
}

func (m *PluginManager) loadOne(soPath string) (*cSource, error) {
	handle, err := purego.Dlopen(soPath, purego.RTLD_NOW)
	if err != nil {
		return nil, fmt.Errorf("dlopen: %w", err)
	}

	lookup := func(symbol string, fnPtr any) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("lookup %s: %v", symbol, r)
				purego.Dlclose(handle)
			}
		}()
		purego.RegisterLibFunc(fnPtr, handle, symbol)
		return nil
	}

	var nameFn func() uintptr
	if err := lookup("plugin_name", &nameFn); err != nil {
		return nil, err
	}

	var fetchFn func(configPtr uintptr) uintptr
	if err := lookup("plugin_fetch", &fetchFn); err != nil {
		return nil, err
	}

	var freeFn func(ptr uintptr)
	if err := lookup("plugin_free", &freeFn); err != nil {
		return nil, err
	}

	namePtr := nameFn()
	if namePtr == 0 {
		purego.Dlclose(handle)
		return nil, fmt.Errorf("plugin_name returned NULL")
	}
	name := cstring(namePtr)
	freeFn(namePtr)

	if name == "" {
		purego.Dlclose(handle)
		return nil, fmt.Errorf("plugin_name returned empty string")
	}

	return &cSource{
		name:    name,
		handle:  handle,
		fetchFn: fetchFn,
		freeFn:  freeFn,
	}, nil
}

func cstring(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var n int
	for *(*byte)(unsafe.Pointer(ptr + uintptr(n))) != 0 {
		n++
	}
	return unsafe.String((*byte)(unsafe.Pointer(ptr)), n)
}

func goBytesToC(s string) uintptr {
	if s == "" {
		return 0
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	b[len(s)] = 0
	return uintptr(unsafe.Pointer(&b[0]))
}
