package datasource

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

func TestPluginSourceHTTP(t *testing.T) {
	t.Parallel()

	var received pluginEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pluginResponse{
			OK:   true,
			Data: map[string]any{"value": float64(42)},
		})
	}))
	defer server.Close()

	source := NewPluginSource(pluginmodel.Capability{
		PluginID: "@test/http",
		Name:     "http_inventory",
		Runtime:  "http",
		Endpoint: server.URL,
	}, config.PluginsConfig{})

	data, err := source.Fetch(context.Background(), Spec{
		Type:   "http_inventory",
		Config: map[string]any{"region": "us"},
		Alias:  "inventory",
	}, FetchDeps{CurrentRunID: "run-1", CurrentTaskID: "task-1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	result := data.(map[string]any)
	if result["value"] != float64(42) {
		t.Fatalf("unexpected data: %#v", data)
	}
	if received.Protocol != Protocol || received.PluginID != "@test/http" || received.Capability != "http_inventory" {
		t.Fatalf("unexpected envelope: %#v", received)
	}
	if received.Context["task_id"] != "task-1" || received.Context["run_id"] != "run-1" {
		t.Fatalf("missing context in envelope: %#v", received.Context)
	}
}

func TestPluginSourceProcess(t *testing.T) {
	t.Parallel()

	releaseDir := t.TempDir()
	entrypoint := filepath.Join(releaseDir, "source.sh")
	script := "#!/bin/sh\ninput=$(cat)\ncase \"$input\" in *'\"capability\":\"proc_source\"'*) echo '{\"ok\":true,\"data\":{\"value\":\"process\"}}' ;; *) echo '{\"ok\":false,\"error\":{\"message\":\"bad envelope\"}}' ;; esac\n"
	if err := os.WriteFile(entrypoint, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	source := NewPluginSource(pluginmodel.Capability{
		PluginID:    "@test/process",
		Name:        "proc_source",
		Runtime:     "process",
		Entrypoint:  "source.sh",
		ReleasePath: releaseDir,
	}, config.PluginsConfig{})

	data, err := source.Fetch(context.Background(), Spec{Type: "proc_source"}, FetchDeps{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	result := data.(map[string]any)
	if result["value"] != "process" {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func TestPluginSourceGRPCReflection(t *testing.T) {
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

	cfg := config.PluginsConfig{}
	cfg.DefaultTimeout.Duration = 3 * time.Second
	source := NewPluginSource(pluginmodel.Capability{
		PluginID: "@pulseops/grpc-source",
		Name:     "grpc",
		Protocol: "grpc",
		Runtime:  "builtin",
		Schema: pluginmodel.Schema{
			"endpoint": {Type: "string", Required: true},
			"service":  {Type: "string", Required: true},
			"method":   {Type: "string", Required: true},
			"request":  {Type: "object", Required: true},
		},
	}, cfg)

	data, err := source.Fetch(context.Background(), Spec{Type: "grpc", Config: map[string]any{
		"endpoint": listener.Addr().String(),
		"service":  "grpc.health.v1.Health",
		"method":   "Check",
		"request":  map[string]any{"service": ""},
	}}, FetchDeps{})
	if err != nil {
		t.Fatalf("fetch grpc: %v", err)
	}
	result := data.(map[string]any)
	if result["status"] != "SERVING" {
		t.Fatalf("unexpected grpc response: %#v", result)
	}
}

func TestPluginSourceGRPCProtoFiles(t *testing.T) {
	t.Parallel()

	releaseDir := t.TempDir()
	protoFile := `syntax = "proto3";
package grpc.health.v1;
service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
}
message HealthCheckRequest {
  string service = 1;
}
message HealthCheckResponse {
  enum ServingStatus {
    UNKNOWN = 0;
    SERVING = 1;
    NOT_SERVING = 2;
    SERVICE_UNKNOWN = 3;
  }
  ServingStatus status = 1;
}
`
	if err := os.WriteFile(filepath.Join(releaseDir, "health.proto"), []byte(protoFile), 0644); err != nil {
		t.Fatalf("write proto: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	cfg := config.PluginsConfig{}
	cfg.DefaultTimeout.Duration = 3 * time.Second
	source := NewPluginSource(pluginmodel.Capability{
		PluginID:    "@pulseops/grpc-source",
		Name:        "grpc",
		Protocol:    "grpc",
		Runtime:     "builtin",
		ReleasePath: releaseDir,
		Schema: pluginmodel.Schema{
			"endpoint": {Type: "string", Required: true},
			"service":  {Type: "string", Required: true},
			"method":   {Type: "string", Required: true},
			"request":  {Type: "object", Required: true},
		},
	}, cfg)

	data, err := source.Fetch(context.Background(), Spec{Type: "grpc", Config: map[string]any{
		"endpoint":       listener.Addr().String(),
		"service":        "grpc.health.v1.Health",
		"method":         "Check",
		"request":        map[string]any{"service": ""},
		"use_reflection": false,
		"proto_files":    []any{"health.proto"},
	}}, FetchDeps{})
	if err != nil {
		t.Fatalf("fetch grpc: %v", err)
	}
	result := data.(map[string]any)
	if result["status"] != "SERVING" {
		t.Fatalf("unexpected grpc response: %#v", result)
	}
}
