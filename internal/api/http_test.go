package api

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"pulseops/internal/config"
	pluginmgr "pulseops/internal/plugin"
	"pulseops/internal/pluginconfig"
	"pulseops/internal/store"
	"pulseops/internal/task"
)

func TestArtifactEndpoints(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		artifactsByRun: map[string][]store.ArtifactRef{
			"task-a/run-1": {{
				ArtifactID:  "artifact-1",
				Kind:        "payload",
				StorageKind: "s3",
				URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
				ContentType: "application/json",
				SizeBytes:   10,
				SHA256:      "abc",
				PreviewText: "{}",
			}},
		},
		artifactsByID: map[string]store.ArtifactRef{
			"artifact-1": {
				ArtifactID:  "artifact-1",
				Kind:        "payload",
				StorageKind: "s3",
				URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
				ContentType: "application/json",
				SizeBytes:   10,
				SHA256:      "abc",
				PreviewText: "{}",
			},
		},
	}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"artifact_id":"artifact-1"`) {
		t.Fatalf("unexpected artifacts response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/artifacts/artifact-1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"download_url":"https://download.local/object"`) {
		t.Fatalf("unexpected artifact detail response: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRunDetailHydratesPayloadFromArtifact(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		runs: map[string]store.RunRecord{
			"task-a/run-1": {
				RunID:       "run-1",
				TaskID:      "task-a",
				TaskKind:    "http_check",
				TriggerType: "manual",
				RunStatus:   "success",
				CheckStatus: "pass",
				StartedAt:   time.Now(),
				EndedAt:     time.Now(),
				ArtifactRefs: []store.ArtifactRef{{
					ArtifactID:  "artifact-1",
					Kind:        "payload",
					StorageKind: "s3",
					URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
					ContentType: "application/json",
				}},
			},
		},
	}, &fakeArtifactStore{
		bodies: map[string]string{
			"prod/task-a/run-1/payload.json": `{"body":{"status":"ok"}}`,
		},
	}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"payload":{"body":{"status":"ok"}}`) {
		t.Fatalf("expected hydrated payload object, got %s", rec.Body.String())
	}
}

func TestRunDetailKeepsInlinePayload(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		runs: map[string]store.RunRecord{
			"task-a/run-1": {
				RunID:       "run-1",
				TaskID:      "task-a",
				TaskKind:    "http_check",
				TriggerType: "manual",
				RunStatus:   "success",
				CheckStatus: "pass",
				StartedAt:   time.Now(),
				EndedAt:     time.Now(),
				Payload:     []byte(`{"inline":true}`),
				ArtifactRefs: []store.ArtifactRef{{
					ArtifactID:  "artifact-1",
					Kind:        "payload",
					StorageKind: "s3",
					URI:         "s3://pulseops-artifacts/prod/task-a/run-1/payload.json",
					ContentType: "application/json",
				}},
			},
		},
	}, &fakeArtifactStore{
		bodies: map[string]string{
			"prod/task-a/run-1/payload.json": `{"inline":false}`,
		},
	}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a/runs/run-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"payload":{"inline":true}`) {
		t.Fatalf("expected inline payload to be preserved, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"inline":false`) {
		t.Fatalf("artifact payload should not override inline payload: %s", rec.Body.String())
	}
}

func TestTaskViewIncludesDefinitionAndDependencies(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{{
			TaskID:  "task-a",
			Name:    "Task A",
			Kind:    "http_check",
			Enabled: true,
			Labels:  map[string]string{"env": "test"},
		}},
		dependencies: []config.TaskDependency{{
			ID:               "dep-1",
			UpstreamTaskID:   "source-a",
			DownstreamTaskID: "task-a",
			Condition:        "run_status == success",
		}},
	}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-a", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"config_status":"valid"`) || !strings.Contains(body, `"upstream_count":1`) {
		t.Fatalf("expected task view contract, got %s", body)
	}
	if !strings.Contains(body, `"definition"`) || !strings.Contains(body, `"dependencies"`) {
		t.Fatalf("expected definition and dependencies, got %s", body)
	}
}

func TestDashboardSummaryReturnsAggregates(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{{
			TaskID:  "task-a",
			Name:    "Task A",
			Kind:    "http_check",
			Enabled: true,
			Labels:  map[string]string{"env": "test"},
		}},
		runItems: []store.RunListItem{{
			RunID:       "run-1",
			TaskID:      "task-a",
			TaskName:    "Task A",
			TaskKind:    "http_check",
			RunStatus:   "success",
			CheckStatus: "pass",
			StartedAt:   time.Now(),
			EndedAt:     time.Now(),
		}},
	}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"counts"`) || !strings.Contains(body, `"recent_runs"`) || !strings.Contains(body, `"label_groups"`) {
		t.Fatalf("expected dashboard summary contract, got %s", body)
	}
}

func TestDashboardSummaryReturnsEmptyLists(t *testing.T) {
	t.Parallel()

	handler := Routes("", &emptyTaskManager{}, &fakeRepository{}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, field := range []string{`"anomalies":[]`, `"recent_runs":[]`, `"label_groups":[]`} {
		if !strings.Contains(body, field) {
			t.Fatalf("expected dashboard summary field %s to be an empty list, got %s", field, body)
		}
	}
}

func TestWriteJSONNormalizesNilCollections(t *testing.T) {
	t.Parallel()

	type nestedPayload struct {
		Items []string          `json:"items"`
		Meta  map[string]string `json:"meta"`
	}

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]any{
		"items":  []string(nil),
		"meta":   map[string]string(nil),
		"nested": nestedPayload{},
	})

	body := rec.Body.String()
	for _, field := range []string{`"items":[]`, `"meta":{}`, `"nested":{"items":[],"meta":{}}`} {
		if !strings.Contains(body, field) {
			t.Fatalf("expected normalized field %s, got %s", field, body)
		}
	}
}

func TestTaskGraphReturnsDependencyEdges(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{
		defs: []config.TaskDefinition{
			{TaskID: "source-a", Name: "Source A", Kind: "http_check", Enabled: true},
			{TaskID: "task-a", Name: "Task A", Kind: "data_process", Enabled: true},
		},
		dependencies: []config.TaskDependency{{
			ID:               "dep-1",
			UpstreamTaskID:   "source-a",
			DownstreamTaskID: "task-a",
			Condition:        "check_status == pass",
		}},
	}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/task-graph", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"nodes"`) || !strings.Contains(body, `"edges"`) || !strings.Contains(body, `"upstream_task_id":"source-a"`) {
		t.Fatalf("expected task graph contract, got %s", body)
	}
}

func TestTaskDefinitionValidateRejectsInvalidCondition(t *testing.T) {
	t.Parallel()

	handler := Routes("", &fakeTaskManager{}, &fakeRepository{}, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/task-defs/validate", strings.NewReader(`{
		"task_id":"task-a",
		"name":"Task A",
		"kind":"http_check",
		"enabled":true,
		"trigger":"on_run",
		"watch_task_id":"source-a",
		"watch_condition":"unknown == value"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"valid":false`) {
		t.Fatalf("expected invalid validation response, got %s", rec.Body.String())
	}
}

func TestPluginConfigSchemaEndpoints(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	capabilityID := pluginmgr.CapabilityID(pluginID, pluginmgr.CapabilityDataSource, "grpc")
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
				Manifest: pluginmgr.Manifest{
					ConfigClasses: map[string]pluginmgr.ConfigClass{
						"TLSConfig": {Fields: map[string]pluginmgr.ConfigField{"enabled": {Type: "bool"}}},
					},
					Config: &pluginmgr.ConfigSchema{Fields: map[string]pluginmgr.ConfigField{
						"endpoint": {Type: "string", Required: true},
					}},
				},
			},
		},
		capabilities: []pluginmgr.Capability{{
			ID:            capabilityID,
			Type:          pluginmgr.CapabilityDataSource,
			Name:          "grpc",
			PluginID:      pluginID,
			PluginVersion: "1.0.0",
			Config: &pluginmgr.ConfigSchema{Fields: map[string]pluginmgr.ConfigField{
				"method": {Type: "string", Overridable: true},
			}},
		}},
	}
	handler := Routes("", &fakeTaskManager{}, &fakeRepository{}, &fakeArtifactStore{}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/"+url.PathEscape(pluginID)+"/config-schema", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected plugin config schema status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"endpoint"`) || !strings.Contains(rec.Body.String(), `"TLSConfig"`) {
		t.Fatalf("expected plugin config schema, got %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-capabilities/"+url.PathEscape(capabilityID)+"/config-schema", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected capability config schema status: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"capability_id":"`+capabilityID+`"`) || !strings.Contains(rec.Body.String(), `"method"`) {
		t.Fatalf("expected capability config schema, got %s", rec.Body.String())
	}
}

func TestPluginConfigReadEndpoints(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	repo := &fakePluginConfigRepository{
		configInstances: []pluginmgr.ConfigInstanceRecord{{
			ID:             "cfg-grpc-prod",
			PluginID:       "@pulseops/grpc-source",
			CapabilityID:   "@pulseops/grpc-source:data_source:grpc",
			CapabilityType: "data_source",
			CapabilityName: "grpc",
			Scope:          "capability",
			Title:          "gRPC prod",
			Status:         "active",
			ActiveVersion:  2,
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
		configVersions: map[string][]pluginmgr.ConfigVersionRecord{
			"cfg-grpc-prod": {{
				InstanceID: "cfg-grpc-prod",
				Version:    2,
				Status:     "active",
				Values:     map[string]any{"endpoint": "inventory.service:9090"},
				CreatedAt:  now,
				UpdatedAt:  now,
			}},
		},
		assets: []pluginmgr.AssetRecord{{
			ID:            "asset-inventory-proto",
			PluginID:      "@pulseops/grpc-source",
			CapabilityID:  "@pulseops/grpc-source:data_source:grpc",
			Scope:         pluginmgr.AssetScopeCapabilityShared,
			Kind:          "proto_files",
			Title:         "Inventory proto",
			Status:        "active",
			ActiveVersion: 1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
		assetVersions: map[string][]pluginmgr.AssetVersionRecord{
			"asset-inventory-proto": {{
				AssetID:    "asset-inventory-proto",
				Version:    1,
				Status:     "active",
				Filename:   "inventory.pb",
				StorageURI: "s3://pulseops/plugins/inventory.pb",
				CreatedAt:  now,
				UpdatedAt:  now,
			}},
		},
		secrets: []pluginmgr.SecretRecord{{
			ID:        "sec-auth",
			PluginID:  "@pulseops/grpc-source",
			Scope:     "grpc-prod",
			Title:     "Authorization",
			Masked:    "********",
			Status:    "active",
			CreatedAt: now,
			UpdatedAt: now,
		}},
		configEvents: []pluginmgr.ConfigEventRecord{{
			ID:           7,
			ResourceType: "config_version",
			ResourceID:   "cfg-grpc-prod:2",
			PluginID:     "@pulseops/grpc-source",
			Action:       "activate",
			Status:       "success",
			CreatedAt:    now,
		}},
	}
	handler := Routes("", &fakeTaskManager{}, repo, &fakeArtifactStore{}, nil, nil, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/plugin-configs?plugin_id="+url.QueryEscape("@pulseops/grpc-source"), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cfg-grpc-prod"`) {
		t.Fatalf("unexpected plugin configs response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-configs/cfg-grpc-prod", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"active"`) || !strings.Contains(rec.Body.String(), `"inventory.service:9090"`) {
		t.Fatalf("unexpected plugin config detail response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-assets?kind=proto_files", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"asset-inventory-proto"`) {
		t.Fatalf("unexpected plugin assets response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-assets/asset-inventory-proto", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"inventory.pb"`) {
		t.Fatalf("unexpected plugin asset detail response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-secrets?scope=grpc-prod", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"masked":"********"`) || strings.Contains(body, "ciphertext") {
		t.Fatalf("unexpected plugin secrets response: %d %s", rec.Code, body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-secrets/sec-auth", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"sec-auth"`) || strings.Contains(body, "ciphertext") {
		t.Fatalf("unexpected plugin secret detail response: %d %s", rec.Code, body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugin-config-events?plugin_id="+url.QueryEscape("@pulseops/grpc-source")+"&limit=10", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"resource_id":"cfg-grpc-prod:2"`) || !strings.Contains(body, `"action":"activate"`) {
		t.Fatalf("unexpected plugin config events response: %d %s", rec.Code, body)
	}
}

func TestPluginConfigVersionWriteValidateAndActivate(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	capabilityID := pluginmgr.CapabilityID(pluginID, pluginmgr.CapabilityDataSource, "grpc")
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
				Manifest: pluginmgr.Manifest{
					ConfigClasses: map[string]pluginmgr.ConfigClass{
						"RequestTemplate": {Fields: map[string]pluginmgr.ConfigField{"body": {Type: "object", Class: "JSONObject"}}},
					},
					Config: &pluginmgr.ConfigSchema{Fields: map[string]pluginmgr.ConfigField{
						"endpoint": {Type: "string", Required: true},
					}},
				},
			},
		},
		capabilities: []pluginmgr.Capability{{
			ID:            capabilityID,
			Type:          pluginmgr.CapabilityDataSource,
			Name:          "grpc",
			Title:         "gRPC",
			PluginID:      pluginID,
			PluginVersion: "1.0.0",
			Config: &pluginmgr.ConfigSchema{Fields: map[string]pluginmgr.ConfigField{
				"service": {Type: "string", Required: true},
				"method":  {Type: "string", Required: true, Overridable: true},
			}, ValidateAction: "validate_config"},
		}},
	}
	repo := &fakePluginConfigRepository{configVersions: map[string][]pluginmgr.ConfigVersionRecord{}}
	handler := Routes("", &fakeTaskManager{}, repo, &fakeArtifactStore{}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/plugin-configs", strings.NewReader(`{
		"id":"cfg-grpc-prod",
		"plugin_id":"@pulseops/grpc-source",
		"capability_id":"@pulseops/grpc-source:data_source:grpc",
		"scope":"capability",
		"title":"gRPC prod"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"cfg-grpc-prod"`) {
		t.Fatalf("unexpected config create response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions", strings.NewReader(`{
		"values":{"method":"GetInventory"}
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"version":1`) {
		t.Fatalf("unexpected version create response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions/1/validate", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"valid":false`) || !strings.Contains(rec.Body.String(), `service is required`) {
		t.Fatalf("unexpected invalid validation response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/plugin-configs/cfg-grpc-prod/versions/1", strings.NewReader(`{
		"values":{"service":"yym.inventory.v1.InventoryService","method":"GetInventory"}
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"draft"`) {
		t.Fatalf("unexpected version update response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions/1/validate", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) || !strings.Contains(rec.Body.String(), `"status":"validated"`) {
		t.Fatalf("unexpected valid validation response: %d %s", rec.Code, rec.Body.String())
	}
	if len(pm.validateConfigCalls) != 1 || pm.validateConfigCalls[0].Action != "validate_config" {
		t.Fatalf("expected validate_config call, got %#v", pm.validateConfigCalls)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions/1/activate", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"instance"`) || !strings.Contains(rec.Body.String(), `"active"`) || !strings.Contains(rec.Body.String(), `"active_version":1`) {
		t.Fatalf("unexpected activate response: %d %s", rec.Code, rec.Body.String())
	}
	assertConfigEvent(t, repo.configEvents, "config_version", "create", "success")
	assertConfigEvent(t, repo.configEvents, "config_version", "update", "success")
	assertConfigEvent(t, repo.configEvents, "config_version", "validate", "success")
	assertConfigEvent(t, repo.configEvents, "config_version", "activate", "success")
}

func TestPluginConfigValidateChecksAssetRefsWithoutValidateAction(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	capabilityID := pluginmgr.CapabilityID(pluginID, pluginmgr.CapabilityDataSource, "grpc")
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
			},
		},
		capabilities: []pluginmgr.Capability{{
			ID:       capabilityID,
			Type:     pluginmgr.CapabilityDataSource,
			Name:     "grpc",
			PluginID: pluginID,
			Config: &pluginmgr.ConfigSchema{Fields: map[string]pluginmgr.ConfigField{
				"proto": {
					Type:       "file",
					AssetKind:  "proto_file",
					AssetScope: pluginmgr.AssetScopeCapabilityShared,
					Required:   true,
				},
			}},
		}},
	}
	now := time.Now().UTC()
	repo := &fakePluginConfigRepository{
		configInstances: []pluginmgr.ConfigInstanceRecord{{
			ID:             "cfg-grpc-prod",
			PluginID:       pluginID,
			CapabilityID:   capabilityID,
			CapabilityType: pluginmgr.CapabilityDataSource,
			CapabilityName: "grpc",
			Scope:          "capability",
			Status:         "draft",
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
		configVersions: map[string][]pluginmgr.ConfigVersionRecord{
			"cfg-grpc-prod": {{
				InstanceID: "cfg-grpc-prod",
				Version:    1,
				Status:     "draft",
				Values:     map[string]any{"proto": "asset-cert"},
				CreatedAt:  now,
				UpdatedAt:  now,
			}},
		},
		assets: []pluginmgr.AssetRecord{{
			ID:            "asset-cert",
			PluginID:      pluginID,
			CapabilityID:  capabilityID,
			Scope:         pluginmgr.AssetScopeCapabilityShared,
			Kind:          "certificate",
			Title:         "Wrong kind",
			Status:        "active",
			ActiveVersion: 1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
		assetVersions: map[string][]pluginmgr.AssetVersionRecord{
			"asset-cert": {{
				AssetID:    "asset-cert",
				Version:    1,
				Status:     "active",
				Filename:   "client.crt",
				StorageURI: "db://plugin-assets/pulseops/grpc/client.crt",
				Content:    []byte("cert"),
				SizeBytes:  4,
				CreatedAt:  now,
				UpdatedAt:  now,
			}},
		},
	}
	handler := Routes("", &fakeTaskManager{}, repo, store.DisabledArtifactStore{Reason: "test"}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions/1/validate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"valid":false`) || !strings.Contains(rec.Body.String(), `asset-cert kind`) || !strings.Contains(rec.Body.String(), `proto_file`) {
		t.Fatalf("unexpected validation response: %d %s", rec.Code, rec.Body.String())
	}
	if len(pm.validateConfigCalls) != 0 {
		t.Fatalf("did not expect validate_config call, got %#v", pm.validateConfigCalls)
	}
}

func TestPluginSecretCreateStoresCiphertextAndReturnsMasked(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
			},
		},
	}
	repo := &fakePluginConfigRepository{secretValues: map[string]pluginmgr.SecretValueRecord{}}
	handler := Routes("", &fakeTaskManager{}, repo, &fakeArtifactStore{}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/plugin-secrets", strings.NewReader(`{
		"id":"sec-auth",
		"plugin_id":"@pulseops/grpc-source",
		"scope":"grpc-prod",
		"title":"Authorization",
		"value":"Bearer very-sensitive-token"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected secret create status: %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"masked":"********"`) || strings.Contains(body, "very-sensitive-token") {
		t.Fatalf("secret response leaked plaintext or missed masked value: %s", body)
	}
	saved, ok := repo.secretValues["sec-auth"]
	if !ok {
		t.Fatal("expected saved secret value")
	}
	if saved.Ciphertext == "" || saved.Ciphertext == "Bearer very-sensitive-token" {
		t.Fatalf("expected encrypted ciphertext, got %#v", saved)
	}
	if saved.EncryptionMeta["alg"] != "aes-256-gcm-local-v1" {
		t.Fatalf("unexpected encryption metadata: %#v", saved.EncryptionMeta)
	}
	assertConfigEvent(t, repo.configEvents, "secret", "upsert", "success")
}

func TestPluginAssetCreateUploadValidateAndActivate(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	capabilityID := pluginmgr.CapabilityID(pluginID, pluginmgr.CapabilityDataSource, "grpc")
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
			},
		},
		capabilities: []pluginmgr.Capability{{
			ID:       capabilityID,
			Type:     pluginmgr.CapabilityDataSource,
			Name:     "grpc",
			PluginID: pluginID,
		}},
	}
	repo := &fakePluginConfigRepository{
		configInstances: []pluginmgr.ConfigInstanceRecord{{
			ID:           "cfg-grpc-prod",
			PluginID:     pluginID,
			CapabilityID: capabilityID,
			Scope:        "capability",
			Status:       "active",
		}},
		assetVersions: map[string][]pluginmgr.AssetVersionRecord{},
	}
	handler := Routes("", &fakeTaskManager{}, repo, store.DisabledArtifactStore{Reason: "test"}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/plugin-assets", strings.NewReader(`{
		"id":"asset-inventory-proto",
		"plugin_id":"@pulseops/grpc-source",
		"capability_id":"@pulseops/grpc-source:data_source:grpc",
		"scope":"capability_shared",
		"kind":"proto_files",
		"title":"Inventory proto"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"asset-inventory-proto"`) {
		t.Fatalf("unexpected asset create response: %d %s", rec.Code, rec.Body.String())
	}
	if len(repo.assets) != 1 || repo.assets[0].Scope != pluginmgr.AssetScopeCapabilityShared {
		t.Fatalf("expected capability-scoped asset, got %#v", repo.assets)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-assets", strings.NewReader(`{
		"id":"asset-private-client-cert",
		"plugin_id":"@pulseops/grpc-source",
		"config_instance_id":"cfg-grpc-prod",
		"scope":"config_instance",
		"kind":"certificate",
		"title":"Private client cert"
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"config_instance_id":"cfg-grpc-prod"`) {
		t.Fatalf("unexpected private asset create response: %d %s", rec.Code, rec.Body.String())
	}
	if len(repo.assets) != 2 || repo.assets[1].CapabilityID != capabilityID || repo.assets[1].Scope != pluginmgr.AssetScopeConfigInstance {
		t.Fatalf("expected config-instance-scoped asset, got %#v", repo.assets)
	}

	body, contentType := multipartBody(t, "file", "inventory.proto", "text/plain", `syntax = "proto3";`)
	req = httptest.NewRequest(http.MethodPost, "/api/plugin-assets/asset-inventory-proto/versions", body)
	req.Header.Set("Content-Type", contentType)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"version":1`) || !strings.Contains(rec.Body.String(), `"inventory.proto"`) {
		t.Fatalf("unexpected asset version upload response: %d %s", rec.Code, rec.Body.String())
	}
	if len(repo.assetVersions["asset-inventory-proto"]) != 1 {
		t.Fatalf("expected uploaded asset version, got %#v", repo.assetVersions)
	}
	uploaded := repo.assetVersions["asset-inventory-proto"][0]
	if !strings.HasPrefix(uploaded.StorageURI, "db://") || string(uploaded.Content) != `syntax = "proto3";` {
		t.Fatalf("expected db-backed asset version, got %#v", uploaded)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-assets/asset-inventory-proto/versions/1/validate", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"validated"`) {
		t.Fatalf("unexpected asset validate response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/plugin-assets/asset-inventory-proto/versions/1/activate", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"active_version":1`) || !strings.Contains(rec.Body.String(), `"status":"active"`) {
		t.Fatalf("unexpected asset activate response: %d %s", rec.Code, rec.Body.String())
	}
	assertConfigEvent(t, repo.configEvents, "asset", "create", "success")
	assertConfigEvent(t, repo.configEvents, "asset_version", "create", "success")
	assertConfigEvent(t, repo.configEvents, "asset_version", "validate", "success")
	assertConfigEvent(t, repo.configEvents, "asset_version", "activate", "success")
}

func assertConfigEvent(t *testing.T, events []pluginmgr.ConfigEventRecord, resourceType, action, status string) {
	t.Helper()
	for _, event := range events {
		if event.ResourceType == resourceType && event.Action == action && event.Status == status {
			return
		}
	}
	t.Fatalf("missing config event type=%s action=%s status=%s in %#v", resourceType, action, status, events)
}

func TestPluginConfigValidateResolvesAssetRefs(t *testing.T) {
	t.Parallel()

	pluginID := "@pulseops/grpc-source"
	capabilityID := pluginmgr.CapabilityID(pluginID, pluginmgr.CapabilityDataSource, "grpc")
	ciphertext, encryptionMeta, err := pluginconfig.EncryptSecret("Bearer api-token")
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	pm := &fakePluginManager{
		plugin: pluginmgr.PluginView{
			Package: pluginmgr.PackageRecord{ID: pluginID, Name: "gRPC Source"},
			Release: &pluginmgr.ReleaseRecord{
				PluginID: pluginID,
				Version:  "1.0.0",
			},
		},
		capabilities: []pluginmgr.Capability{{
			ID:       capabilityID,
			Type:     pluginmgr.CapabilityDataSource,
			Name:     "grpc",
			PluginID: pluginID,
			Config: &pluginmgr.ConfigSchema{
				ValidateAction: "validate_config",
				Fields: map[string]pluginmgr.ConfigField{
					"service":     {Type: "string", Required: true},
					"method":      {Type: "string", Required: true},
					"request":     {Type: "object", Class: "JSONObject", Required: true},
					"proto_files": {Type: "array", Items: &pluginmgr.ConfigField{Type: "file", AssetKind: "proto_file", AssetScope: pluginmgr.AssetScopeCapabilityShared}},
					"authorization": {
						Type: "secret",
					},
				},
			},
		}},
		validateConfigCheck: func(req pluginmgr.ConfigValidationRequest) error {
			items, ok := req.Config["proto_files"].([]any)
			if !ok || len(items) != 1 {
				t.Fatalf("expected resolved proto_files list, got %#v", req.Config["proto_files"])
			}
			path, ok := items[0].(string)
			if !ok || path == "" {
				t.Fatalf("expected resolved proto file path, got %#v", items[0])
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read resolved proto file: %v", err)
			}
			if !strings.Contains(string(raw), "service Health") {
				t.Fatalf("unexpected resolved proto content: %s", string(raw))
			}
			if req.Config["authorization"] != "Bearer api-token" {
				t.Fatalf("expected decrypted secret, got %#v", req.Config["authorization"])
			}
			return nil
		},
	}
	now := time.Now().UTC()
	repo := &fakePluginConfigRepository{
		configInstances: []pluginmgr.ConfigInstanceRecord{{
			ID:             "cfg-grpc-prod",
			PluginID:       pluginID,
			CapabilityID:   capabilityID,
			CapabilityType: pluginmgr.CapabilityDataSource,
			CapabilityName: "grpc",
			Scope:          "capability",
			Status:         "draft",
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
		configVersions: map[string][]pluginmgr.ConfigVersionRecord{
			"cfg-grpc-prod": {{
				InstanceID: "cfg-grpc-prod",
				Version:    1,
				Status:     "draft",
				Values: map[string]any{
					"service":       "grpc.health.v1.Health",
					"method":        "Check",
					"request":       map[string]any{"service": ""},
					"proto_files":   []any{"asset-health-proto"},
					"authorization": "sec-auth",
				},
				CreatedAt: now,
				UpdatedAt: now,
			}},
		},
		assets: []pluginmgr.AssetRecord{{
			ID:            "asset-health-proto",
			PluginID:      pluginID,
			CapabilityID:  capabilityID,
			Scope:         pluginmgr.AssetScopeCapabilityShared,
			Kind:          "proto_file",
			Title:         "Health proto",
			Status:        "active",
			ActiveVersion: 1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
		assetVersions: map[string][]pluginmgr.AssetVersionRecord{
			"asset-health-proto": {{
				AssetID:    "asset-health-proto",
				Version:    1,
				Status:     "active",
				Filename:   "health.proto",
				StorageURI: "db://plugin-assets/pulseops/grpc/health.proto",
				Content:    []byte(`syntax = "proto3"; package grpc.health.v1; service Health { rpc Check(HealthCheckRequest) returns (HealthCheckResponse); } message HealthCheckRequest { string service = 1; } message HealthCheckResponse { string status = 1; }`),
				SizeBytes:  128,
				Checksum:   "sha256:abc",
				CreatedAt:  now,
				UpdatedAt:  now,
			}},
		},
		secrets: []pluginmgr.SecretRecord{{
			ID:        "sec-auth",
			PluginID:  pluginID,
			Status:    "active",
			Masked:    "********",
			CreatedAt: now,
			UpdatedAt: now,
		}},
		secretValues: map[string]pluginmgr.SecretValueRecord{
			"sec-auth": {
				SecretID:       "sec-auth",
				Ciphertext:     ciphertext,
				EncryptionMeta: encryptionMeta,
				UpdatedAt:      now,
			},
		},
	}
	handler := Routes("", &fakeTaskManager{}, repo, store.DisabledArtifactStore{Reason: "test"}, nil, pm, testPlatform(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/plugin-configs/cfg-grpc-prod/versions/1/validate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("unexpected validation response: %d %s", rec.Code, rec.Body.String())
	}
	if len(pm.validateConfigCalls) != 1 {
		t.Fatalf("expected one validate_config call, got %#v", pm.validateConfigCalls)
	}
	assetRefs, ok := pm.validateConfigCalls[0].Input["assets"].([]map[string]any)
	if !ok || len(assetRefs) != 1 {
		t.Fatalf("expected validate_config asset refs, got %#v", pm.validateConfigCalls[0].Input["assets"])
	}
	if assetRefs[0]["field"] != "proto_files[0]" || assetRefs[0]["asset_id"] != "asset-health-proto" || assetRefs[0]["version"] != 1 {
		t.Fatalf("unexpected validate_config asset ref: %#v", assetRefs[0])
	}
}

func testPlatform() config.PlatformConfigSummary {
	return config.PlatformConfigSummary{Mode: "active", Applied: true}
}

type fakeTaskManager struct{}

type emptyTaskManager struct {
	fakeTaskManager
}

func (m *emptyTaskManager) ListTasks() []store.TaskState {
	return nil
}

func (m *fakeTaskManager) ListTasks() []store.TaskState {
	now := time.Now()
	return []store.TaskState{{
		TaskID:          "task-a",
		Name:            "Task A",
		Kind:            "http_check",
		Enabled:         true,
		Status:          "running",
		Labels:          map[string]string{"env": "test"},
		LastRunAt:       &now,
		LastRunStatus:   "success",
		LastCheckStatus: "pass",
		UpdatedAt:       now,
	}}
}
func (m *fakeTaskManager) GetTask(string) (store.TaskState, bool) {
	return store.TaskState{}, false
}
func (m *fakeTaskManager) RunTask(context.Context, string, task.TriggerType) (store.RunRecord, error) {
	return store.RunRecord{}, nil
}
func (m *fakeTaskManager) ReloadTask(context.Context, string) error           { return nil }
func (m *fakeTaskManager) SetTaskEnabled(context.Context, string, bool) error { return nil }
func (m *fakeTaskManager) UpsertTaskFromDB(context.Context, config.TaskDefinition) (store.TaskState, error) {
	return store.TaskState{}, nil
}
func (m *fakeTaskManager) ValidateTaskDefinition(def config.TaskDefinition) (config.TaskSpec, error) {
	spec, err := def.ToTaskSpec()
	if err != nil {
		return spec, err
	}
	var cfg config.Config
	cfg.Normalize()
	spec.Normalize(cfg)
	return spec, spec.ValidateBasic()
}
func (m *fakeTaskManager) TestRunTaskDefinition(context.Context, config.TaskDefinition) (store.RunRecord, error) {
	return store.RunRecord{RunID: "dry-run-1", TriggerType: "dry_run", RunStatus: "success", CheckStatus: "pass"}, nil
}
func (m *fakeTaskManager) RemoveTaskByID(context.Context, string) error { return nil }

type fakeRepository struct {
	artifactsByRun map[string][]store.ArtifactRef
	artifactsByID  map[string]store.ArtifactRef
	runs           map[string]store.RunRecord
	runItems       []store.RunListItem
	defs           []config.TaskDefinition
	dependencies   []config.TaskDependency
}

func (r *fakeRepository) Close() error                                           { return nil }
func (r *fakeRepository) UpsertTaskState(context.Context, store.TaskState) error { return nil }
func (r *fakeRepository) DeleteTaskState(context.Context, string) error          { return nil }
func (r *fakeRepository) InsertRun(context.Context, store.RunRecord) error       { return nil }
func (r *fakeRepository) ListRuns(context.Context, string, int, int, time.Duration) ([]store.RunRecord, error) {
	return nil, nil
}
func (r *fakeRepository) CountRuns(context.Context, string, time.Duration) (int, error) {
	return 0, nil
}
func (r *fakeRepository) ListRunItems(context.Context, string, int, int, time.Duration) ([]store.RunListItem, error) {
	return r.runItems, nil
}
func (r *fakeRepository) ListRunsAcrossTasks(context.Context, store.RunQuery) ([]store.RunListItem, int, error) {
	return r.runItems, len(r.runItems), nil
}
func (r *fakeRepository) ListConsecutiveFailures(context.Context, []string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (r *fakeRepository) ListRunStats(context.Context, string, time.Duration) ([]store.RunStat, error) {
	return nil, nil
}
func (r *fakeRepository) GetRun(_ context.Context, taskID, runID string) (store.RunRecord, error) {
	record, ok := r.runs[taskID+"/"+runID]
	if !ok {
		return store.RunRecord{}, sql.ErrNoRows
	}
	return record, nil
}
func (r *fakeRepository) ListArtifactsByRun(_ context.Context, taskID, runID string) ([]store.ArtifactRef, error) {
	return r.artifactsByRun[taskID+"/"+runID], nil
}
func (r *fakeRepository) GetArtifact(_ context.Context, artifactID string) (store.ArtifactRef, error) {
	artifact, ok := r.artifactsByID[artifactID]
	if !ok {
		return store.ArtifactRef{}, sql.ErrNoRows
	}
	return artifact, nil
}
func (r *fakeRepository) InsertReloadFailure(context.Context, string, string, string) error {
	return nil
}
func (r *fakeRepository) InsertAIAnalysis(context.Context, store.AIAnalysisRecord) error { return nil }
func (r *fakeRepository) GetAIAnalysis(context.Context, string) (*store.AIAnalysisRecord, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) ListAIAnalyses(context.Context, string, int) ([]store.AIAnalysisRecord, error) {
	return nil, nil
}
func (r *fakeRepository) ListTaskDefinitions(context.Context) ([]config.TaskDefinition, error) {
	return r.defs, nil
}
func (r *fakeRepository) GetTaskDefinition(context.Context, string) (*config.TaskDefinition, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) InsertTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *fakeRepository) UpdateTaskDefinition(context.Context, config.TaskDefinition) error {
	return nil
}
func (r *fakeRepository) DeleteTaskDefinition(context.Context, string) error       { return nil }
func (r *fakeRepository) ListPipelines(context.Context) ([]config.Pipeline, error) { return nil, nil }
func (r *fakeRepository) GetPipeline(context.Context, string) (*config.Pipeline, error) {
	return nil, sql.ErrNoRows
}
func (r *fakeRepository) InsertPipeline(context.Context, config.Pipeline) error { return nil }
func (r *fakeRepository) UpdatePipeline(context.Context, config.Pipeline) error { return nil }
func (r *fakeRepository) DeletePipeline(context.Context, string) error          { return nil }
func (r *fakeRepository) ListTaskDefinitionsByPipeline(context.Context, string) ([]config.TaskDefinition, error) {
	return r.defs, nil
}
func (r *fakeRepository) UpdateTaskPipeline(context.Context, string, *string) error { return nil }
func (r *fakeRepository) ListTaskDependencies(context.Context) ([]config.TaskDependency, error) {
	return r.dependencies, nil
}
func (r *fakeRepository) ListTaskDependenciesByPipeline(context.Context, string) ([]config.TaskDependency, error) {
	return r.dependencies, nil
}
func (r *fakeRepository) ReplaceTaskDependencies(context.Context, string, []config.TaskDependency) error {
	return nil
}
func (r *fakeRepository) UpsertTaskDependency(_ context.Context, dep config.TaskDependency) (config.TaskDependency, error) {
	return dep, nil
}
func (r *fakeRepository) DeleteTaskDependency(context.Context, string) error { return nil }
func (r *fakeRepository) GetMeta(context.Context, string) (string, error) {
	return "", store.ErrMetaNotFound
}
func (r *fakeRepository) SetMeta(context.Context, string, string) error { return nil }
func (r *fakeRepository) LoadGlobalSettings(context.Context) (config.GlobalSettings, error) {
	return config.GlobalSettings{MaxPayloadBytes: 4096}, nil
}
func (r *fakeRepository) SaveGlobalSettings(context.Context, config.GlobalSettings) error { return nil }
func (r *fakeRepository) LoadPlatformConfig(context.Context) (config.PlatformConfigSummary, error) {
	return config.PlatformConfigSummary{}, store.ErrMetaNotFound
}
func (r *fakeRepository) SavePlatformConfig(context.Context, config.PlatformConfigSummary) error {
	return nil
}

type fakePluginConfigRepository struct {
	fakeRepository
	configInstances []pluginmgr.ConfigInstanceRecord
	configVersions  map[string][]pluginmgr.ConfigVersionRecord
	assets          []pluginmgr.AssetRecord
	assetVersions   map[string][]pluginmgr.AssetVersionRecord
	secrets         []pluginmgr.SecretRecord
	secretValues    map[string]pluginmgr.SecretValueRecord
	configEvents    []pluginmgr.ConfigEventRecord
}

func (r *fakePluginConfigRepository) UpsertPluginConfigInstance(_ context.Context, record pluginmgr.ConfigInstanceRecord) error {
	for i := range r.configInstances {
		if r.configInstances[i].ID == record.ID {
			r.configInstances[i] = record
			return nil
		}
	}
	r.configInstances = append(r.configInstances, record)
	return nil
}

func (r *fakePluginConfigRepository) UpdatePluginConfigInstanceStatus(_ context.Context, instanceID, status string) error {
	for i := range r.configInstances {
		if r.configInstances[i].ID == instanceID {
			r.configInstances[i].Status = status
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *fakePluginConfigRepository) UpsertPluginConfigVersion(_ context.Context, record pluginmgr.ConfigVersionRecord) error {
	if r.configVersions == nil {
		r.configVersions = map[string][]pluginmgr.ConfigVersionRecord{}
	}
	for i := range r.configVersions[record.InstanceID] {
		if r.configVersions[record.InstanceID][i].Version == record.Version {
			r.configVersions[record.InstanceID][i] = record
			return nil
		}
	}
	r.configVersions[record.InstanceID] = append(r.configVersions[record.InstanceID], record)
	return nil
}

func (r *fakePluginConfigRepository) InsertPluginConfigEvent(_ context.Context, record pluginmgr.ConfigEventRecord) error {
	r.configEvents = append(r.configEvents, record)
	return nil
}

func (r *fakePluginConfigRepository) ListPluginConfigEvents(_ context.Context, pluginID, resourceType, resourceID string, limit int) ([]pluginmgr.ConfigEventRecord, error) {
	var out []pluginmgr.ConfigEventRecord
	for _, record := range r.configEvents {
		if pluginID != "" && record.PluginID != pluginID {
			continue
		}
		if resourceType != "" && record.ResourceType != resourceType {
			continue
		}
		if resourceID != "" && record.ResourceID != resourceID {
			continue
		}
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *fakePluginConfigRepository) ActivatePluginConfigVersion(_ context.Context, instanceID string, version int) error {
	found := false
	for i := range r.configVersions[instanceID] {
		record := &r.configVersions[instanceID][i]
		if record.Version == version {
			record.Status = "active"
			record.ActivatedAt = ptrTime(time.Now().UTC())
			found = true
			continue
		}
		if record.Status == "active" {
			record.Status = "retired"
			record.RetiredAt = ptrTime(time.Now().UTC())
		}
	}
	if !found {
		return sql.ErrNoRows
	}
	for i := range r.configInstances {
		if r.configInstances[i].ID == instanceID {
			r.configInstances[i].Status = "active"
			r.configInstances[i].ActiveVersion = version
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *fakePluginConfigRepository) ListPluginConfigInstances(_ context.Context, pluginID, capabilityID string) ([]pluginmgr.ConfigInstanceRecord, error) {
	var out []pluginmgr.ConfigInstanceRecord
	for _, record := range r.configInstances {
		if pluginID != "" && record.PluginID != pluginID {
			continue
		}
		if capabilityID != "" && record.CapabilityID != capabilityID {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *fakePluginConfigRepository) GetPluginConfigInstance(_ context.Context, instanceID string) (pluginmgr.ConfigInstanceRecord, error) {
	for _, record := range r.configInstances {
		if record.ID == instanceID {
			return record, nil
		}
	}
	return pluginmgr.ConfigInstanceRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) ListPluginConfigVersions(_ context.Context, instanceID string) ([]pluginmgr.ConfigVersionRecord, error) {
	return r.configVersions[instanceID], nil
}

func (r *fakePluginConfigRepository) GetPluginConfigVersion(_ context.Context, instanceID string, version int) (pluginmgr.ConfigVersionRecord, error) {
	for _, record := range r.configVersions[instanceID] {
		if record.Version == version {
			return record, nil
		}
	}
	return pluginmgr.ConfigVersionRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) GetActivePluginConfigVersion(_ context.Context, instanceID string) (pluginmgr.ConfigVersionRecord, error) {
	for _, record := range r.configVersions[instanceID] {
		if record.Status == "active" {
			return record, nil
		}
	}
	return pluginmgr.ConfigVersionRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) UpsertPluginAsset(_ context.Context, record pluginmgr.AssetRecord) error {
	for i := range r.assets {
		if r.assets[i].ID == record.ID {
			r.assets[i] = record
			return nil
		}
	}
	r.assets = append(r.assets, record)
	return nil
}

func (r *fakePluginConfigRepository) UpsertPluginAssetVersion(_ context.Context, record pluginmgr.AssetVersionRecord) error {
	if r.assetVersions == nil {
		r.assetVersions = map[string][]pluginmgr.AssetVersionRecord{}
	}
	for i := range r.assetVersions[record.AssetID] {
		if r.assetVersions[record.AssetID][i].Version == record.Version {
			r.assetVersions[record.AssetID][i] = record
			return nil
		}
	}
	r.assetVersions[record.AssetID] = append(r.assetVersions[record.AssetID], record)
	return nil
}

func (r *fakePluginConfigRepository) ActivatePluginAssetVersion(_ context.Context, assetID string, version int) error {
	found := false
	for i := range r.assetVersions[assetID] {
		record := &r.assetVersions[assetID][i]
		if record.Version == version {
			record.Status = "active"
			record.ActivatedAt = ptrTime(time.Now().UTC())
			found = true
			continue
		}
		if record.Status == "active" {
			record.Status = "retired"
			record.RetiredAt = ptrTime(time.Now().UTC())
		}
	}
	if !found {
		return sql.ErrNoRows
	}
	for i := range r.assets {
		if r.assets[i].ID == assetID {
			r.assets[i].Status = "active"
			r.assets[i].ActiveVersion = version
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *fakePluginConfigRepository) ListPluginAssets(_ context.Context, pluginID, capabilityID, configInstanceID, scope, kind string) ([]pluginmgr.AssetRecord, error) {
	var out []pluginmgr.AssetRecord
	for _, record := range r.assets {
		if pluginID != "" && record.PluginID != pluginID {
			continue
		}
		if capabilityID != "" && record.CapabilityID != capabilityID {
			continue
		}
		if configInstanceID != "" && record.ConfigInstanceID != configInstanceID {
			continue
		}
		if scope != "" && record.Scope != scope {
			continue
		}
		if kind != "" && record.Kind != kind {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *fakePluginConfigRepository) GetPluginAsset(_ context.Context, assetID string) (pluginmgr.AssetRecord, error) {
	for _, record := range r.assets {
		if record.ID == assetID {
			return record, nil
		}
	}
	return pluginmgr.AssetRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) ListPluginAssetVersions(_ context.Context, assetID string) ([]pluginmgr.AssetVersionRecord, error) {
	return r.assetVersions[assetID], nil
}

func (r *fakePluginConfigRepository) GetPluginAssetVersion(_ context.Context, assetID string, version int) (pluginmgr.AssetVersionRecord, error) {
	for _, record := range r.assetVersions[assetID] {
		if record.Version == version {
			return record, nil
		}
	}
	return pluginmgr.AssetVersionRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) GetActivePluginAssetVersion(_ context.Context, assetID string) (pluginmgr.AssetVersionRecord, error) {
	for _, record := range r.assetVersions[assetID] {
		if record.Status == "active" {
			return record, nil
		}
	}
	return pluginmgr.AssetVersionRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) UpsertPluginSecret(_ context.Context, secret pluginmgr.SecretRecord, value pluginmgr.SecretValueRecord) error {
	for i := range r.secrets {
		if r.secrets[i].ID == secret.ID {
			r.secrets[i] = secret
			if r.secretValues == nil {
				r.secretValues = map[string]pluginmgr.SecretValueRecord{}
			}
			r.secretValues[secret.ID] = value
			return nil
		}
	}
	r.secrets = append(r.secrets, secret)
	if r.secretValues == nil {
		r.secretValues = map[string]pluginmgr.SecretValueRecord{}
	}
	r.secretValues[secret.ID] = value
	return nil
}

func (r *fakePluginConfigRepository) ListPluginSecrets(_ context.Context, pluginID, scope string) ([]pluginmgr.SecretRecord, error) {
	var out []pluginmgr.SecretRecord
	for _, record := range r.secrets {
		if pluginID != "" && record.PluginID != pluginID {
			continue
		}
		if scope != "" && record.Scope != scope {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *fakePluginConfigRepository) GetPluginSecret(_ context.Context, secretID string) (pluginmgr.SecretRecord, error) {
	for _, record := range r.secrets {
		if record.ID == secretID {
			return record, nil
		}
	}
	return pluginmgr.SecretRecord{}, sql.ErrNoRows
}

func (r *fakePluginConfigRepository) GetPluginSecretValue(_ context.Context, secretID string) (pluginmgr.SecretValueRecord, error) {
	if record, ok := r.secretValues[secretID]; ok {
		return record, nil
	}
	return pluginmgr.SecretValueRecord{}, sql.ErrNoRows
}

type fakePluginManager struct {
	plugin              pluginmgr.PluginView
	capabilities        []pluginmgr.Capability
	validateConfigCalls []pluginmgr.ConfigValidationRequest
	validateConfigCheck func(pluginmgr.ConfigValidationRequest) error
	validateConfigErr   error
}

func (m *fakePluginManager) Catalog(context.Context) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{Plugins: []pluginmgr.PluginView{m.plugin}}, nil
}

func (m *fakePluginManager) Plugin(_ context.Context, pluginID string) (pluginmgr.PluginView, error) {
	if m.plugin.Package.ID == pluginID {
		return m.plugin, nil
	}
	return pluginmgr.PluginView{}, sql.ErrNoRows
}

func (m *fakePluginManager) Releases(context.Context, string) ([]pluginmgr.ReleaseRecord, error) {
	return nil, nil
}

func (m *fakePluginManager) Capabilities(context.Context, string, string) ([]pluginmgr.Capability, error) {
	return m.capabilities, nil
}

func (m *fakePluginManager) Reload(context.Context) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) ValidateRelease(context.Context, string, string) (pluginmgr.ReleaseRecord, error) {
	return pluginmgr.ReleaseRecord{}, nil
}

func (m *fakePluginManager) ActivateRelease(context.Context, string, string) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) DisablePlugin(context.Context, string) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) EnablePlugin(context.Context, string) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) RollbackPlugin(context.Context, string) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) GC(context.Context) (pluginmgr.Catalog, error) {
	return pluginmgr.Catalog{}, nil
}

func (m *fakePluginManager) ValidateConfig(_ context.Context, req pluginmgr.ConfigValidationRequest) error {
	m.validateConfigCalls = append(m.validateConfigCalls, req)
	if m.validateConfigCheck != nil {
		return m.validateConfigCheck(req)
	}
	return m.validateConfigErr
}

func (m *fakePluginManager) ExportRelease(context.Context, string, string) (string, []byte, error) {
	return "", nil, nil
}

func (m *fakePluginManager) ImportRelease(context.Context, io.Reader) (pluginmgr.ReleaseRecord, error) {
	return pluginmgr.ReleaseRecord{}, nil
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func multipartBody(t *testing.T, fieldName, filename, contentType, content string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + fieldName + `"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

type fakeArtifactStore struct {
	bodies     map[string]string
	lastPutKey string
	lastPut    string
}

func (s *fakeArtifactStore) Kind() string { return "s3" }
func (s *fakeArtifactStore) Put(_ context.Context, key string, body io.Reader, meta store.ArtifactMeta) (store.ArtifactRef, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return store.ArtifactRef{}, err
	}
	s.lastPutKey = key
	s.lastPut = string(raw)
	contentType := meta.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return store.ArtifactRef{
		ArtifactID:  "artifact-upload",
		Kind:        meta.Kind,
		StorageKind: "s3",
		URI:         "s3://pulseops-artifacts/" + key,
		ContentType: contentType,
		SizeBytes:   int64(len(raw)),
		SHA256:      "abc123",
	}, nil
}
func (s *fakeArtifactStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if s.bodies != nil {
		return io.NopCloser(strings.NewReader(s.bodies[key])), nil
	}
	return io.NopCloser(strings.NewReader("")), nil
}
func (s *fakeArtifactStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://download.local/object", nil
}
func (s *fakeArtifactStore) Delete(context.Context, string) error { return nil }
