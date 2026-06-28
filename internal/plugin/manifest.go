package plugin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	ManifestFilename     = "pulseops.plugin.toml"
	ReleaseChecksumFile  = "pulseops.plugin.sha256"
	ReleaseSignatureFile = "pulseops.plugin.sig"
)

func LoadManifest(path string) (Manifest, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := toml.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("decode manifest: %w", err)
	}
	sum := sha256.Sum256(content)
	return manifest, hex.EncodeToString(sum[:]), nil
}

func ReleaseChecksum(dir string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == ReleaseChecksumFile {
			return nil
		}
		if entry.Name() == ReleaseSignatureFile {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk release dir: %w", err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return "", fmt.Errorf("resolve release file %s: %w", path, err)
		}
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open release file %s: %w", path, err)
		}
		if _, err := io.Copy(hash, file); err != nil {
			_ = file.Close()
			return "", fmt.Errorf("hash release file %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close release file %s: %w", path, err)
		}
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func VerifyReleaseChecksumSidecar(dir, checksum string) error {
	raw, err := os.ReadFile(filepath.Join(dir, ReleaseChecksumFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read checksum sidecar: %w", err)
	}
	expected := strings.Fields(string(raw))
	if len(expected) == 0 {
		return fmt.Errorf("%s is empty", ReleaseChecksumFile)
	}
	if !strings.EqualFold(expected[0], checksum) {
		return fmt.Errorf("release checksum mismatch: sidecar %s, actual %s", expected[0], checksum)
	}
	return nil
}

func VerifyReleaseSignatureSidecar(dir, checksum, key string) error {
	raw, err := os.ReadFile(filepath.Join(dir, ReleaseSignatureFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read signature sidecar: %w", err)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s exists but plugins.signature_key is not configured", ReleaseSignatureFile)
	}
	expected := strings.Fields(string(raw))
	if len(expected) == 0 {
		return fmt.Errorf("%s is empty", ReleaseSignatureFile)
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(checksum))
	actual := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(expected[0])), []byte(actual)) {
		return fmt.Errorf("release signature mismatch")
	}
	return nil
}

func ValidateManifest(m Manifest) error {
	var errs []error
	if m.SchemaVersion == "" {
		errs = append(errs, errors.New("schema_version is required"))
	} else if m.SchemaVersion != SchemaVersionV1 {
		errs = append(errs, fmt.Errorf("unsupported schema_version %q", m.SchemaVersion))
	}
	if strings.TrimSpace(m.ID) == "" {
		errs = append(errs, errors.New("id is required"))
	}
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, errors.New("version is required"))
	}
	seen := map[string]struct{}{}
	for _, cap := range ManifestCapabilities(m, false, false, true) {
		if cap.Name == "" {
			errs = append(errs, fmt.Errorf("%s capability name is required", cap.Type))
			continue
		}
		key := cap.Type + ":" + cap.Name
		if _, ok := seen[key]; ok {
			errs = append(errs, fmt.Errorf("duplicate capability %s", key))
		}
		seen[key] = struct{}{}
	}
	return errors.Join(errs...)
}

func ManifestCapabilities(m Manifest, official, bundled, enabled bool) []Capability {
	var caps []Capability
	appendCap := func(cap Capability) {
		if cap.Name == "" {
			return
		}
		cap.PluginID = m.ID
		cap.PluginName = m.Name
		cap.PluginVersion = m.Version
		cap.ID = CapabilityID(m.ID, cap.Type, cap.Name)
		cap.Enabled = enabled
		cap.Official = official
		cap.Bundled = bundled
		if cap.Status == "" {
			if enabled {
				cap.Status = "active"
			} else {
				cap.Status = "disabled"
			}
		}
		if len(cap.Permissions) == 0 {
			cap.Permissions = append([]string(nil), m.Permissions...)
		}
		caps = append(caps, cap)
	}

	for _, item := range m.TaskTemplates {
		appendCap(Capability{
			Type:        CapabilityTaskTemplate,
			Name:        item.ID,
			Title:       item.Title,
			Description: item.Description,
			Kind:        item.Kind,
			Permissions: append([]string(nil), item.Permissions...),
			Defaults:    cloneAnyMap(item.Defaults),
			Params:      cloneAnyMap(item.Params),
			Schema:      cloneSchema(item.Schema),
		})
	}
	for _, item := range m.TaskDrivers {
		appendCap(namedCapability(CapabilityTaskDriver, item))
	}
	for _, item := range m.DataSources {
		appendCap(Capability{
			Type:        CapabilityDataSource,
			Name:        item.Name,
			Title:       item.Title,
			Description: item.Description,
			Protocol:    item.Protocol,
			Runtime:     item.Runtime,
			Entrypoint:  item.Entrypoint,
			Endpoint:    item.Endpoint,
			Permissions: append([]string(nil), item.Permissions...),
			Defaults:    cloneAnyMap(item.Defaults),
			Schema:      cloneSchema(item.Schema),
		})
	}
	for _, item := range m.AIDataSources {
		appendCap(runtimeCapability(CapabilityAIDataSource, item))
	}
	for _, item := range m.OutputWriters {
		appendCap(namedCapability(CapabilityOutputWriter, item))
	}
	for _, item := range m.Evaluators {
		appendCap(namedCapability(CapabilityEvaluator, item))
	}
	for _, item := range m.TraceSinks {
		appendCap(namedCapability(CapabilityTraceSink, item))
	}
	for _, item := range m.Hooks {
		appendCap(namedCapability(CapabilityHook, item))
	}
	for _, item := range m.UIExtensions {
		appendCap(Capability{
			Type:        CapabilityUIExtension,
			Name:        item.ID,
			Title:       item.Title,
			Description: item.Description,
			Path:        item.Path,
			Permissions: append([]string(nil), item.Permissions...),
		})
	}

	sort.Slice(caps, func(i, j int) bool {
		if caps[i].Type == caps[j].Type {
			return caps[i].Name < caps[j].Name
		}
		return caps[i].Type < caps[j].Type
	})
	return caps
}

func CapabilityID(pluginID, typ, name string) string {
	return pluginID + ":" + typ + ":" + name
}

func namedCapability(typ string, item NamedCapability) Capability {
	return Capability{
		Type:        typ,
		Name:        item.Name,
		Title:       item.Title,
		Description: item.Description,
		Runtime:     item.Runtime,
		Entrypoint:  item.Entrypoint,
		Endpoint:    item.Endpoint,
		Permissions: append([]string(nil), item.Permissions...),
		Defaults:    cloneAnyMap(item.Defaults),
		Schema:      cloneSchema(item.Schema),
	}
}

func runtimeCapability(typ string, item RuntimeCapability) Capability {
	return Capability{
		Type:        typ,
		Name:        item.Name,
		Title:       item.Title,
		Description: item.Description,
		Runtime:     item.Runtime,
		Entrypoint:  item.Entrypoint,
		Endpoint:    item.Endpoint,
		Permissions: append([]string(nil), item.Permissions...),
		Defaults:    cloneAnyMap(item.Defaults),
		Schema:      cloneSchema(item.Schema),
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneSchema(input Schema) Schema {
	if len(input) == 0 {
		return nil
	}
	out := make(Schema, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func FindReleaseManifests(pluginDir string) ([]string, error) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plugin dir: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		releasesDir := filepath.Join(pluginDir, entry.Name(), "releases")
		releases, err := os.ReadDir(releasesDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read releases dir %s: %w", releasesDir, err)
		}
		for _, release := range releases {
			if !release.IsDir() {
				continue
			}
			manifestPath := filepath.Join(releasesDir, release.Name(), ManifestFilename)
			if _, err := os.Stat(manifestPath); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("stat manifest %s: %w", manifestPath, err)
			}
			paths = append(paths, manifestPath)
		}
	}
	sort.Strings(paths)
	return paths, nil
}
