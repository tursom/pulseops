package plugin

import (
	"strings"
	"testing"
)

func TestValidateConfigValuesAcceptsStructuredConfig(t *testing.T) {
	t.Parallel()

	minLen := 3
	min := 1.0
	max := 5.0
	schema := &ConfigSchema{Fields: map[string]ConfigField{
		"endpoint": {
			Type:     "string",
			Required: true,
			Validation: ConfigValidation{
				MinLen:  minLen,
				Pattern: "^[^:]+:[0-9]+$",
			},
		},
		"timeout": {
			Type: "number",
			Validation: ConfigValidation{
				Min: &min,
				Max: &max,
			},
		},
		"mode": {
			Type:    "select",
			Default: "reflection",
			Options: []ConfigOption{
				{Value: "reflection"},
				{Value: "proto_files"},
			},
		},
		"tags": {
			Type: "multi_select",
			Options: []ConfigOption{
				{Value: "prod"},
				{Value: "read"},
			},
		},
		"tls": {
			Type:  "object",
			Class: "TLSConfig",
		},
		"proto": {
			Type:       "file",
			AssetKind:  "proto_files",
			AssetScope: AssetScopeCapabilityShared,
		},
		"authorization": {
			Type: "secret",
		},
		"request": {
			Type:  "object",
			Class: "JSONObject",
		},
	}}
	classes := map[string]ConfigClass{
		"TLSConfig": {
			Fields: map[string]ConfigField{
				"enabled": {Type: "bool", Required: true},
				"server_name": {
					Type: "string",
				},
			},
		},
	}
	values := map[string]any{
		"endpoint":      "inventory.service:9090",
		"timeout":       3,
		"mode":          "proto_files",
		"tags":          []any{"prod", "read"},
		"tls":           map[string]any{"enabled": true, "server_name": "inventory.service"},
		"proto":         map[string]any{"asset_id": "asset-inventory-proto", "version": 2},
		"authorization": "sec-auth",
		"request":       map[string]any{"user_id": "{{ .Run.Labels.user_id }}"},
	}

	if err := ValidateConfigValues(schema, classes, values, ConfigValueValidationOptions{}); err != nil {
		t.Fatalf("validate config values: %v", err)
	}
}

func TestValidateConfigValuesRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	schema := &ConfigSchema{Fields: map[string]ConfigField{
		"endpoint": {
			Type:     "string",
			Required: true,
			Validation: ConfigValidation{
				Pattern: "^[^:]+:[0-9]+$",
			},
		},
		"mode": {
			Type: "select",
			Options: []ConfigOption{
				{Value: "reflection"},
			},
		},
		"tls": {
			Type:  "object",
			Class: "TLSConfig",
		},
		"headers": {
			Type: "array",
			Items: &ConfigField{
				Type:  "object",
				Class: "Header",
			},
		},
		"proto": {
			Type:       "file",
			AssetKind:  "proto_files",
			AssetScope: AssetScopeCapabilityShared,
		},
	}}
	classes := map[string]ConfigClass{
		"TLSConfig": {
			Fields: map[string]ConfigField{
				"enabled": {Type: "bool", Required: true},
			},
		},
		"Header": {
			Fields: map[string]ConfigField{
				"key": {Type: "string", Required: true},
			},
		},
	}
	values := map[string]any{
		"endpoint": "inventory.service",
		"mode":     "proto_files",
		"tls":      map[string]any{"server_name": "inventory.service"},
		"headers":  []any{map[string]any{"value": "missing-key"}},
		"proto":    map[string]any{"version": 1},
		"extra":    true,
	}

	err := ValidateConfigValues(schema, classes, values, ConfigValueValidationOptions{})
	if err == nil {
		t.Fatal("expected config value validation error")
	}
	msg := err.Error()
	for _, want := range []string{
		"config.extra is not declared in schema",
		`config.endpoint must match pattern`,
		"config.mode must be one of the declared options",
		"config.tls.enabled is required",
		"config.headers[0].key is required",
		"config.proto.asset_id is required",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to contain %q, got %q", want, msg)
		}
	}
}

func TestValidateConfigValuesEnforcesOverrideFlags(t *testing.T) {
	t.Parallel()

	schema := &ConfigSchema{Fields: map[string]ConfigField{
		"endpoint": {
			Type:        "string",
			Overridable: false,
		},
		"method": {
			Type:        "string",
			Overridable: true,
		},
	}}
	values := map[string]any{
		"endpoint": "inventory.service:9090",
		"method":   "GetInventory",
	}

	err := ValidateConfigValues(schema, nil, values, ConfigValueValidationOptions{Overrides: true})
	if err == nil {
		t.Fatal("expected override validation error")
	}
	if !strings.Contains(err.Error(), "config.endpoint is not overridable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigValuesAllowsPartialOverrides(t *testing.T) {
	t.Parallel()

	schema := &ConfigSchema{Fields: map[string]ConfigField{
		"endpoint": {
			Type:     "string",
			Required: true,
		},
		"service": {
			Type:     "string",
			Required: true,
		},
		"method": {
			Type:        "string",
			Required:    true,
			Overridable: true,
		},
	}}

	values := map[string]any{
		"method": "GetInventoryV2",
	}
	if err := ValidateConfigValues(schema, nil, values, ConfigValueValidationOptions{Overrides: true}); err != nil {
		t.Fatalf("partial override should not require untouched fields: %v", err)
	}

	values["method"] = nil
	err := ValidateConfigValues(schema, nil, values, ConfigValueValidationOptions{Overrides: true})
	if err == nil || !strings.Contains(err.Error(), "config.method is required") {
		t.Fatalf("expected nil required override error, got %v", err)
	}
}
