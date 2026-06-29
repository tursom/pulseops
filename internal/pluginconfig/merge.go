package pluginconfig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

type ConfigReader interface {
	GetPluginConfigInstance(ctx context.Context, instanceID string) (pluginmodel.ConfigInstanceRecord, error)
	GetActivePluginConfigVersion(ctx context.Context, instanceID string) (pluginmodel.ConfigVersionRecord, error)
}

type ResolveCapabilityOptions struct {
	PluginConfigRef     string
	CapabilityConfigRef string
	Config              map[string]any
	Overrides           map[string]any
	RuntimeStore        RuntimeStore
	ArtifactStore       store.ArtifactStore
}

type ResolveCapabilityResult struct {
	Config         map[string]any
	ConfigVersions map[string]any
	AssetVersions  map[string]any
	TaskOverrides  map[string]any
	Cleanup        func()
}

func ResolveCapabilityConfig(ctx context.Context, reader ConfigReader, capability pluginmodel.Capability, opts ResolveCapabilityOptions) (ResolveCapabilityResult, error) {
	result := ResolveCapabilityResult{
		ConfigVersions: map[string]any{},
		AssetVersions:  map[string]any{},
		TaskOverrides:  map[string]any{},
		Cleanup:        func() {},
	}
	defaultConfig, defaultPluginRef, defaultCapabilityRef := splitReferenceFields(capability.Defaults, true)
	configMap, configPluginRef, configCapabilityRef := splitReferenceFields(opts.Config, false)
	pluginConfigRef := firstString(opts.PluginConfigRef, configPluginRef, defaultPluginRef)
	capabilityConfigRef := firstString(opts.CapabilityConfigRef, configCapabilityRef, defaultCapabilityRef)
	out := ConfigSchemaDefaults(capability.Config)
	MergeAnyMap(out, defaultConfig)
	configInstanceIDs := make([]string, 0, 2)
	if pluginConfigRef != "" || capabilityConfigRef != "" {
		if reader == nil {
			return result, fmt.Errorf("plugin config store is unavailable")
		}
	}
	if pluginConfigRef != "" {
		if capability.Config == nil || !capability.Config.AllowPluginConfigRef {
			return result, fmt.Errorf("capability %q does not allow plugin_config_ref", capability.ID)
		}
		values, version, err := ActiveConfigValues(ctx, reader, pluginConfigRef, capability, "plugin")
		if err != nil {
			return result, err
		}
		result.ConfigVersions[pluginConfigRef] = version
		configInstanceIDs = append(configInstanceIDs, pluginConfigRef)
		MergeAnyMap(out, values)
	}
	if capabilityConfigRef != "" {
		values, version, err := ActiveConfigValues(ctx, reader, capabilityConfigRef, capability, "capability")
		if err != nil {
			return result, err
		}
		result.ConfigVersions[capabilityConfigRef] = version
		configInstanceIDs = append(configInstanceIDs, capabilityConfigRef)
		MergeAnyMap(out, values)
	}
	MergeAnyMap(out, configMap)
	if len(opts.Overrides) > 0 {
		if err := ValidateCapabilityOverrides(capability, opts.Overrides); err != nil {
			return result, err
		}
		MergeAnyMap(out, opts.Overrides)
		result.TaskOverrides[capability.Name] = opts.Overrides
	}
	if capability.Config != nil {
		if err := ValidateValues(capability.Config, capability.ConfigClasses, out, ValidationOptions{}); err != nil {
			return result, fmt.Errorf("validate capability config: %w", err)
		}
	}
	resolved, assetVersions, _, cleanup, err := ResolveRuntimeValuesWithOptions(ctx, opts.RuntimeStore, opts.ArtifactStore, capability.Config, capability.ConfigClasses, out, RuntimeResolveOptions{
		PluginID:          capability.PluginID,
		CapabilityID:      capability.ID,
		ConfigInstanceIDs: configInstanceIDs,
	})
	if cleanup != nil {
		result.Cleanup = cleanup
	}
	MergeAnyMap(result.AssetVersions, assetVersions)
	if err != nil {
		return result, err
	}
	result.Config = resolved
	return result, nil
}

func ActiveConfigValues(ctx context.Context, reader ConfigReader, instanceID string, capability pluginmodel.Capability, wantScope string) (map[string]any, int, error) {
	instance, err := reader.GetPluginConfigInstance(ctx, instanceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, fmt.Errorf("plugin config instance %q not found", instanceID)
		}
		return nil, 0, fmt.Errorf("load plugin config instance %q: %w", instanceID, err)
	}
	if instance.Status != "active" {
		return nil, 0, fmt.Errorf("plugin config instance %q is not active", instanceID)
	}
	if instance.Scope != wantScope {
		return nil, 0, fmt.Errorf("plugin config instance %q scope is %q, want %q", instanceID, instance.Scope, wantScope)
	}
	if capability.PluginID != "" && instance.PluginID != capability.PluginID {
		return nil, 0, fmt.Errorf("plugin config instance %q belongs to plugin %q", instanceID, instance.PluginID)
	}
	if wantScope == "capability" && capability.ID != "" && instance.CapabilityID != capability.ID {
		return nil, 0, fmt.Errorf("plugin config instance %q belongs to capability %q", instanceID, instance.CapabilityID)
	}
	version, err := reader.GetActivePluginConfigVersion(ctx, instanceID)
	if err != nil {
		return nil, 0, fmt.Errorf("load active plugin config version for %q: %w", instanceID, err)
	}
	if version.Status != "active" {
		return nil, 0, fmt.Errorf("plugin config instance %q active version is %q", instanceID, version.Status)
	}
	return version.Values, version.Version, nil
}

func ConfigSchemaDefaults(schema *pluginmodel.ConfigSchema) map[string]any {
	out := map[string]any{}
	if schema == nil {
		return out
	}
	for key, field := range schema.Fields {
		if field.Default != nil {
			out[key] = field.Default
		}
	}
	return out
}

func ValidateCapabilityOverrides(capability pluginmodel.Capability, overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}
	if capability.Config == nil {
		return fmt.Errorf("capability %q does not declare config schema", capability.ID)
	}
	if err := ValidateValues(capability.Config, capability.ConfigClasses, overrides, ValidationOptions{Overrides: true}); err != nil {
		return err
	}
	return nil
}

func MergeAnyMap(dst map[string]any, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func splitReferenceFields(input map[string]any, nestedConfig bool) (map[string]any, string, string) {
	out := map[string]any{}
	var pluginRef string
	var capabilityRef string
	for key, value := range input {
		switch key {
		case "plugin_config_ref":
			if pluginRef == "" {
				pluginRef, _ = value.(string)
				pluginRef = strings.TrimSpace(pluginRef)
			}
		case "capability_config_ref", "config_ref":
			if capabilityRef == "" {
				capabilityRef, _ = value.(string)
				capabilityRef = strings.TrimSpace(capabilityRef)
			}
		case "config":
			if nestedConfig {
				MergeAnyMap(out, MapFromAny(value))
			} else {
				out[key] = value
			}
		case "overrides":
			if !nestedConfig {
				out[key] = value
			}
		default:
			out[key] = value
		}
	}
	return out, pluginRef, capabilityRef
}

func MapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
