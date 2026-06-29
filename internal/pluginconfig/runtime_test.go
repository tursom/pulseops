package pluginconfig

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"pulseops/internal/pluginmodel"
	"pulseops/internal/store"
)

func TestResolveRuntimeValuesEnforcesAssetScope(t *testing.T) {
	t.Parallel()

	schema := &pluginmodel.ConfigSchema{Fields: map[string]pluginmodel.ConfigField{
		"proto": {
			Type:       "file",
			AssetKind:  "proto_file",
			AssetScope: pluginmodel.AssetScopeCapabilityShared,
		},
		"cert": {
			Type:       "file",
			AssetKind:  "certificate",
			AssetScope: pluginmodel.AssetScopeConfigInstance,
		},
	}}
	runtimeStore := &runtimeTestStore{
		assets: map[string]pluginmodel.AssetRecord{
			"asset-proto": {
				ID:           "asset-proto",
				PluginID:     "@pulseops/grpc-source",
				CapabilityID: "@pulseops/grpc-source:data_source:grpc",
				Scope:        pluginmodel.AssetScopeCapabilityShared,
				Kind:         "proto_file",
				Status:       "active",
			},
			"asset-cert": {
				ID:               "asset-cert",
				PluginID:         "@pulseops/grpc-source",
				CapabilityID:     "@pulseops/grpc-source:data_source:grpc",
				ConfigInstanceID: "cfg-grpc-prod",
				Scope:            pluginmodel.AssetScopeConfigInstance,
				Kind:             "certificate",
				Status:           "active",
			},
		},
		assetVersions: map[string]pluginmodel.AssetVersionRecord{
			"asset-proto": {
				AssetID:    "asset-proto",
				Version:    2,
				Status:     "active",
				Filename:   "inventory.proto",
				StorageURI: "db://plugin-assets/grpc/asset-proto/2/inventory.proto",
				Content:    []byte("syntax = \"proto3\";"),
				SizeBytes:  int64(len("syntax = \"proto3\";")),
			},
			"asset-cert": {
				AssetID:    "asset-cert",
				Version:    1,
				Status:     "active",
				Filename:   "client.crt",
				StorageURI: "db://plugin-assets/grpc/asset-cert/1/client.crt",
				Content:    []byte("cert"),
				SizeBytes:  4,
			},
		},
	}

	resolved, assetVersions, assetRefs, cleanup, err := ResolveRuntimeValuesWithOptions(
		context.Background(),
		runtimeStore,
		store.DisabledArtifactStore{Reason: "test"},
		schema,
		nil,
		map[string]any{
			"proto": "asset-proto",
			"cert":  "asset-cert",
		},
		RuntimeResolveOptions{
			PluginID:          "@pulseops/grpc-source",
			CapabilityID:      "@pulseops/grpc-source:data_source:grpc",
			ConfigInstanceIDs: []string{"cfg-grpc-prod"},
		},
	)
	if err != nil {
		t.Fatalf("resolve runtime values: %v", err)
	}
	defer cleanup()
	if resolved["proto"] == "asset-proto" || resolved["cert"] == "asset-cert" {
		t.Fatalf("expected file references to resolve to temp paths, got %#v", resolved)
	}
	if assetVersions["asset-proto"] != 2 || assetVersions["asset-cert"] != 1 {
		t.Fatalf("unexpected asset version trace: %#v", assetVersions)
	}
	if len(assetRefs) != 2 {
		t.Fatalf("expected two asset refs, got %#v", assetRefs)
	}
}

func TestResolveRuntimeValuesRejectsWrongAssetKind(t *testing.T) {
	t.Parallel()

	schema := &pluginmodel.ConfigSchema{Fields: map[string]pluginmodel.ConfigField{
		"proto": {
			Type:       "file",
			AssetKind:  "proto_file",
			AssetScope: pluginmodel.AssetScopeCapabilityShared,
		},
	}}
	runtimeStore := &runtimeTestStore{
		assets: map[string]pluginmodel.AssetRecord{
			"asset-cert": {
				ID:           "asset-cert",
				PluginID:     "@pulseops/grpc-source",
				CapabilityID: "@pulseops/grpc-source:data_source:grpc",
				Scope:        pluginmodel.AssetScopeCapabilityShared,
				Kind:         "certificate",
				Status:       "active",
			},
		},
		assetVersions: map[string]pluginmodel.AssetVersionRecord{
			"asset-cert": {
				AssetID:    "asset-cert",
				Version:    1,
				Status:     "active",
				Filename:   "client.crt",
				StorageURI: "db://plugin-assets/grpc/asset-cert/1/client.crt",
				Content:    []byte("cert"),
				SizeBytes:  4,
			},
		},
	}

	_, _, _, cleanup, err := ResolveRuntimeValuesWithOptions(
		context.Background(),
		runtimeStore,
		store.DisabledArtifactStore{Reason: "test"},
		schema,
		nil,
		map[string]any{"proto": "asset-cert"},
		RuntimeResolveOptions{
			PluginID:     "@pulseops/grpc-source",
			CapabilityID: "@pulseops/grpc-source:data_source:grpc",
		},
	)
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), `kind is "certificate", want "proto_file"`) {
		t.Fatalf("expected wrong kind error, got %v", err)
	}
}

func TestResolveRuntimeValuesRejectsOtherConfigInstanceAsset(t *testing.T) {
	t.Parallel()

	schema := &pluginmodel.ConfigSchema{Fields: map[string]pluginmodel.ConfigField{
		"cert": {
			Type:       "file",
			AssetKind:  "certificate",
			AssetScope: pluginmodel.AssetScopeConfigInstance,
		},
	}}
	runtimeStore := &runtimeTestStore{
		assets: map[string]pluginmodel.AssetRecord{
			"asset-cert": {
				ID:               "asset-cert",
				PluginID:         "@pulseops/grpc-source",
				CapabilityID:     "@pulseops/grpc-source:data_source:grpc",
				ConfigInstanceID: "cfg-other",
				Scope:            pluginmodel.AssetScopeConfigInstance,
				Kind:             "certificate",
				Status:           "active",
			},
		},
		assetVersions: map[string]pluginmodel.AssetVersionRecord{
			"asset-cert": {
				AssetID:    "asset-cert",
				Version:    1,
				Status:     "active",
				Filename:   "client.crt",
				StorageURI: "db://plugin-assets/grpc/asset-cert/1/client.crt",
				Content:    []byte("cert"),
				SizeBytes:  4,
			},
		},
	}

	_, _, _, cleanup, err := ResolveRuntimeValuesWithOptions(
		context.Background(),
		runtimeStore,
		store.DisabledArtifactStore{Reason: "test"},
		schema,
		nil,
		map[string]any{"cert": "asset-cert"},
		RuntimeResolveOptions{
			PluginID:          "@pulseops/grpc-source",
			CapabilityID:      "@pulseops/grpc-source:data_source:grpc",
			ConfigInstanceIDs: []string{"cfg-grpc-prod"},
		},
	)
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), `belongs to config instance "cfg-other"`) {
		t.Fatalf("expected config instance ownership error, got %v", err)
	}
}

type runtimeTestStore struct {
	assets        map[string]pluginmodel.AssetRecord
	assetVersions map[string]pluginmodel.AssetVersionRecord
}

func (s *runtimeTestStore) GetPluginAsset(_ context.Context, assetID string) (pluginmodel.AssetRecord, error) {
	if record, ok := s.assets[assetID]; ok {
		return record, nil
	}
	return pluginmodel.AssetRecord{}, sql.ErrNoRows
}

func (s *runtimeTestStore) GetPluginAssetVersion(_ context.Context, assetID string, version int) (pluginmodel.AssetVersionRecord, error) {
	record, ok := s.assetVersions[assetID]
	if !ok || record.Version != version {
		return pluginmodel.AssetVersionRecord{}, sql.ErrNoRows
	}
	return record, nil
}

func (s *runtimeTestStore) GetActivePluginAssetVersion(_ context.Context, assetID string) (pluginmodel.AssetVersionRecord, error) {
	if record, ok := s.assetVersions[assetID]; ok {
		return record, nil
	}
	return pluginmodel.AssetVersionRecord{}, sql.ErrNoRows
}

func (s *runtimeTestStore) GetPluginSecret(_ context.Context, secretID string) (pluginmodel.SecretRecord, error) {
	return pluginmodel.SecretRecord{}, sql.ErrNoRows
}

func (s *runtimeTestStore) GetPluginSecretValue(_ context.Context, secretID string) (pluginmodel.SecretValueRecord, error) {
	return pluginmodel.SecretValueRecord{}, sql.ErrNoRows
}
