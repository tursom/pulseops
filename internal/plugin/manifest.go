package plugin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	ManifestFilename       = "pulseops.plugin.yaml"
	LegacyManifestFilename = "pulseops.plugin.toml"
	ReleaseChecksumFile    = "pulseops.plugin.sha256"
	ReleaseSignatureFile   = "pulseops.plugin.sig"
)

func LoadManifest(path string) (Manifest, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("decode manifest: %w", err)
	}
	sum := sha256.Sum256(content)
	return manifest, hex.EncodeToString(sum[:]), nil
}

func ValidateReleaseManifestFiles(dir string) error {
	legacyPath := filepath.Join(dir, LegacyManifestFilename)
	if _, err := os.Stat(legacyPath); err == nil {
		return fmt.Errorf("%s is not supported; use %s", legacyPath, ManifestFilename)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat legacy manifest %s: %w", legacyPath, err)
	}
	manifestPath := filepath.Join(dir, ManifestFilename)
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is required", manifestPath)
		}
		return fmt.Errorf("stat manifest %s: %w", manifestPath, err)
	}
	return nil
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
	errs = append(errs, validateManifestConfig(m)...)
	errs = append(errs, validateManifestRuntimes(m)...)
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

func validateManifestRuntimes(m Manifest) []error {
	var errs []error
	check := func(path, runtime string) {
		if strings.TrimSpace(runtime) == "c_abi" {
			errs = append(errs, fmt.Errorf("%s.runtime c_abi is not supported in plugin manifest V1; use process or http", path))
		}
	}
	for _, item := range m.DataSources {
		check("data_sources."+item.Name, item.Runtime)
	}
	for _, item := range m.AIDataSources {
		check("ai_data_sources."+item.Name, item.Runtime)
	}
	for _, item := range m.OutputWriters {
		check("output_writers."+item.Name, item.Runtime)
	}
	for _, item := range m.TaskDrivers {
		check("task_drivers."+item.Name, item.Runtime)
	}
	return errs
}

func validateManifestConfig(m Manifest) []error {
	var errs []error
	classes := m.ConfigClasses
	for name, class := range classes {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("config_classes contains an empty class name"))
			continue
		}
		if len(class.Fields) == 0 {
			errs = append(errs, fmt.Errorf("config class %q requires at least one field", name))
			continue
		}
		for fieldName, field := range class.Fields {
			errs = append(errs, validateConfigField("config_classes."+name+"."+fieldName, field, classes, []string{name})...)
		}
	}
	errs = append(errs, validateConfigSchema("config", m.Config, classes)...)
	for _, item := range m.TaskTemplates {
		errs = append(errs, validateConfigSchema("task_templates."+item.ID+".config", item.Config, classes)...)
	}
	for _, item := range m.TaskDrivers {
		errs = append(errs, validateConfigSchema("task_drivers."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.DataSources {
		errs = append(errs, validateConfigSchema("data_sources."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.AIDataSources {
		errs = append(errs, validateConfigSchema("ai_data_sources."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.OutputWriters {
		errs = append(errs, validateConfigSchema("output_writers."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.Evaluators {
		errs = append(errs, validateConfigSchema("evaluators."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.TraceSinks {
		errs = append(errs, validateConfigSchema("trace_sinks."+item.Name+".config", item.Config, classes)...)
	}
	for _, item := range m.Hooks {
		errs = append(errs, validateConfigSchema("hooks."+item.Name+".config", item.Config, classes)...)
	}
	return errs
}

func validateConfigSchema(path string, schema *ConfigSchema, classes map[string]ConfigClass) []error {
	if schema == nil {
		return nil
	}
	if len(schema.Fields) == 0 {
		return []error{fmt.Errorf("%s requires at least one field", path)}
	}
	var errs []error
	for name, field := range schema.Fields {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, fmt.Errorf("%s contains an empty field name", path))
			continue
		}
		errs = append(errs, validateConfigField(path+"."+name, field, classes, nil)...)
	}
	return errs
}

func validateConfigField(path string, field ConfigField, classes map[string]ConfigClass, classStack []string) []error {
	var errs []error
	typ := strings.TrimSpace(field.Type)
	if typ == "" {
		return []error{fmt.Errorf("%s.type is required", path)}
	}
	if !allowedConfigFieldType(typ) {
		errs = append(errs, fmt.Errorf("%s.type %q is not supported", path, typ))
	}
	if field.Validation.Pattern != "" {
		if _, err := regexp.Compile(field.Validation.Pattern); err != nil {
			errs = append(errs, fmt.Errorf("%s.validation.pattern is invalid: %w", path, err))
		}
	}
	if field.UI.VisibleWhen != nil && !allowedConfigConditionOp(field.UI.VisibleWhen.Op) {
		errs = append(errs, fmt.Errorf("%s.ui.visible_when.op %q is not supported", path, field.UI.VisibleWhen.Op))
	}
	switch typ {
	case "object":
		className := strings.TrimSpace(field.Class)
		if className == "" {
			errs = append(errs, fmt.Errorf("%s.class is required for object fields", path))
			break
		}
		if className == "JSONObject" {
			break
		}
		class, ok := classes[className]
		if !ok {
			errs = append(errs, fmt.Errorf("%s.class %q is not defined in config_classes", path, className))
			break
		}
		if containsString(classStack, className) {
			errs = append(errs, fmt.Errorf("%s.class %q forms a cycle", path, className))
			break
		}
		nextStack := append(append([]string(nil), classStack...), className)
		for name, child := range class.Fields {
			errs = append(errs, validateConfigField(path+"."+name, child, classes, nextStack)...)
		}
	case "array":
		if field.Items == nil {
			errs = append(errs, fmt.Errorf("%s.items is required for array fields", path))
			break
		}
		errs = append(errs, validateConfigField(path+".items", *field.Items, classes, classStack)...)
	case "select", "multi_select":
		if len(field.Options) == 0 {
			errs = append(errs, fmt.Errorf("%s.options is required for %s fields", path, typ))
		}
		for i, option := range field.Options {
			if option.Value == nil {
				errs = append(errs, fmt.Errorf("%s.options[%d].value is required", path, i))
			}
		}
	case "file":
		if strings.TrimSpace(field.AssetKind) == "" {
			errs = append(errs, fmt.Errorf("%s.asset_kind is required for file fields", path))
		}
		if !allowedAssetScope(field.AssetScope) {
			errs = append(errs, fmt.Errorf("%s.asset_scope %q is not supported", path, field.AssetScope))
		}
	}
	return errs
}

func allowedAssetScope(scope string) bool {
	switch strings.TrimSpace(scope) {
	case AssetScopePluginShared, AssetScopeCapabilityShared, AssetScopeConfigInstance:
		return true
	default:
		return false
	}
}

func allowedConfigFieldType(typ string) bool {
	switch typ {
	case "string", "number", "bool", "select", "multi_select", "object", "array", "file", "secret":
		return true
	default:
		return false
	}
}

func allowedConfigConditionOp(op string) bool {
	switch strings.TrimSpace(op) {
	case "", "eq", "ne", "in", "not_in", "exists", "empty":
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
		cap.ConfigClasses = cloneConfigClasses(m.ConfigClasses)
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
			Config:      cloneConfigSchema(item.Config),
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
			Config:      cloneConfigSchema(item.Config),
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
		Config:      cloneConfigSchema(item.Config),
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
		Config:      cloneConfigSchema(item.Config),
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

func cloneConfigClasses(input map[string]ConfigClass) map[string]ConfigClass {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]ConfigClass, len(input))
	for key, value := range input {
		value.Fields = cloneConfigFields(value.Fields)
		out[key] = value
	}
	return out
}

func cloneConfigSchema(input *ConfigSchema) *ConfigSchema {
	if input == nil {
		return nil
	}
	out := *input
	out.Fields = cloneConfigFields(input.Fields)
	return &out
}

func cloneConfigFields(input map[string]ConfigField) map[string]ConfigField {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]ConfigField, len(input))
	for key, value := range input {
		if value.Items != nil {
			items := *value.Items
			if items.Items != nil {
				nested := *items.Items
				items.Items = &nested
			}
			value.Items = &items
		}
		value.Options = append([]ConfigOption(nil), value.Options...)
		value.Accept = append([]string(nil), value.Accept...)
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
			releaseDir := filepath.Join(releasesDir, release.Name())
			manifestPath := filepath.Join(releaseDir, ManifestFilename)
			if err := ValidateReleaseManifestFiles(releaseDir); err != nil {
				if strings.Contains(err.Error(), ManifestFilename+" is required") {
					continue
				}
				return nil, err
			}
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
