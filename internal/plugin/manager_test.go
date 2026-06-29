package plugin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/task"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func TestManagerActivatesExternalProcessTaskDriver(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := filepath.Join(pluginDir, "external-driver", "releases", "1.0.0")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version: pulseops.plugin/v1
id: external-driver
name: External Driver
version: 1.0.0
enabled: true
task_drivers:
  - name: external_check
    title: External Check
    runtime: process
    entrypoint: driver.sh
    schema:
      target:
        type: string
        required: true
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

func TestManagerLifecycleTransitionsDrainReleasesAndCreateGenerations(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	writeTestRelease(t, pluginDir, "external-driver", "1.0.0")
	writeTestRelease(t, pluginDir, "external-driver", "1.1.0")
	store := newMemoryPluginStore()
	manager := NewManager(Options{
		Config: config.PluginsConfig{Dir: pluginDir},
		Store:  store,
	})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	startupGeneration := manager.ActiveGeneration().ID

	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.0.0"); err != nil {
		t.Fatalf("activate 1.0.0: %v", err)
	}
	firstActive := manager.ActiveGeneration()
	if firstActive.ID == startupGeneration || firstActive.ActiveVersions["external-driver"] != "1.0.0" {
		t.Fatalf("unexpected first active generation: %#v", firstActive)
	}
	assertMemoryReleaseStatus(t, store, "external-driver", "1.0.0", ReleaseStatusActive)

	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.1.0"); err != nil {
		t.Fatalf("activate 1.1.0: %v", err)
	}
	oldDriver := task.NewPluginDriver(firstActive.Capabilities[0], config.PluginsConfig{}, nil)
	if err := oldDriver.Validate(config.TaskSpec{ID: "old-external-task", Kind: "external_check", Params: map[string]any{"target": "steam"}}); err == nil ||
		!strings.Contains(err.Error(), "draining") {
		t.Fatalf("old generation capability should reject new calls after cutover, got %v", err)
	}
	secondActive := manager.ActiveGeneration()
	if secondActive.ID == firstActive.ID || secondActive.ActiveVersions["external-driver"] != "1.1.0" {
		t.Fatalf("unexpected second active generation: %#v", secondActive)
	}
	assertMemoryReleaseStatus(t, store, "external-driver", "1.0.0", ReleaseStatusDraining)
	assertMemoryReleaseStatus(t, store, "external-driver", "1.1.0", ReleaseStatusActive)

	if _, err := manager.DisablePlugin(context.Background(), "external-driver"); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	disabledGeneration := manager.ActiveGeneration()
	if disabledGeneration.ID == secondActive.ID {
		t.Fatal("disable should create a new generation")
	}
	if _, ok := disabledGeneration.ActiveVersions["external-driver"]; ok {
		t.Fatalf("disabled generation should not expose active external-driver: %#v", disabledGeneration.ActiveVersions)
	}
	assertMemoryPackageStatus(t, store, "external-driver", PackageStatusDisabled)
	assertMemoryReleaseStatus(t, store, "external-driver", "1.1.0", ReleaseStatusDraining)

	if _, err := manager.RollbackPlugin(context.Background(), "external-driver"); err != nil {
		t.Fatalf("rollback plugin: %v", err)
	}
	rollbackGeneration := manager.ActiveGeneration()
	if rollbackGeneration.ID == disabledGeneration.ID || rollbackGeneration.ActiveVersions["external-driver"] != "1.0.0" {
		t.Fatalf("unexpected rollback generation: %#v", rollbackGeneration)
	}
	assertMemoryPackageStatus(t, store, "external-driver", PackageStatusEnabled)
	assertMemoryReleaseStatus(t, store, "external-driver", "1.0.0", ReleaseStatusActive)
	assertMemoryReleaseStatus(t, store, "external-driver", "1.1.0", ReleaseStatusDraining)
}

func TestManagerReadinessFailureBlocksValidateAndActivate(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	writeTestReleaseWithReadiness(t, pluginDir, "external-driver", "1.0.0", true)
	writeTestReleaseWithReadiness(t, pluginDir, "external-driver", "1.1.0", false)
	store := newMemoryPluginStore()
	manager := NewManager(Options{
		Config: config.PluginsConfig{Dir: pluginDir},
		Store:  store,
	})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.0.0"); err != nil {
		t.Fatalf("activate 1.0.0: %v", err)
	}
	activeBefore := manager.ActiveGeneration()

	if _, err := manager.ValidateRelease(context.Background(), "external-driver", "1.1.0"); err == nil ||
		!strings.Contains(err.Error(), "readiness") {
		t.Fatalf("expected readiness validation failure, got %v", err)
	}
	assertMemoryReleaseStatus(t, store, "external-driver", "1.1.0", ReleaseStatusFailed)

	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.1.0"); err == nil ||
		!strings.Contains(err.Error(), "readiness") {
		t.Fatalf("expected readiness activation failure, got %v", err)
	}
	activeAfter := manager.ActiveGeneration()
	if activeAfter.ID != activeBefore.ID || activeAfter.ActiveVersions["external-driver"] != "1.0.0" {
		t.Fatalf("activation failure should keep active generation: before=%#v after=%#v", activeBefore, activeAfter)
	}
	assertMemoryReleaseStatus(t, store, "external-driver", "1.0.0", ReleaseStatusActive)
}

func TestManagerReloadDoesNotCutActiveGenerationOrVersion(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	writeTestRelease(t, pluginDir, "external-driver", "1.0.0")
	store := newMemoryPluginStore()
	manager := NewManager(Options{Config: config.PluginsConfig{Dir: pluginDir}, Store: store})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := manager.ActivateRelease(context.Background(), "external-driver", "1.0.0"); err != nil {
		t.Fatalf("activate 1.0.0: %v", err)
	}
	activeBefore := manager.ActiveGeneration()
	writeTestRelease(t, pluginDir, "external-driver", "1.1.0")

	catalog, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	activeAfter := manager.ActiveGeneration()
	if activeAfter.ID != activeBefore.ID {
		t.Fatalf("reload should not replace active generation: before=%s after=%s", activeBefore.ID, activeAfter.ID)
	}
	if activeAfter.ActiveVersions["external-driver"] != "1.0.0" {
		t.Fatalf("reload changed active version: %#v", activeAfter.ActiveVersions)
	}
	if catalog.ActiveGenerationID != activeBefore.ID {
		t.Fatalf("catalog generation changed on reload: %s", catalog.ActiveGenerationID)
	}
	assertMemoryReleaseStatus(t, store, "external-driver", "1.1.0", ReleaseStatusStaged)
}

func TestManagerGCProtectsActiveReferencedAndRetainedReleases(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	root := t.TempDir()
	store := newMemoryPluginStore()
	store.packages["external-driver"] = PackageRecord{ID: "external-driver", Name: "External Driver", Status: PackageStatusEnabled}
	store.active["external-driver"] = "active"
	releaseDir := func(version, status string) string {
		t.Helper()
		path := filepath.Join(root, version)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("mkdir release %s: %v", version, err)
		}
		store.releases[releaseKey("external-driver", version)] = ReleaseRecord{
			PluginID: "external-driver",
			Version:  version,
			Path:     path,
			Status:   status,
		}
		return path
	}
	activePath := releaseDir("active", ReleaseStatusActive)
	drainingRetainedPath := releaseDir("draining-retained", ReleaseStatusDraining)
	drainingExpiredPath := releaseDir("draining-expired", ReleaseStatusDraining)
	retiredRetainedPath := releaseDir("retired-retained", ReleaseStatusRetired)
	retiredExpiredPath := releaseDir("retired-expired", ReleaseStatusRetired)
	deletedRetainedPath := releaseDir("deleted-retained", ReleaseStatusDeleted)

	store.generations = []GenerationRecord{
		{ID: "plugin-gen-active", ActiveVersions: map[string]string{"external-driver": "active"}},
		{ID: "plugin-gen-draining-retained", ActiveVersions: map[string]string{"external-driver": "draining-retained"}},
		{ID: "plugin-gen-draining-expired", ActiveVersions: map[string]string{"external-driver": "draining-expired"}},
		{ID: "plugin-gen-retired-retained", ActiveVersions: map[string]string{"external-driver": "retired-retained"}},
		{ID: "plugin-gen-retired-expired", ActiveVersions: map[string]string{"external-driver": "retired-expired"}},
		{ID: "plugin-gen-deleted-retained", ActiveVersions: map[string]string{"external-driver": "deleted-retained"}},
	}
	store.generationRefs = map[string]memoryGenerationRef{
		"plugin-gen-active":            {lastReleasedAt: now.Add(-time.Hour)},
		"plugin-gen-draining-retained": {lastReleasedAt: now},
		"plugin-gen-draining-expired":  {lastReleasedAt: now.Add(-time.Hour)},
		"plugin-gen-retired-retained":  {lastReleasedAt: now},
		"plugin-gen-retired-expired":   {lastReleasedAt: now.Add(-time.Hour)},
		"plugin-gen-deleted-retained":  {lastReleasedAt: now},
	}
	manager := NewManager(Options{
		Config: config.PluginsConfig{GenerationRetention: config.Duration{Duration: 10 * time.Minute}},
		Store:  store,
	})
	manager.active = &Generation{ID: "plugin-gen-active", ActiveVersions: map[string]string{"external-driver": "active"}}

	if _, err := manager.GC(context.Background()); err != nil {
		t.Fatalf("gc: %v", err)
	}

	assertPathExists(t, activePath)
	assertPathExists(t, drainingRetainedPath)
	assertPathExists(t, drainingExpiredPath)
	assertPathExists(t, retiredRetainedPath)
	assertPathMissing(t, retiredExpiredPath)
	assertPathExists(t, deletedRetainedPath)
	assertMemoryReleaseStatus(t, store, "external-driver", "active", ReleaseStatusActive)
	assertMemoryReleaseStatus(t, store, "external-driver", "draining-retained", ReleaseStatusDraining)
	assertMemoryReleaseStatus(t, store, "external-driver", "draining-expired", ReleaseStatusRetired)
	assertMemoryReleaseStatus(t, store, "external-driver", "retired-retained", ReleaseStatusRetired)
	assertMemoryReleaseStatus(t, store, "external-driver", "retired-expired", ReleaseStatusDeleted)
	assertMemoryReleaseStatus(t, store, "external-driver", "deleted-retained", ReleaseStatusDeleted)
}

func TestManagerValidateReleaseEntrypointsCoversRuntimeCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		capType    string
		configure  func(*Manifest)
		wantPrefix string
	}{
		{
			name:       "output writer",
			capType:    CapabilityOutputWriter,
			wantPrefix: "writer",
			configure: func(m *Manifest) {
				m.OutputWriters = []NamedCapability{{Name: "writer", Runtime: "process", Entrypoint: "missing.sh"}}
			},
		},
		{
			name:       "evaluator",
			capType:    CapabilityEvaluator,
			wantPrefix: "price_eval",
			configure: func(m *Manifest) {
				m.Evaluators = []NamedCapability{{Name: "price_eval", Runtime: "process", Entrypoint: "missing.sh"}}
			},
		},
		{
			name:       "trace sink",
			capType:    CapabilityTraceSink,
			wantPrefix: "notify",
			configure: func(m *Manifest) {
				m.TraceSinks = []NamedCapability{{Name: "notify", Runtime: "process", Entrypoint: "missing.sh"}}
			},
		},
		{
			name:       "hook",
			capType:    CapabilityHook,
			wantPrefix: "audit",
			configure: func(m *Manifest) {
				m.Hooks = []NamedCapability{{Name: "audit", Runtime: "process", Entrypoint: "missing.sh"}}
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifest := Manifest{
				SchemaVersion: SchemaVersionV1,
				ID:            "external-observer",
				Name:          "External Observer",
				Version:       "1.0.0",
				Enabled:       true,
			}
			tt.configure(&manifest)
			manager := NewManager(Options{})
			err := manager.validateReleaseEntrypoints(ReleaseRecord{
				PluginID: "external-observer",
				Version:  "1.0.0",
				Path:     t.TempDir(),
				Manifest: manifest,
			})
			if err == nil {
				t.Fatal("expected missing process entrypoint error")
			}
			want := CapabilityID("external-observer", tt.capType, tt.wantPrefix)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %v", want, err)
			}
		})
	}
}

func TestManagerValidateConfigCallsHTTPRuntime(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writePluginTestResponse(t, w, map[string]any{"valid": true})
	}))
	defer server.Close()

	manager := NewManager(Options{Config: config.PluginsConfig{}, Store: newMemoryPluginStore()})
	if err := manager.RegisterBundled(BundledPlugin{
		Manifest: Manifest{
			ID:            "http-config-plugin",
			Name:          "HTTP Config Plugin",
			Version:       "1.0.0",
			SchemaVersion: SchemaVersionV1,
			Enabled:       true,
			DataSources: []DataSourceManifest{{
				Name:     "grpc",
				Runtime:  "http",
				Endpoint: server.URL,
				Config: &ConfigSchema{
					ValidateAction: "validate_config",
					Fields: map[string]ConfigField{
						"service": {Type: "string", Required: true},
					},
				},
			}},
		},
		DefaultEnabled: true,
	}); err != nil {
		t.Fatalf("register bundled: %v", err)
	}
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	capabilityID := CapabilityID("http-config-plugin", CapabilityDataSource, "grpc")
	if err := manager.ValidateConfig(context.Background(), ConfigValidationRequest{
		PluginID:     "http-config-plugin",
		CapabilityID: capabilityID,
		Scope:        "capability",
		Action:       "validate_config",
		Config:       map[string]any{"service": "yym.inventory.v1.InventoryService"},
		Input:        map[string]any{"instance_id": "cfg-grpc-prod"},
	}); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if captured["action"] != "validate_config" {
		t.Fatalf("expected validate_config action, got %#v", captured)
	}
	configMap, _ := captured["config"].(map[string]any)
	if configMap["service"] != "yym.inventory.v1.InventoryService" {
		t.Fatalf("unexpected config payload: %#v", configMap)
	}
	inputMap, _ := captured["input"].(map[string]any)
	if inputMap["scope"] != "capability" || inputMap["instance_id"] != "cfg-grpc-prod" {
		t.Fatalf("unexpected input payload: %#v", inputMap)
	}
}

func TestManagerValidateConfigReturnsPluginInvalid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePluginTestResponse(t, w, map[string]any{"valid": false, "message": "method not found"})
	}))
	defer server.Close()

	manager := NewManager(Options{Config: config.PluginsConfig{}, Store: newMemoryPluginStore()})
	if err := manager.RegisterBundled(BundledPlugin{
		Manifest: Manifest{
			ID:            "http-config-plugin",
			Name:          "HTTP Config Plugin",
			Version:       "1.0.0",
			SchemaVersion: SchemaVersionV1,
			Enabled:       true,
			DataSources: []DataSourceManifest{{
				Name:     "grpc",
				Runtime:  "http",
				Endpoint: server.URL,
				Config: &ConfigSchema{
					ValidateAction: "validate_config",
					Fields: map[string]ConfigField{
						"service": {Type: "string", Required: true},
					},
				},
			}},
		},
		DefaultEnabled: true,
	}); err != nil {
		t.Fatalf("register bundled: %v", err)
	}
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	err := manager.ValidateConfig(context.Background(), ConfigValidationRequest{
		PluginID:     "http-config-plugin",
		CapabilityID: CapabilityID("http-config-plugin", CapabilityDataSource, "grpc"),
		Action:       "validate_config",
		Config:       map[string]any{"service": "missing.Service"},
	})
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected plugin invalid error, got %v", err)
	}
}

func TestManagerValidateBuiltinGRPCConfigWithReflection(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	manager := NewManager(Options{Config: config.PluginsConfig{}, Store: newMemoryPluginStore()})
	if err := manager.RegisterBundled(GRPCSourcePlugin(true)); err != nil {
		t.Fatalf("register grpc plugin: %v", err)
	}
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := manager.ValidateConfig(context.Background(), ConfigValidationRequest{
		PluginID:     "@pulseops/grpc-source",
		CapabilityID: CapabilityID("@pulseops/grpc-source", CapabilityDataSource, "grpc"),
		Scope:        "capability",
		Action:       "validate_config",
		Config: map[string]any{
			"endpoint":    listener.Addr().String(),
			"schema_mode": "reflection",
			"service":     "grpc.health.v1.Health",
			"method":      "Check",
			"request":     map[string]any{"service": ""},
		},
	}); err != nil {
		t.Fatalf("validate builtin grpc config: %v", err)
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
	manifest := `schema_version: pulseops.plugin/v1
id: template-plugin
name: Template Plugin
version: 1.0.0
enabled: true
permissions:
  - tasks:write
task_templates:
  - id: external-template
    kind: http_check
    title: External Template
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

func TestManagerRejectsCABIManifestRuntime(t *testing.T) {
	t.Parallel()

	pluginDir := t.TempDir()
	releaseDir := filepath.Join(pluginDir, "native-ai", "releases", "1.0.0")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version: pulseops.plugin/v1
id: native-ai
name: Native AI
version: 1.0.0
enabled: true
ai_data_sources:
  - name: native_source
    runtime: c_abi
    entrypoint: ../native.so
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manager := NewManager(Options{
		Config: config.PluginsConfig{Dir: pluginDir, Strict: true},
		Store:  newMemoryPluginStore(),
	})
	err := manager.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "c_abi is not supported") {
		t.Fatalf("expected C ABI manifest runtime error, got %v", err)
	}
}

func writeTestRelease(t *testing.T, pluginDir, pluginID, version string) string {
	t.Helper()
	return writeTestReleaseWithReadiness(t, pluginDir, pluginID, version, true)
}

func writeTestReleaseWithReadiness(t *testing.T, pluginDir, pluginID, version string, ready bool) string {
	t.Helper()
	releaseDir := filepath.Join(pluginDir, pluginID, "releases", version)
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release: %v", err)
	}
	manifest := `schema_version: pulseops.plugin/v1
id: ` + pluginID + `
name: External Driver
version: ` + version + `
enabled: true
task_drivers:
  - name: external_check
    title: External Check
    runtime: process
    entrypoint: driver.sh
    schema:
      target:
        type: string
        required: true
`
	if err := os.WriteFile(filepath.Join(releaseDir, ManifestFilename), []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	readiness := `echo '{"ok":true,"data":{"ready":true}}'`
	if !ready {
		readiness = `echo '{"ok":false,"error":{"code":"not_ready","message":"runtime not ready"}}'`
	}
	script := `#!/bin/sh
input=$(cat)
case "$input" in
  *'"action":"validate_runtime"'*) ` + readiness + ` ;;
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
	mu             sync.Mutex
	packages       map[string]PackageRecord
	releases       map[string]ReleaseRecord
	active         map[string]string
	generations    []GenerationRecord
	generationRefs map[string]memoryGenerationRef
	events         []EventRecord
}

type memoryGenerationRef struct {
	refCount       int
	lastReleasedAt time.Time
}

func newMemoryPluginStore() *memoryPluginStore {
	return &memoryPluginStore{
		packages:       map[string]PackageRecord{},
		releases:       map[string]ReleaseRecord{},
		active:         map[string]string{},
		generationRefs: map[string]memoryGenerationRef{},
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
	if s.generationRefs == nil {
		s.generationRefs = map[string]memoryGenerationRef{}
	}
	if _, ok := s.generationRefs[record.ID]; !ok {
		s.generationRefs[record.ID] = memoryGenerationRef{lastReleasedAt: time.Now()}
	}
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
		for _, generation := range s.generations {
			if generation.ID == commit.Generation.ID || generation.ActiveVersions[commit.PackageID] != commit.DrainingVersion {
				continue
			}
			ref := s.generationRefs[generation.ID]
			if ref.refCount == 0 {
				ref.lastReleasedAt = time.Now()
				s.generationRefs[generation.ID] = ref
			}
		}
	}
	if commit.ActiveReleaseVersion != "" {
		key := releaseKey(commit.PackageID, commit.ActiveReleaseVersion)
		release := s.releases[key]
		release.Status = ReleaseStatusActive
		release.ValidationError = ""
		s.releases[key] = release
	}
	s.generations = append(s.generations, commit.Generation)
	if s.generationRefs == nil {
		s.generationRefs = map[string]memoryGenerationRef{}
	}
	if _, ok := s.generationRefs[commit.Generation.ID]; !ok {
		s.generationRefs[commit.Generation.ID] = memoryGenerationRef{lastReleasedAt: time.Now()}
	}
	if commit.Event.Action != "" {
		if commit.Event.GenerationID == "" {
			commit.Event.GenerationID = commit.Generation.ID
		}
		s.events = append(s.events, commit.Event)
	}
	return nil
}

func (s *memoryPluginStore) PluginReleaseProtected(_ context.Context, pluginID, version string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, generation := range s.generations {
		if generation.ActiveVersions[pluginID] == version {
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryPluginStore) DeleteExpiredPluginGenerations(_ context.Context, activeGenerationID string, retention time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-retention)
	next := s.generations[:0]
	removed := 0
	for _, generation := range s.generations {
		ref := s.generationRefs[generation.ID]
		if generation.ID != activeGenerationID && ref.refCount == 0 && !ref.lastReleasedAt.IsZero() && !ref.lastReleasedAt.After(cutoff) {
			delete(s.generationRefs, generation.ID)
			removed++
			continue
		}
		next = append(next, generation)
	}
	s.generations = next
	return removed, nil
}

func releaseKey(pluginID, version string) string {
	return pluginID + "\x00" + version
}

func writePluginTestResponse(t *testing.T, w http.ResponseWriter, data map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func assertMemoryReleaseStatus(t *testing.T, store *memoryPluginStore, pluginID, version, want string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	release, ok := store.releases[releaseKey(pluginID, version)]
	if !ok {
		t.Fatalf("release %s@%s not found", pluginID, version)
	}
	if release.Status != want {
		t.Fatalf("release %s@%s status=%s, want %s", pluginID, version, release.Status, want)
	}
}

func assertMemoryPackageStatus(t *testing.T, store *memoryPluginStore, pluginID, want string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	pkg, ok := store.packages[pluginID]
	if !ok {
		t.Fatalf("package %s not found", pluginID)
	}
	if pkg.Status != want {
		t.Fatalf("package %s status=%s, want %s", pluginID, pkg.Status, want)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected path %s to be removed, stat err=%v", path, err)
	}
}
