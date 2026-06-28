package plugin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"pulseops/internal/config"
	"pulseops/internal/task"
)

func TestManagerActivatesExternalProcessTaskDriver(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := filepath.Join(pluginDir, "external-driver", "releases", "1.0.0")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version = "pulseops.plugin/v1"
id = "external-driver"
name = "External Driver"
version = "1.0.0"
enabled = true

[[task_drivers]]
name = "external_check"
title = "External Check"
runtime = "process"
entrypoint = "driver.sh"

[task_drivers.schema]
target = { type = "string", required = true }
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	script := `#!/bin/sh
input=$(cat)
case "$input" in
  *'"action":"validate_runtime"'*) echo '{"ok":true,"data":{"ready":true}}' ;;
  *'"action":"validate"'*) echo '{"ok":true,"data":{"valid":true}}' ;;
  *'"action":"run"'*) echo '{"ok":true,"data":{"check_status":"pass","summary":{"driver":"external"},"payload":{"ok":true}}}' ;;
  *) echo '{"ok":false,"error":{"message":"bad action"}}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(releaseDir, "driver.sh"), []byte(script), 0755); err != nil {
		t.Fatalf("write driver: %v", err)
	}

	store := newMemoryPluginStore()
	manager := NewManager(Options{
		BaseDir: t.TempDir(),
		Config:  config.PluginsConfig{Dir: pluginDir},
		Store:   store,
	})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.0.0"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	drivers, generationID := manager.ActiveDriverRegistry()
	if generationID == "" {
		t.Fatal("expected active generation id")
	}
	driver, ok := drivers.Get("external_check")
	if !ok {
		t.Fatal("expected external_check driver in active registry")
	}
	spec := config.TaskSpec{
		ID:   "external-task",
		Kind: "external_check",
		Params: map[string]any{
			"target": "steam",
		},
	}
	if err := driver.Validate(spec); err != nil {
		t.Fatalf("driver validate: %v", err)
	}
	runner, err := driver.NewRunner(spec, task.RunnerDeps{})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), task.TriggerManual)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.CheckStatus != "pass" || result.Summary["driver"] != "external" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestManagerRejectsMutatedReleaseChecksum(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := writeTestRelease(t, pluginDir, "external-driver", "1.0.0")
	store := newMemoryPluginStore()
	manager := NewManager(Options{Config: config.PluginsConfig{Dir: pluginDir}, Store: store})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "driver.sh"), []byte("#!/bin/sh\necho tampered\n"), 0755); err != nil {
		t.Fatalf("tamper release: %v", err)
	}
	if _, err := manager.ValidateRelease(context.Background(), "external-driver", "1.0.0"); err == nil {
		t.Fatal("expected checksum validation error")
	}
}

func TestManagerExportImportRelease(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	releaseDir := writeTestRelease(t, sourceDir, "external-driver", "1.0.0")
	checksum, err := ReleaseChecksum(releaseDir)
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, ReleaseChecksumFile), []byte(checksum+"  .\n"), 0644); err != nil {
		t.Fatalf("write checksum sidecar: %v", err)
	}
	sourceStore := newMemoryPluginStore()
	sourceManager := NewManager(Options{Config: config.PluginsConfig{Dir: sourceDir}, Store: sourceStore})
	if err := sourceManager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize source: %v", err)
	}
	filename, archive, err := sourceManager.ExportRelease(context.Background(), "external-driver", "1.0.0")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if filename == "" || len(archive) == 0 {
		t.Fatalf("unexpected export filename=%q len=%d", filename, len(archive))
	}

	targetStore := newMemoryPluginStore()
	targetManager := NewManager(Options{Config: config.PluginsConfig{Dir: t.TempDir()}, Store: targetStore})
	release, err := targetManager.ImportRelease(context.Background(), bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if release.PluginID != "external-driver" || release.Version != "1.0.0" || release.Checksum == "" {
		t.Fatalf("unexpected release: %#v", release)
	}
	if _, err := os.Stat(filepath.Join(release.Path, ManifestFilename)); err != nil {
		t.Fatalf("expected imported manifest: %v", err)
	}
}

func TestManagerVerifiesReleaseSignature(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := writeTestRelease(t, pluginDir, "external-driver", "1.0.0")
	checksum, err := ReleaseChecksum(releaseDir)
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, ReleaseChecksumFile), []byte(checksum+"\n"), 0644); err != nil {
		t.Fatalf("write checksum: %v", err)
	}
	key := "test-signature-key"
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(checksum))
	if err := os.WriteFile(filepath.Join(releaseDir, ReleaseSignatureFile), []byte(hex.EncodeToString(mac.Sum(nil))+"\n"), 0644); err != nil {
		t.Fatalf("write signature: %v", err)
	}
	manager := NewManager(Options{Config: config.PluginsConfig{Dir: pluginDir, SignatureKey: key}, Store: newMemoryPluginStore()})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize with signature: %v", err)
	}

	badManager := NewManager(Options{Config: config.PluginsConfig{Dir: pluginDir, Strict: true, SignatureKey: "wrong"}, Store: newMemoryPluginStore()})
	if err := badManager.Initialize(context.Background()); err == nil {
		t.Fatal("expected signature validation error")
	}
}

func TestManagerRejectsPermissionOutsideAllowlist(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := filepath.Join(pluginDir, "template-plugin", "releases", "1.0.0")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version = "pulseops.plugin/v1"
id = "template-plugin"
name = "Template Plugin"
version = "1.0.0"
enabled = true
permissions = ["tasks:write"]

[[task_templates]]
id = "external-template"
kind = "http_check"
title = "External Template"
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manager := NewManager(Options{
		Config: config.PluginsConfig{
			Dir:                pluginDir,
			Strict:             true,
			AllowedPermissions: []string{"runs:read"},
		},
		Store: newMemoryPluginStore(),
	})
	if err := manager.Initialize(context.Background()); err == nil {
		t.Fatal("expected permission allowlist validation error")
	}
}

func TestManagerRejectsCABIEntrypointEscape(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := filepath.Join(pluginDir, "native-ai", "releases", "1.0.0")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version = "pulseops.plugin/v1"
id = "native-ai"
name = "Native AI"
version = "1.0.0"
enabled = true

[[ai_data_sources]]
name = "native_source"
runtime = "c_abi"
entrypoint = "../native.so"
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manager := NewManager(Options{
		Config: config.PluginsConfig{Dir: pluginDir, Strict: true},
		Store:  newMemoryPluginStore(),
	})
	if err := manager.Initialize(context.Background()); err == nil {
		t.Fatal("expected C ABI entrypoint validation error")
	}
}

func writeTestRelease(t *testing.T, pluginDir, pluginID, version string) string {
	t.Helper()
	releaseDir := filepath.Join(pluginDir, pluginID, "releases", version)
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version = "pulseops.plugin/v1"
id = "` + pluginID + `"
name = "External Driver"
version = "` + version + `"
enabled = true

[[task_drivers]]
name = "external_check"
title = "External Check"
runtime = "process"
entrypoint = "driver.sh"

[task_drivers.schema]
target = { type = "string", required = true }
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	script := `#!/bin/sh
input=$(cat)
case "$input" in
  *'"action":"validate_runtime"'*) echo '{"ok":true,"data":{"ready":true}}' ;;
  *'"action":"validate"'*) echo '{"ok":true,"data":{"valid":true}}' ;;
  *'"action":"run"'*) echo '{"ok":true,"data":{"check_status":"pass","summary":{"driver":"external"},"payload":{"ok":true}}}' ;;
  *) echo '{"ok":false,"error":{"message":"bad action"}}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(releaseDir, "driver.sh"), []byte(script), 0755); err != nil {
		t.Fatalf("write driver: %v", err)
	}
	return releaseDir
}

type memoryPluginStore struct {
	mu          sync.Mutex
	packages    map[string]PackageRecord
	releases    map[string]ReleaseRecord
	active      map[string]string
	generations []GenerationRecord
	events      []EventRecord
}

func newMemoryPluginStore() *memoryPluginStore {
	return &memoryPluginStore{
		packages: map[string]PackageRecord{},
		releases: map[string]ReleaseRecord{},
		active:   map[string]string{},
	}
}

func (s *memoryPluginStore) EnsurePluginPackage(_ context.Context, record PackageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.packages[record.ID]; ok {
		record.CreatedAt = existing.CreatedAt
	}
	s.packages[record.ID] = record
	return nil
}

func (s *memoryPluginStore) UpsertPluginRelease(_ context.Context, record ReleaseRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases[releaseKey(record.PluginID, record.Version)] = record
	return nil
}

func (s *memoryPluginStore) ListPluginPackages(context.Context) ([]PackageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PackageRecord, 0, len(s.packages))
	for _, record := range s.packages {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memoryPluginStore) GetPluginPackage(_ context.Context, pluginID string) (PackageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.packages[pluginID]
	if !ok {
		return PackageRecord{}, sql.ErrNoRows
	}
	return record, nil
}

func (s *memoryPluginStore) UpdatePluginPackageStatus(_ context.Context, pluginID, status, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.packages[pluginID]
	record.Status = status
	record.LastError = lastError
	s.packages[pluginID] = record
	return nil
}

func (s *memoryPluginStore) ListPluginReleases(_ context.Context, pluginID string) ([]ReleaseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ReleaseRecord
	for _, record := range s.releases {
		if record.PluginID == pluginID {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func (s *memoryPluginStore) GetPluginRelease(_ context.Context, pluginID, version string) (ReleaseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.releases[releaseKey(pluginID, version)]
	if !ok {
		return ReleaseRecord{}, sql.ErrNoRows
	}
	return record, nil
}

func (s *memoryPluginStore) UpdatePluginReleaseStatus(_ context.Context, pluginID, version, status, validationError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := releaseKey(pluginID, version)
	record := s.releases[key]
	record.Status = status
	record.ValidationError = validationError
	s.releases[key] = record
	return nil
}

func (s *memoryPluginStore) SetActivePluginVersion(_ context.Context, pluginID, version, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[pluginID] = version
	return nil
}

func (s *memoryPluginStore) GetActivePluginVersions(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.active))
	for key, value := range s.active {
		out[key] = value
	}
	return out, nil
}

func (s *memoryPluginStore) InsertPluginGeneration(_ context.Context, record GenerationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generations = append(s.generations, record)
	return nil
}

func (s *memoryPluginStore) InsertPluginEvent(_ context.Context, record EventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, record)
	return nil
}

func (s *memoryPluginStore) CommitPluginGeneration(_ context.Context, commit GenerationCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if commit.SetActiveVersion {
		if s.active[commit.PackageID] != commit.ExpectedActiveVersion {
			return sql.ErrNoRows
		}
		s.active[commit.PackageID] = commit.ActiveVersion
	}
	if commit.PackageID != "" && commit.PackageStatus != "" {
		pkg := s.packages[commit.PackageID]
		pkg.Status = commit.PackageStatus
		pkg.LastError = commit.PackageLastError
		s.packages[commit.PackageID] = pkg
	}
	if commit.DrainingVersion != "" && commit.DrainingVersion != commit.ActiveReleaseVersion {
		key := releaseKey(commit.PackageID, commit.DrainingVersion)
		release := s.releases[key]
		release.Status = ReleaseStatusDraining
		release.ValidationError = ""
		s.releases[key] = release
	}
	if commit.ActiveReleaseVersion != "" {
		key := releaseKey(commit.PackageID, commit.ActiveReleaseVersion)
		release := s.releases[key]
		release.Status = ReleaseStatusActive
		release.ValidationError = ""
		s.releases[key] = release
	}
	s.generations = append(s.generations, commit.Generation)
	if commit.Event.Action != "" {
		if commit.Event.GenerationID == "" {
			commit.Event.GenerationID = commit.Generation.ID
		}
		s.events = append(s.events, commit.Event)
	}
	return nil
}

func releaseKey(pluginID, version string) string {
	return pluginID + "\x00" + version
}
