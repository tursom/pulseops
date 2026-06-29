package pluginconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

type RuntimeStore interface {
	GetPluginAsset(ctx context.Context, assetID string) (pluginmodel.AssetRecord, error)
	GetPluginAssetVersion(ctx context.Context, assetID string, version int) (pluginmodel.AssetVersionRecord, error)
	GetActivePluginAssetVersion(ctx context.Context, assetID string) (pluginmodel.AssetVersionRecord, error)
	GetPluginSecret(ctx context.Context, secretID string) (pluginmodel.SecretRecord, error)
	GetPluginSecretValue(ctx context.Context, secretID string) (pluginmodel.SecretValueRecord, error)
}

type RuntimeResolveOptions struct {
	PluginID          string
	CapabilityID      string
	ConfigInstanceIDs []string
}

func ResolveRuntimeValues(
	ctx context.Context,
	runtimeStore RuntimeStore,
	artifactStore store.ArtifactStore,
	schema *pluginmodel.ConfigSchema,
	classes map[string]pluginmodel.ConfigClass,
	values map[string]any,
) (map[string]any, map[string]any, []map[string]any, func(), error) {
	return ResolveRuntimeValuesWithOptions(ctx, runtimeStore, artifactStore, schema, classes, values, RuntimeResolveOptions{})
}

func ResolveRuntimeValuesWithOptions(
	ctx context.Context,
	runtimeStore RuntimeStore,
	artifactStore store.ArtifactStore,
	schema *pluginmodel.ConfigSchema,
	classes map[string]pluginmodel.ConfigClass,
	values map[string]any,
	opts RuntimeResolveOptions,
) (map[string]any, map[string]any, []map[string]any, func(), error) {
	if schema == nil || len(values) == 0 {
		return values, map[string]any{}, nil, func() {}, nil
	}
	resolver := &runtimeResolver{
		ctx:           ctx,
		runtimeStore:  runtimeStore,
		artifactStore: artifactStore,
		opts:          opts,
		assetVersions: map[string]any{},
		assetRefs:     []map[string]any{},
	}
	resolved, err := resolver.resolveFields("", schema.Fields, classes, values)
	if err != nil {
		resolver.cleanup()
		return nil, resolver.assetVersions, resolver.assetRefs, func() {}, err
	}
	return resolved, resolver.assetVersions, resolver.assetRefs, resolver.cleanup, nil
}

type runtimeResolver struct {
	ctx           context.Context
	runtimeStore  RuntimeStore
	artifactStore store.ArtifactStore
	opts          RuntimeResolveOptions
	tempDir       string
	counter       int
	assetVersions map[string]any
	assetRefs     []map[string]any
}

func (r *runtimeResolver) resolveFields(path string, fields map[string]pluginmodel.ConfigField, classes map[string]pluginmodel.ConfigClass, values map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	for name, field := range fields {
		value, ok := values[name]
		if !ok || value == nil {
			continue
		}
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		resolved, err := r.resolveField(fieldPath, field, classes, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = resolved
	}
	return out, nil
}

func (r *runtimeResolver) resolveField(path string, field pluginmodel.ConfigField, classes map[string]pluginmodel.ConfigClass, value any) (any, error) {
	switch strings.TrimSpace(field.Type) {
	case "file":
		return r.resolveFile(path, field, value)
	case "secret":
		return r.resolveSecret(value)
	case "array":
		if field.Items == nil {
			return value, nil
		}
		items, ok := configSlice(value)
		if !ok {
			return value, nil
		}
		out := make([]any, 0, len(items))
		for i, item := range items {
			resolved, err := r.resolveField(fmt.Sprintf("%s[%d]", path, i), *field.Items, classes, item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, resolved)
		}
		return out, nil
	case "object":
		if field.Class == "" || field.Class == "JSONObject" {
			return value, nil
		}
		class, ok := classes[field.Class]
		if !ok {
			return value, nil
		}
		object, ok := configMap(value)
		if !ok {
			return value, nil
		}
		return r.resolveFields(path, class.Fields, classes, object)
	default:
		return value, nil
	}
}

func (r *runtimeResolver) resolveFile(path string, field pluginmodel.ConfigField, value any) (any, error) {
	assetID := ""
	version := 0
	if text, ok := value.(string); ok {
		assetID = strings.TrimSpace(text)
	} else if ref, ok := configMap(value); ok {
		assetID, _ = ref["asset_id"].(string)
		assetID = strings.TrimSpace(assetID)
		version = intFromAny(ref["version"])
	} else {
		return value, nil
	}
	if assetID == "" {
		return value, nil
	}
	if r.runtimeStore == nil {
		return nil, fmt.Errorf("plugin runtime store is unavailable")
	}
	asset, err := r.runtimeStore.GetPluginAsset(r.ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("load asset %s: %w", assetID, err)
	}
	if err := validateRuntimeAssetRef(path, field, asset, r.opts); err != nil {
		return nil, err
	}
	var record pluginmodel.AssetVersionRecord
	if version > 0 {
		record, err = r.runtimeStore.GetPluginAssetVersion(r.ctx, assetID, version)
	} else {
		record, err = r.runtimeStore.GetActivePluginAssetVersion(r.ctx, assetID)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve asset %s: %w", assetID, err)
	}
	if record.Status != "active" {
		return nil, fmt.Errorf("asset %s version %d is not active", assetID, record.Version)
	}
	r.assetVersions[assetID] = record.Version
	r.assetRefs = append(r.assetRefs, map[string]any{
		"field":    path,
		"asset_id": assetID,
		"version":  record.Version,
	})
	if strings.TrimSpace(record.StorageURI) == "" {
		return nil, fmt.Errorf("asset %s version %d has no storage_uri", assetID, record.Version)
	}
	if strings.HasPrefix(record.StorageURI, "db://") {
		if len(record.Content) == 0 && record.SizeBytes > 0 {
			return nil, fmt.Errorf("asset %s version %d has no db content", assetID, record.Version)
		}
		if r.tempDir == "" {
			r.tempDir, err = os.MkdirTemp("", "pulseops-plugin-assets-*")
			if err != nil {
				return nil, fmt.Errorf("create plugin asset temp dir: %w", err)
			}
		}
		r.counter++
		filename := safePathSegment(record.Filename)
		if filename == "" {
			filename = "asset-" + strconv.Itoa(record.Version)
		}
		target := filepath.Join(r.tempDir, strconv.Itoa(r.counter)+"-"+filename)
		if err := os.WriteFile(target, record.Content, 0600); err != nil {
			return nil, fmt.Errorf("write plugin asset temp file: %w", err)
		}
		return target, nil
	}
	if r.artifactStore == nil || r.artifactStore.Kind() == "disabled" {
		return nil, fmt.Errorf("plugin artifact store is unavailable")
	}
	key, err := store.ObjectKeyFromURI(record.StorageURI)
	if err != nil {
		return nil, err
	}
	reader, err := r.artifactStore.Get(r.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read asset %s version %d: %w", assetID, record.Version, err)
	}
	defer reader.Close()
	if r.tempDir == "" {
		r.tempDir, err = os.MkdirTemp("", "pulseops-plugin-assets-*")
		if err != nil {
			return nil, fmt.Errorf("create plugin asset temp dir: %w", err)
		}
	}
	r.counter++
	filename := safePathSegment(record.Filename)
	if filename == "" {
		filename = "asset-" + strconv.Itoa(record.Version)
	}
	target := filepath.Join(r.tempDir, strconv.Itoa(r.counter)+"-"+filename)
	file, err := os.Create(target)
	if err != nil {
		return nil, fmt.Errorf("create plugin asset temp file: %w", err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write plugin asset temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close plugin asset temp file: %w", err)
	}
	return target, nil
}

func validateRuntimeAssetRef(path string, field pluginmodel.ConfigField, asset pluginmodel.AssetRecord, opts RuntimeResolveOptions) error {
	if strings.TrimSpace(asset.Kind) != strings.TrimSpace(field.AssetKind) {
		return fmt.Errorf("%s asset %s kind is %q, want %q", path, asset.ID, asset.Kind, field.AssetKind)
	}
	if asset.Status != "active" {
		return fmt.Errorf("%s asset %s is not active", path, asset.ID)
	}
	if opts.PluginID != "" && asset.PluginID != opts.PluginID {
		return fmt.Errorf("%s asset %s belongs to plugin %q", path, asset.ID, asset.PluginID)
	}
	switch strings.TrimSpace(field.AssetScope) {
	case pluginmodel.AssetScopePluginShared:
		if asset.Scope != pluginmodel.AssetScopePluginShared {
			return fmt.Errorf("%s asset %s scope is %q, want %q", path, asset.ID, asset.Scope, pluginmodel.AssetScopePluginShared)
		}
	case pluginmodel.AssetScopeCapabilityShared:
		if asset.Scope != pluginmodel.AssetScopeCapabilityShared {
			return fmt.Errorf("%s asset %s scope is %q, want %q", path, asset.ID, asset.Scope, pluginmodel.AssetScopeCapabilityShared)
		}
		if opts.CapabilityID != "" && asset.CapabilityID != opts.CapabilityID {
			return fmt.Errorf("%s asset %s belongs to capability %q", path, asset.ID, asset.CapabilityID)
		}
	case pluginmodel.AssetScopeConfigInstance:
		if asset.Scope != pluginmodel.AssetScopeConfigInstance {
			return fmt.Errorf("%s asset %s scope is %q, want %q", path, asset.ID, asset.Scope, pluginmodel.AssetScopeConfigInstance)
		}
		if !containsString(opts.ConfigInstanceIDs, asset.ConfigInstanceID) {
			return fmt.Errorf("%s asset %s belongs to config instance %q", path, asset.ID, asset.ConfigInstanceID)
		}
	default:
		return fmt.Errorf("%s asset_scope %q is not supported", path, field.AssetScope)
	}
	return nil
}

func (r *runtimeResolver) resolveSecret(value any) (any, error) {
	if r.runtimeStore == nil {
		return nil, fmt.Errorf("plugin runtime store is unavailable")
	}
	secretID := ""
	if text, ok := value.(string); ok {
		secretID = strings.TrimSpace(text)
	} else if ref, ok := configMap(value); ok {
		secretID, _ = ref["secret_id"].(string)
		secretID = strings.TrimSpace(secretID)
	}
	if secretID == "" {
		return value, nil
	}
	secret, err := r.runtimeStore.GetPluginSecret(r.ctx, secretID)
	if err != nil {
		return nil, fmt.Errorf("load secret %s: %w", secretID, err)
	}
	if secret.Status != "active" {
		return nil, fmt.Errorf("secret %s is not active", secretID)
	}
	record, err := r.runtimeStore.GetPluginSecretValue(r.ctx, secretID)
	if err != nil {
		return nil, fmt.Errorf("load secret value %s: %w", secretID, err)
	}
	plaintext, err := DecryptSecret(record)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func (r *runtimeResolver) cleanup() {
	if r.tempDir != "" {
		_ = os.RemoveAll(r.tempDir)
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func safePathSegment(input string) string {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	if input == "" {
		return ""
	}
	input = strings.Trim(input, ".")
	if input == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_", " ", "_")
	return replacer.Replace(input)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
