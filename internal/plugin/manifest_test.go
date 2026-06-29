package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestYAMLWithConfigSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFilename)
	raw := `schema_version: pulseops.plugin/v1
id: grpc-plugin
name: gRPC Plugin
version: 1.0.0
enabled: true
config_classes:
  TLSConfig:
    fields:
      enabled:
        type: bool
      ca_cert:
        type: file
        asset_kind: certificate
        asset_scope: config_instance
config:
  title: Plugin Config
  validate_action: validate_config
  fields:
    endpoint:
      type: string
      required: true
      overridable: true
      validation:
        pattern: "^[^:]+:[0-9]+$"
    tls:
      type: object
      class: TLSConfig
data_sources:
  - name: grpc
    title: gRPC
    protocol: grpc
    runtime: builtin
    config:
      allow_plugin_config_ref: true
      fields:
        schema_mode:
          type: select
          default: reflection
          options:
            - value: reflection
              label: Reflection
            - value: proto_files
              label: Proto files
        request:
          type: object
          class: JSONObject
          overridable: true
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, checksum, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if checksum == "" {
		t.Fatal("expected checksum")
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	if manifest.Config == nil || manifest.Config.Fields["endpoint"].Type != "string" {
		t.Fatalf("missing plugin config schema: %#v", manifest.Config)
	}
	if manifest.DataSources[0].Config == nil || !manifest.DataSources[0].Config.AllowPluginConfigRef {
		t.Fatalf("missing capability config schema: %#v", manifest.DataSources[0].Config)
	}
}

func TestValidateManifestRejectsInvalidConfigSchema(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion: SchemaVersionV1,
		ID:            "bad-config",
		Name:          "Bad Config",
		Version:       "1.0.0",
		Config: &ConfigSchema{Fields: map[string]ConfigField{
			"headers": {
				Type: "array",
			},
			"mode": {
				Type: "select",
			},
			"tls": {
				Type:  "object",
				Class: "MissingClass",
			},
		}},
	}
	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected config schema validation error")
	}
	msg := err.Error()
	for _, want := range []string{"headers.items is required", "mode.options is required", `tls.class "MissingClass"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to contain %q, got %q", want, msg)
		}
	}
}

func TestValidateManifestRejectsConfigClassCycleAndBadCondition(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion: SchemaVersionV1,
		ID:            "bad-config",
		Name:          "Bad Config",
		Version:       "1.0.0",
		ConfigClasses: map[string]ConfigClass{
			"A": {
				Fields: map[string]ConfigField{
					"b": {Type: "object", Class: "B"},
				},
			},
			"B": {
				Fields: map[string]ConfigField{
					"a": {Type: "object", Class: "A"},
				},
			},
		},
		Config: &ConfigSchema{Fields: map[string]ConfigField{
			"mode": {
				Type: "select",
				Options: []ConfigOption{
					{Value: "reflection"},
				},
			},
			"descriptor": {
				Type:       "file",
				AssetKind:  "proto_descriptor_set",
				AssetScope: AssetScopeCapabilityShared,
				UI: ConfigUI{VisibleWhen: &ConfigCondition{
					Field: "mode",
					Op:    "starts_with",
					Value: "descriptor",
				}},
			},
		}},
	}
	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected manifest validation error")
	}
	msg := err.Error()
	for _, want := range []string{`class "A" forms a cycle`, `ui.visible_when.op "starts_with" is not supported`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to contain %q, got %q", want, msg)
		}
	}
}

func TestValidateManifestRejectsFileFieldWithoutAssetScope(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SchemaVersion: SchemaVersionV1,
		ID:            "bad-file",
		Name:          "Bad File",
		Version:       "1.0.0",
		Config: &ConfigSchema{Fields: map[string]ConfigField{
			"descriptor": {
				Type:      "file",
				AssetKind: "proto_descriptor_set",
			},
		}},
	}
	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected file asset_scope validation error")
	}
	if !strings.Contains(err.Error(), `descriptor.asset_scope "" is not supported`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateReleaseManifestFilesRejectsLegacyTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LegacyManifestFilename), []byte("schema_version = \"pulseops.plugin/v1\"\n"), 0644); err != nil {
		t.Fatalf("write legacy manifest: %v", err)
	}
	err := ValidateReleaseManifestFiles(dir)
	if err == nil {
		t.Fatal("expected legacy manifest rejection")
	}
	if !strings.Contains(err.Error(), "is not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
