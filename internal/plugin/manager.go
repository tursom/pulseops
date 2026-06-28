package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pulseops/internal/config"
	"pulseops/internal/evaluator"
	"pulseops/internal/pluginruntime"
	"pulseops/internal/task"
)

type Options struct {
	BaseDir string
	Config  config.PluginsConfig
	Store   Store
	Logger  *slog.Logger
}

type Manager struct {
	baseDir   string
	cfg       config.PluginsConfig
	store     Store
	logger    *slog.Logger
	bundled   map[string]BundledPlugin
	activeSeq atomic.Int64

	mu        sync.RWMutex
	active    *Generation
	errors    []string
	listeners []func(*Generation)
}

type releaseProtectionStore interface {
	PluginReleaseProtected(ctx context.Context, pluginID, version string, retention time.Duration) (bool, error)
}

func NewManager(opts Options) *Manager {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		baseDir: opts.BaseDir,
		cfg:     opts.Config,
		store:   opts.Store,
		logger:  logger,
		bundled: map[string]BundledPlugin{},
	}
}

func (m *Manager) RegisterBundled(plugin BundledPlugin) error {
	if plugin.Manifest.ID == "" {
		return errors.New("bundled plugin id is required")
	}
	if plugin.Manifest.Version == "" {
		plugin.Manifest.Version = BundledVersion
	}
	if plugin.Manifest.SchemaVersion == "" {
		plugin.Manifest.SchemaVersion = SchemaVersionV1
	}
	if err := ValidateManifest(plugin.Manifest); err != nil {
		return fmt.Errorf("bundled plugin %s: %w", plugin.Manifest.ID, err)
	}
	m.bundled[plugin.Manifest.ID] = plugin
	return nil
}

func (m *Manager) Initialize(ctx context.Context) error {
	if m.store == nil {
		return errors.New("plugin store is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errors = nil
	if err := m.ensureBundled(ctx); err != nil {
		return err
	}
	if err := m.scanExternalLocked(ctx); err != nil {
		if m.cfg.Strict {
			return err
		}
		m.addErrorLocked(err.Error())
	}
	gen, err := m.buildGenerationLocked(ctx, nil, nil)
	if err != nil {
		if m.cfg.Strict {
			return err
		}
		m.addErrorLocked(err.Error())
		gen = m.emptyGenerationLocked()
	}
	if err := m.persistGenerationLocked(ctx, gen, "startup", "", "", "ready"); err != nil {
		return err
	}
	m.active = gen
	m.notifyGenerationListenersLocked(gen)
	return nil
}

func (m *Manager) RegisterGenerationListener(listener func(*Generation)) {
	if listener == nil {
		return
	}
	m.mu.Lock()
	m.listeners = append(m.listeners, listener)
	active := cloneGeneration(m.active)
	m.mu.Unlock()
	if active != nil {
		listener(active)
	}
}

func (m *Manager) ActiveDriverRegistry() (*task.Registry, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return task.NewRegistry(), ""
	}
	return m.active.DriverRegistry, m.active.ID
}

func (m *Manager) ActiveEvaluatorRegistry() (*evaluator.Registry, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return evaluator.NewRegistry(), ""
	}
	return m.active.EvaluatorRegistry, m.active.ID
}

func (m *Manager) ActiveGeneration() *Generation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return nil
	}
	return cloneGeneration(m.active)
}

func (m *Manager) Catalog(ctx context.Context) (Catalog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.catalogLocked(ctx, true)
}

func (m *Manager) Plugin(ctx context.Context, pluginID string) (PluginView, error) {
	catalog, err := m.Catalog(ctx)
	if err != nil {
		return PluginView{}, err
	}
	for _, item := range catalog.Plugins {
		if item.Package.ID == pluginID {
			return item, nil
		}
	}
	return PluginView{}, sql.ErrNoRows
}

func (m *Manager) Releases(ctx context.Context, pluginID string) ([]ReleaseRecord, error) {
	return m.store.ListPluginReleases(ctx, pluginID)
}

func (m *Manager) Capabilities(ctx context.Context, typ, kind string) ([]Capability, error) {
	gen := m.ActiveGeneration()
	if gen == nil {
		return []Capability{}, nil
	}
	var caps []Capability
	for _, cap := range gen.Capabilities {
		if typ != "" && cap.Type != typ {
			continue
		}
		if kind != "" && cap.Kind != kind && cap.Name != kind {
			continue
		}
		caps = append(caps, cap)
	}
	sortCapabilities(caps)
	return caps, nil
}

func (m *Manager) Reload(ctx context.Context) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.scanExternalLocked(ctx); err != nil {
		if m.cfg.Strict {
			return Catalog{}, err
		}
		m.addErrorLocked(err.Error())
	}
	if err := m.store.InsertPluginEvent(ctx, EventRecord{Action: "reload", Status: "ok", Message: "plugin directory scanned"}); err != nil {
		return Catalog{}, err
	}
	return m.catalogLocked(ctx, true)
}

func (m *Manager) ValidateRelease(ctx context.Context, pluginID, version string) (ReleaseRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.store.GetPluginRelease(ctx, pluginID, version)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if err := ValidateManifest(release.Manifest); err != nil {
		msg := err.Error()
		_ = m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusFailed, msg)
		_ = m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "failed", Message: msg})
		release.Status = ReleaseStatusFailed
		release.ValidationError = msg
		return release, err
	}
	if err := m.validateManifestPermissions(release.Manifest); err != nil {
		msg := err.Error()
		_ = m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusFailed, msg)
		_ = m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "failed", Message: msg})
		release.Status = ReleaseStatusFailed
		release.ValidationError = msg
		return release, err
	}
	if err := m.validateReleaseChecksum(release); err != nil {
		msg := err.Error()
		_ = m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusFailed, msg)
		_ = m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "failed", Message: msg})
		release.Status = ReleaseStatusFailed
		release.ValidationError = msg
		return release, err
	}
	if err := m.validateReleaseEntrypoints(release); err != nil {
		msg := err.Error()
		_ = m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusFailed, msg)
		_ = m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "failed", Message: msg})
		release.Status = ReleaseStatusFailed
		release.ValidationError = msg
		return release, err
	}
	if err := m.validateReleaseRuntime(ctx, release); err != nil {
		msg := err.Error()
		_ = m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusFailed, msg)
		_ = m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "failed", Message: msg})
		release.Status = ReleaseStatusFailed
		release.ValidationError = msg
		return release, err
	}
	if err := m.store.UpdatePluginReleaseStatus(ctx, pluginID, version, ReleaseStatusValidated, ""); err != nil {
		return ReleaseRecord{}, err
	}
	if err := m.store.InsertPluginEvent(ctx, EventRecord{PluginID: pluginID, Version: version, Action: "validate", Status: "ok"}); err != nil {
		return ReleaseRecord{}, err
	}
	release.Status = ReleaseStatusValidated
	release.ValidationError = ""
	return release, nil
}

func (m *Manager) ActivateRelease(ctx context.Context, pluginID, version string) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.store.GetPluginRelease(ctx, pluginID, version)
	if err != nil {
		return Catalog{}, err
	}
	if err := ValidateManifest(release.Manifest); err != nil {
		return Catalog{}, err
	}
	if err := m.validateManifestPermissions(release.Manifest); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseChecksum(release); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseEntrypoints(release); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseRuntime(ctx, release); err != nil {
		return Catalog{}, err
	}
	statusOverrides := map[string]string{pluginID: PackageStatusEnabled}
	activeOverrides := map[string]string{pluginID: version}
	gen, err := m.buildGenerationLocked(ctx, statusOverrides, activeOverrides)
	if err != nil {
		return Catalog{}, err
	}
	oldActive := ""
	activeVersions, _ := m.store.GetActivePluginVersions(ctx)
	if activeVersions != nil {
		oldActive = activeVersions[pluginID]
	}
	drainingVersion := oldActive
	if drainingVersion == version {
		drainingVersion = ""
	}
	if err := m.store.CommitPluginGeneration(ctx, GenerationCommit{
		PackageID:             pluginID,
		PackageStatus:         PackageStatusEnabled,
		SetActiveVersion:      true,
		ExpectedActiveVersion: oldActive,
		ActiveVersion:         version,
		ActiveReleaseVersion:  version,
		DrainingVersion:       drainingVersion,
		Generation:            generationRecord(gen),
		Event: EventRecord{
			PluginID: pluginID,
			Version:  version,
			Action:   "activate",
			Status:   "ok",
		},
	}); err != nil {
		return Catalog{}, err
	}
	m.active = gen
	m.notifyGenerationListenersLocked(gen)
	return m.catalogLocked(ctx, true)
}

func (m *Manager) DisablePlugin(ctx context.Context, pluginID string) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	statusOverrides := map[string]string{pluginID: PackageStatusDisabled}
	gen, err := m.buildGenerationLocked(ctx, statusOverrides, nil)
	if err != nil {
		return Catalog{}, err
	}
	activeVersions, _ := m.store.GetActivePluginVersions(ctx)
	activeVersion := activeVersions[pluginID]
	if err := m.store.CommitPluginGeneration(ctx, GenerationCommit{
		PackageID:       pluginID,
		PackageStatus:   PackageStatusDisabled,
		DrainingVersion: activeVersion,
		Generation:      generationRecord(gen),
		Event: EventRecord{
			PluginID: pluginID,
			Version:  activeVersion,
			Action:   "disable",
			Status:   "ok",
		},
	}); err != nil {
		return Catalog{}, err
	}
	m.active = gen
	m.notifyGenerationListenersLocked(gen)
	return m.catalogLocked(ctx, true)
}

func (m *Manager) EnablePlugin(ctx context.Context, pluginID string) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	activeVersions, err := m.store.GetActivePluginVersions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	version := activeVersions[pluginID]
	if version == "" {
		return Catalog{}, fmt.Errorf("plugin %s has no active version", pluginID)
	}
	release, err := m.store.GetPluginRelease(ctx, pluginID, version)
	if err != nil {
		return Catalog{}, err
	}
	if err := ValidateManifest(release.Manifest); err != nil {
		return Catalog{}, err
	}
	if err := m.validateManifestPermissions(release.Manifest); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseChecksum(release); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseEntrypoints(release); err != nil {
		return Catalog{}, err
	}
	if err := m.validateReleaseRuntime(ctx, release); err != nil {
		return Catalog{}, err
	}
	gen, err := m.buildGenerationLocked(ctx, map[string]string{pluginID: PackageStatusEnabled}, nil)
	if err != nil {
		return Catalog{}, err
	}
	if err := m.store.CommitPluginGeneration(ctx, GenerationCommit{
		PackageID:             pluginID,
		PackageStatus:         PackageStatusEnabled,
		SetActiveVersion:      true,
		ExpectedActiveVersion: version,
		ActiveVersion:         version,
		ActiveReleaseVersion:  version,
		Generation:            generationRecord(gen),
		Event: EventRecord{
			PluginID: pluginID,
			Version:  version,
			Action:   "enable",
			Status:   "ok",
		},
	}); err != nil {
		return Catalog{}, err
	}
	m.active = gen
	m.notifyGenerationListenersLocked(gen)
	return m.catalogLocked(ctx, true)
}

func (m *Manager) RollbackPlugin(ctx context.Context, pluginID string) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	activeVersions, err := m.store.GetActivePluginVersions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	current := activeVersions[pluginID]
	releases, err := m.store.ListPluginReleases(ctx, pluginID)
	if err != nil {
		return Catalog{}, err
	}
	target := ReleaseRecord{}
	for _, release := range releases {
		if release.Version == current {
			continue
		}
		if release.Status != ReleaseStatusValidated && release.Status != ReleaseStatusDraining && release.Status != ReleaseStatusRetired && release.Status != ReleaseStatusActive {
			continue
		}
		if target.Version == "" || release.UpdatedAt.After(target.UpdatedAt) || release.CreatedAt.After(target.CreatedAt) {
			target = release
		}
	}
		if target.Version == "" {
			return Catalog{}, fmt.Errorf("plugin %s has no rollback release", pluginID)
		}
		if err := ValidateManifest(target.Manifest); err != nil {
			return Catalog{}, err
		}
		if err := m.validateManifestPermissions(target.Manifest); err != nil {
			return Catalog{}, err
		}
		if err := m.validateReleaseChecksum(target); err != nil {
			return Catalog{}, err
		}
		if err := m.validateReleaseEntrypoints(target); err != nil {
			return Catalog{}, err
		}
		if err := m.validateReleaseRuntime(ctx, target); err != nil {
			return Catalog{}, err
		}
		gen, err := m.buildGenerationLocked(ctx, map[string]string{pluginID: PackageStatusEnabled}, map[string]string{pluginID: target.Version})
		if err != nil {
			return Catalog{}, err
	}
	if err := m.store.CommitPluginGeneration(ctx, GenerationCommit{
		PackageID:             pluginID,
		PackageStatus:         PackageStatusEnabled,
		SetActiveVersion:      true,
		ExpectedActiveVersion: current,
		ActiveVersion:         target.Version,
		ActiveReleaseVersion:  target.Version,
		DrainingVersion:       current,
		Generation:            generationRecord(gen),
		Event: EventRecord{
			PluginID: pluginID,
			Version:  target.Version,
			Action:   "rollback",
			Status:   "ok",
		},
	}); err != nil {
		return Catalog{}, err
	}
	m.active = gen
	m.notifyGenerationListenersLocked(gen)
	return m.catalogLocked(ctx, true)
}

func (m *Manager) GC(ctx context.Context) (Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	activeVersions, err := m.store.GetActivePluginVersions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	packages, err := m.store.ListPluginPackages(ctx)
	if err != nil {
		return Catalog{}, err
	}
	removed := 0
	for _, pkg := range packages {
		releases, err := m.store.ListPluginReleases(ctx, pkg.ID)
		if err != nil {
			return Catalog{}, err
		}
		for _, release := range releases {
			if release.Bundled || release.Path == "" || activeVersions[pkg.ID] == release.Version {
				continue
			}
			protected := false
			if guard, ok := m.store.(releaseProtectionStore); ok {
				var err error
				protected, err = guard.PluginReleaseProtected(ctx, release.PluginID, release.Version, m.cfg.GenerationRetention.Duration)
				if err != nil {
					return Catalog{}, err
				}
			}
			if release.Status == ReleaseStatusDraining {
				if protected {
					continue
				}
				_ = m.store.UpdatePluginReleaseStatus(ctx, release.PluginID, release.Version, ReleaseStatusRetired, "")
				continue
			}
			if release.Status != ReleaseStatusRetired && release.Status != ReleaseStatusDeleted {
				continue
			}
			if protected {
				continue
			}
			if err := os.RemoveAll(release.Path); err != nil {
				return Catalog{}, fmt.Errorf("remove release %s@%s: %w", release.PluginID, release.Version, err)
			}
			removed++
			_ = m.store.UpdatePluginReleaseStatus(ctx, release.PluginID, release.Version, ReleaseStatusDeleted, "")
		}
	}
	if err := m.store.InsertPluginEvent(ctx, EventRecord{Action: "gc", Status: "ok", Message: fmt.Sprintf("removed %d release directories", removed)}); err != nil {
		return Catalog{}, err
	}
	return m.catalogLocked(ctx, true)
}

func (m *Manager) ExportRelease(ctx context.Context, pluginID, version string) (string, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	release, err := m.store.GetPluginRelease(ctx, pluginID, version)
	if err != nil {
		return "", nil, err
	}
	return ExportReleaseArchive(release)
}

func (m *Manager) ImportRelease(ctx context.Context, reader io.Reader) (ReleaseRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.cfg.Dir, 0755); err != nil {
		return ReleaseRecord{}, fmt.Errorf("create plugin dir: %w", err)
	}
	tempDir, err := os.MkdirTemp(m.cfg.Dir, ".import-*")
	if err != nil {
		return ReleaseRecord{}, fmt.Errorf("create import temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if err := ExtractReleaseArchive(reader, tempDir); err != nil {
		return ReleaseRecord{}, err
	}
	manifestPath := filepath.Join(tempDir, ManifestFilename)
	manifest, _, err := LoadManifest(manifestPath)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return ReleaseRecord{}, err
	}
	if err := m.validateManifestPermissions(manifest); err != nil {
		return ReleaseRecord{}, err
	}
	if err := m.validateReleaseEntrypoints(ReleaseRecord{PluginID: manifest.ID, Manifest: manifest, Path: tempDir}); err != nil {
		return ReleaseRecord{}, err
	}
	checksum, err := ReleaseChecksum(tempDir)
	if err != nil {
		return ReleaseRecord{}, err
	}
	if err := VerifyReleaseChecksumSidecar(tempDir, checksum); err != nil {
		return ReleaseRecord{}, err
	}
	if err := VerifyReleaseSignatureSidecar(tempDir, checksum, m.cfg.SignatureKey); err != nil {
		return ReleaseRecord{}, err
	}
	targetDir := filepath.Join(m.cfg.Dir, safeArchiveName(manifest.ID), "releases", safeArchiveName(manifest.Version))
	if _, err := os.Stat(targetDir); err == nil {
		return ReleaseRecord{}, fmt.Errorf("release %s@%s already exists", manifest.ID, manifest.Version)
	} else if !os.IsNotExist(err) {
		return ReleaseRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return ReleaseRecord{}, err
	}
	if err := os.Rename(tempDir, targetDir); err != nil {
		return ReleaseRecord{}, fmt.Errorf("install imported release: %w", err)
	}
	release := ReleaseRecord{
		PluginID:      manifest.ID,
		Version:       manifest.Version,
		SchemaVersion: manifest.SchemaVersion,
		Manifest:      manifest,
		Path:          targetDir,
		Status:        ReleaseStatusStaged,
		Checksum:      checksum,
	}
	if err := m.store.EnsurePluginPackage(ctx, PackageRecord{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Description: manifest.Description,
		Author:      manifest.Author,
		Homepage:    manifest.Homepage,
		Status:      PackageStatusDisabled,
	}); err != nil {
		return ReleaseRecord{}, err
	}
	if err := m.store.UpsertPluginRelease(ctx, release); err != nil {
		return ReleaseRecord{}, err
	}
	if err := m.store.InsertPluginEvent(ctx, EventRecord{PluginID: manifest.ID, Version: manifest.Version, Action: "import", Status: "ok", Message: "plugin release imported"}); err != nil {
		return ReleaseRecord{}, err
	}
	return release, nil
}

func (m *Manager) ensureBundled(ctx context.Context) error {
	ids := make([]string, 0, len(m.bundled))
	for id := range m.bundled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		bundled := m.bundled[id]
		status := PackageStatusDisabled
		releaseStatus := ReleaseStatusValidated
		if bundled.DefaultEnabled {
			status = PackageStatusEnabled
			releaseStatus = ReleaseStatusActive
		}
		pkg := PackageRecord{
			ID:          bundled.Manifest.ID,
			Name:        bundled.Manifest.Name,
			Description: bundled.Manifest.Description,
			Author:      bundled.Manifest.Author,
			Homepage:    bundled.Manifest.Homepage,
			Official:    true,
			Bundled:     true,
			Status:      status,
		}
		if err := m.store.EnsurePluginPackage(ctx, pkg); err != nil {
			return err
		}
		if bundled.ForceDefaultStatus {
			if err := m.store.UpdatePluginPackageStatus(ctx, pkg.ID, status, ""); err != nil {
				return err
			}
		}
		if err := m.store.UpsertPluginRelease(ctx, ReleaseRecord{
			PluginID:      bundled.Manifest.ID,
			Version:       bundled.Manifest.Version,
			SchemaVersion: bundled.Manifest.SchemaVersion,
			Manifest:      bundled.Manifest,
			Status:        releaseStatus,
			Official:      true,
			Bundled:       true,
		}); err != nil {
			return err
		}
		if err := m.store.SetActivePluginVersion(ctx, bundled.Manifest.ID, bundled.Manifest.Version, ""); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) scanExternalLocked(ctx context.Context) error {
	if !m.cfg.IsEnabled() {
		return nil
	}
	paths, err := FindReleaseManifests(m.cfg.Dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, path := range paths {
		manifest, _, err := LoadManifest(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		releaseDir := filepath.Dir(path)
		checksum, checksumErr := ReleaseChecksum(releaseDir)
		if checksumErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, checksumErr))
			continue
		}
		if err := VerifyReleaseChecksumSidecar(releaseDir, checksum); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if err := VerifyReleaseSignatureSidecar(releaseDir, checksum, m.cfg.SignatureKey); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		validationErr := errors.Join(
			ValidateManifest(manifest),
			m.validateManifestPermissions(manifest),
			m.validateReleaseEntrypoints(ReleaseRecord{PluginID: manifest.ID, Manifest: manifest, Path: releaseDir}),
		)
		status := ReleaseStatusStaged
		validationMessage := ""
		if validationErr != nil {
			status = ReleaseStatusFailed
			validationMessage = validationErr.Error()
		}
		pkgStatus := PackageStatusDisabled
		if !manifest.Enabled {
			pkgStatus = PackageStatusDisabled
		}
		if manifest.ID == "" {
			errs = append(errs, fmt.Errorf("%s: manifest id is required", path))
			continue
		}
		pkg := PackageRecord{
			ID:          manifest.ID,
			Name:        manifest.Name,
			Description: manifest.Description,
			Author:      manifest.Author,
			Homepage:    manifest.Homepage,
			Status:      pkgStatus,
		}
		if err := m.store.EnsurePluginPackage(ctx, pkg); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := m.store.UpsertPluginRelease(ctx, ReleaseRecord{
			PluginID:        manifest.ID,
			Version:         manifest.Version,
			SchemaVersion:   manifest.SchemaVersion,
			Manifest:        manifest,
			Path:            releaseDir,
			Status:          status,
			Checksum:        checksum,
			ValidationError: validationMessage,
		}); err != nil {
			errs = append(errs, err)
			continue
		}
		if validationErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, validationErr))
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) buildGenerationLocked(ctx context.Context, statusOverrides, activeOverrides map[string]string) (*Generation, error) {
	packages, err := m.store.ListPluginPackages(ctx)
	if err != nil {
		return nil, err
	}
	activeVersions, err := m.store.GetActivePluginVersions(ctx)
	if err != nil {
		return nil, err
	}
	for id, version := range activeOverrides {
		activeVersions[id] = version
	}

	drivers := task.NewRegistry()
	evaluators := evaluator.NewRegistry()
	gen := &Generation{
		ID:                m.nextGenerationIDLocked(),
		ActiveVersions:    map[string]string{},
		CreatedAt:         time.Now().UTC(),
		DriverRegistry:    drivers,
		EvaluatorRegistry: evaluators,
	}
	seenCapabilities := map[string]struct{}{}
	var errs []error
	for _, pkg := range packages {
		status := pkg.Status
		if override, ok := statusOverrides[pkg.ID]; ok {
			status = override
		}
		if status != PackageStatusEnabled {
			continue
		}
		version := activeVersions[pkg.ID]
		if version == "" {
			errs = append(errs, fmt.Errorf("plugin %s is enabled but has no active version", pkg.ID))
			continue
		}
		release, err := m.store.GetPluginRelease(ctx, pkg.ID, version)
		if err != nil {
			errs = append(errs, fmt.Errorf("plugin %s active release %s: %w", pkg.ID, version, err))
			continue
		}
		if err := m.validateManifestPermissions(release.Manifest); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s active release %s permissions: %w", pkg.ID, version, err))
			continue
		}
		caps := ManifestCapabilities(release.Manifest, pkg.Official, pkg.Bundled, true)
		for _, cap := range caps {
			cap.ReleasePath = release.Path
			if _, exists := seenCapabilities[cap.ID]; exists {
				errs = append(errs, fmt.Errorf("capability conflict: %s", cap.ID))
				continue
			}
			seenCapabilities[cap.ID] = struct{}{}
			gen.Capabilities = append(gen.Capabilities, cap)
			if cap.Type == CapabilityTaskDriver && isPluginRuntime(cap.Runtime) {
				if err := drivers.Register(task.NewPluginDriver(cap, m.cfg, m.logger)); err != nil {
					errs = append(errs, fmt.Errorf("plugin %s driver %s: %w", pkg.ID, cap.Name, err))
				}
			}
			if cap.Type == CapabilityEvaluator && isPluginRuntime(cap.Runtime) {
				if err := evaluators.Register(evaluator.NewPluginEvaluator(cap, m.cfg)); err != nil {
					errs = append(errs, fmt.Errorf("plugin %s evaluator %s: %w", pkg.ID, cap.Name, err))
				}
			}
		}
		if bundled, ok := m.bundled[pkg.ID]; ok && bundled.Build != nil {
			reg := bundled.Build()
			for _, driver := range reg.Drivers {
				if driver == nil {
					continue
				}
				if err := drivers.Register(driver); err != nil {
					errs = append(errs, fmt.Errorf("plugin %s driver %s: %w", pkg.ID, driver.Kind(), err))
				}
			}
			for _, item := range reg.Evaluators {
				if item == nil {
					continue
				}
				if err := evaluators.Register(item); err != nil {
					errs = append(errs, fmt.Errorf("plugin %s evaluator %s: %w", pkg.ID, item.Name(), err))
				}
			}
		}
		gen.ActiveVersions[pkg.ID] = version
	}
	sortCapabilities(gen.Capabilities)
	if len(errs) > 0 {
		return gen, errors.Join(errs...)
	}
	return gen, nil
}

func (m *Manager) validateReleaseEntrypoints(release ReleaseRecord) error {
	if release.Bundled {
		return nil
	}
	if release.Path == "" {
		return nil
	}
	var errs []error
	check := func(runtime, entrypoint, name string) {
		if runtime != "process" && runtime != "c_abi" {
			return
		}
		if entrypoint == "" {
			errs = append(errs, fmt.Errorf("%s: %s entrypoint is required", name, runtime))
			return
		}
		if filepath.IsAbs(entrypoint) {
			errs = append(errs, fmt.Errorf("%s: entrypoint must be relative to the plugin release path", name))
			return
		}
		absRelease, err := filepath.Abs(release.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: resolve release path: %w", name, err))
			return
		}
		path := filepath.Join(absRelease, entrypoint)
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: resolve entrypoint: %w", name, err))
			return
		}
		rel, err := filepath.Rel(absRelease, absPath)
		if err != nil || strings.HasPrefix(rel, "..") || rel == "." || filepath.IsAbs(rel) {
			errs = append(errs, fmt.Errorf("%s: entrypoint must stay inside the plugin release path", name))
			return
		}
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		if fileInfo.IsDir() {
			errs = append(errs, fmt.Errorf("%s: entrypoint %s is a directory", name, entrypoint))
			return
		}
		if runtime == "process" && fileInfo.Mode().Perm()&0111 == 0 {
			errs = append(errs, fmt.Errorf("%s: process entrypoint %s is not executable", name, entrypoint))
		}
	}
	for _, item := range release.Manifest.DataSources {
		check(item.Runtime, item.Entrypoint, CapabilityID(release.PluginID, CapabilityDataSource, item.Name))
	}
	for _, item := range release.Manifest.AIDataSources {
		check(item.Runtime, item.Entrypoint, CapabilityID(release.PluginID, CapabilityAIDataSource, item.Name))
	}
	for _, item := range release.Manifest.TaskDrivers {
		check(item.Runtime, item.Entrypoint, CapabilityID(release.PluginID, CapabilityTaskDriver, item.Name))
	}
	return errors.Join(errs...)
}

func (m *Manager) validateReleaseChecksum(release ReleaseRecord) error {
	if release.Bundled || release.Path == "" {
		return nil
	}
	checksum, err := ReleaseChecksum(release.Path)
	if err != nil {
		return err
	}
	if release.Checksum != "" && !strings.EqualFold(release.Checksum, checksum) {
		return fmt.Errorf("release checksum mismatch: stored %s, actual %s", release.Checksum, checksum)
	}
	if err := VerifyReleaseChecksumSidecar(release.Path, checksum); err != nil {
		return err
	}
	return VerifyReleaseSignatureSidecar(release.Path, checksum, m.cfg.SignatureKey)
}

func (m *Manager) validateReleaseRuntime(ctx context.Context, release ReleaseRecord) error {
	if release.Bundled {
		return nil
	}
	caps := ManifestCapabilities(release.Manifest, release.Official, release.Bundled, true)
	var errs []error
	for _, cap := range caps {
		if !isPluginRuntime(cap.Runtime) {
			continue
		}
		cap.ReleasePath = release.Path
		client := pluginruntime.NewClient(cap, m.cfg)
		if err := client.ValidateAvailable(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cap.ID, err))
			continue
		}
		if _, err := client.Call(ctx, pluginruntime.Request{
			Action: "validate_runtime",
			Config: cloneAnyMap(cap.Defaults),
			Input: map[string]any{
				"capability": cap,
			},
		}, pluginruntime.Deps{}); err != nil {
			errs = append(errs, fmt.Errorf("%s readiness: %w", cap.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) validateManifestPermissions(manifest Manifest) error {
	var errs []error
	for _, cap := range ManifestCapabilities(manifest, false, false, true) {
		for _, permission := range cap.Permissions {
			permission = strings.TrimSpace(permission)
			if permission == "" {
				continue
			}
			if !m.cfg.PermissionAllowed(permission) {
				errs = append(errs, fmt.Errorf("%s requires permission %q outside plugins.allowed_permissions", cap.ID, permission))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) catalogLocked(ctx context.Context, includeReleases bool) (Catalog, error) {
	packages, err := m.store.ListPluginPackages(ctx)
	if err != nil {
		return Catalog{}, err
	}
	activeVersions, err := m.store.GetActivePluginVersions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	activeCaps := map[string][]Capability{}
	if m.active != nil {
		for _, cap := range m.active.Capabilities {
			activeCaps[cap.PluginID] = append(activeCaps[cap.PluginID], cap)
		}
	}
	catalog := Catalog{
		GeneratedAt: time.Now().UTC(),
		PluginDir:   m.cfg.Dir,
		Status:      "ready",
		Errors:      append([]string(nil), m.errors...),
	}
	if m.active != nil {
		catalog.ActiveGenerationID = m.active.ID
	}
	for _, pkg := range packages {
		view := PluginView{
			Package:       pkg,
			ActiveVersion: activeVersions[pkg.ID],
			Capabilities:  activeCaps[pkg.ID],
		}
		releases, err := m.store.ListPluginReleases(ctx, pkg.ID)
		if err != nil {
			return Catalog{}, err
		}
		for i := range releases {
			if releases[i].Version == view.ActiveVersion {
				release := releases[i]
				view.Release = &release
				view.Permissions = append([]string(nil), release.Manifest.Permissions...)
			}
		}
		if includeReleases {
			view.Releases = releases
		}
		catalog.Plugins = append(catalog.Plugins, view)
	}
	sort.Slice(catalog.Plugins, func(i, j int) bool {
		return catalog.Plugins[i].Package.ID < catalog.Plugins[j].Package.ID
	})
	catalog.Stats.Total = len(catalog.Plugins)
	for _, item := range catalog.Plugins {
		switch item.Package.Status {
		case PackageStatusEnabled:
			catalog.Stats.Enabled++
		case PackageStatusDisabled:
			catalog.Stats.Disabled++
		case PackageStatusDegraded:
			catalog.Stats.Errors++
		}
		if item.Package.LastError != "" {
			catalog.Stats.Errors++
		}
		catalog.Stats.Capabilities += len(item.Capabilities)
	}
	if len(catalog.Errors) > 0 || catalog.Stats.Errors > 0 {
		catalog.Status = "degraded"
	}
	return catalog, nil
}

func (m *Manager) persistGenerationLocked(ctx context.Context, gen *Generation, action, pluginID, version, status string) error {
	if gen == nil {
		return errors.New("generation is nil")
	}
	if err := m.store.InsertPluginGeneration(ctx, generationRecord(gen)); err != nil {
		return err
	}
	if action != "" {
		if err := m.store.InsertPluginEvent(ctx, EventRecord{
			PluginID:     pluginID,
			Version:      version,
			Action:       action,
			Status:       status,
			GenerationID: gen.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func generationRecord(gen *Generation) GenerationRecord {
	return GenerationRecord{
		ID:             gen.ID,
		Status:         "active",
		ActiveVersions: cloneStringMap(gen.ActiveVersions),
		Capabilities:   append([]Capability(nil), gen.Capabilities...),
		CreatedAt:      gen.CreatedAt,
	}
}

func (m *Manager) emptyGenerationLocked() *Generation {
	return &Generation{
		ID:                m.nextGenerationIDLocked(),
		ActiveVersions:    map[string]string{},
		Capabilities:      []Capability{},
		CreatedAt:         time.Now().UTC(),
		DriverRegistry:    task.NewRegistry(),
		EvaluatorRegistry: evaluator.NewRegistry(),
	}
}

func (m *Manager) nextGenerationIDLocked() string {
	n := m.activeSeq.Add(1)
	now := time.Now().UTC()
	return fmt.Sprintf("plugin-gen-%s-%d-%05d", now.Format("20060102-150405"), now.UnixNano(), n)
}

func (m *Manager) addErrorLocked(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	m.errors = append(m.errors, message)
	m.logger.Warn("plugin catalog degraded", "error", message)
}

func (m *Manager) notifyGenerationListenersLocked(gen *Generation) {
	for _, listener := range m.listeners {
		listener(cloneGeneration(gen))
	}
}

func sortCapabilities(caps []Capability) {
	sort.Slice(caps, func(i, j int) bool {
		if caps[i].PluginID == caps[j].PluginID {
			if caps[i].Type == caps[j].Type {
				return caps[i].Name < caps[j].Name
			}
			return caps[i].Type < caps[j].Type
		}
		return caps[i].PluginID < caps[j].PluginID
	})
}

func isPluginRuntime(runtime string) bool {
	return runtime == "process" || runtime == "http" || runtime == "http_plugin"
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneGeneration(input *Generation) *Generation {
	if input == nil {
		return nil
	}
	return &Generation{
		ID:                input.ID,
		ActiveVersions:    cloneStringMap(input.ActiveVersions),
		Capabilities:      append([]Capability(nil), input.Capabilities...),
		CreatedAt:         input.CreatedAt,
		DriverRegistry:    input.DriverRegistry,
		EvaluatorRegistry: input.EvaluatorRegistry,
	}
}
