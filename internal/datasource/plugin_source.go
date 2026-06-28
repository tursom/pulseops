package datasource

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bufbuild/protocompile"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

const Protocol = "pulseops.plugin/v1"

type PluginSource struct {
	cap pluginmodel.Capability
	cfg config.PluginsConfig
}

type pluginEnvelope struct {
	Protocol   string         `json:"protocol"`
	CallID     string         `json:"call_id"`
	PluginID   string         `json:"plugin_id"`
	Capability string         `json:"capability"`
	Action     string         `json:"action"`
	TimeoutMS  int64          `json:"timeout_ms"`
	Context    map[string]any `json:"context,omitempty"`
	Config     map[string]any `json:"config,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type pluginResponse struct {
	OK      bool           `json:"ok"`
	Data    any            `json:"data"`
	Summary map[string]any `json:"summary,omitempty"`
	Error   *pluginError   `json:"error,omitempty"`
}

type pluginError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	Details   any    `json:"details,omitempty"`
}

func NewPluginSource(cap pluginmodel.Capability, cfg config.PluginsConfig) *PluginSource {
	return &PluginSource{cap: cap, cfg: cfg}
}

func (s *PluginSource) Name() string {
	return s.cap.Name
}

func (s *PluginSource) ValidateSpec(spec Spec) error {
	merged := mergeConfig(s.cap.Defaults, spec.Config)
	switch s.protocol() {
	case "grpc":
		if !s.cfg.GRPCAllowed() {
			return fmt.Errorf("grpc data source runtime is disabled")
		}
		if stringValue(merged, "endpoint") == "" {
			return fmt.Errorf("grpc data source %q requires config.endpoint", s.cap.Name)
		}
		if stringValue(merged, "service") == "" {
			return fmt.Errorf("grpc data source %q requires config.service", s.cap.Name)
		}
		if stringValue(merged, "method") == "" {
			return fmt.Errorf("grpc data source %q requires config.method", s.cap.Name)
		}
		if _, ok := merged["request"]; !ok {
			return fmt.Errorf("grpc data source %q requires config.request", s.cap.Name)
		}
	case "http", "http_plugin":
		if !s.cfg.HTTPAllowed() {
			return fmt.Errorf("http plugin data source runtime is disabled")
		}
		if s.cap.Endpoint == "" {
			return fmt.Errorf("http plugin data source %q requires an endpoint", s.cap.Name)
		}
	case "process":
		if !s.cfg.ProcessAllowed() {
			return fmt.Errorf("process data source runtime is disabled")
		}
		if s.cap.Entrypoint == "" {
			return fmt.Errorf("process data source %q requires an entrypoint", s.cap.Name)
		}
		if s.cap.ReleasePath == "" {
			return fmt.Errorf("process data source %q requires a release path", s.cap.Name)
		}
	}
	for name, field := range s.cap.Schema {
		if field.Required && merged[name] == nil {
			return fmt.Errorf("data source %q requires config.%s", s.cap.Name, name)
		}
	}
	return nil
}

func (s *PluginSource) Fetch(ctx context.Context, spec Spec, deps FetchDeps) (any, error) {
	if err := s.ValidateSpec(spec); err != nil {
		return nil, err
	}
	merged := mergeConfig(s.cap.Defaults, spec.Config)
	switch s.protocol() {
	case "grpc":
		return s.fetchGRPC(ctx, merged)
	case "http", "http_plugin":
		return s.fetchHTTP(ctx, merged, deps)
	case "process":
		return s.fetchProcess(ctx, merged, deps)
	default:
		return nil, fmt.Errorf("unsupported data source protocol/runtime %q for %q", s.protocol(), s.cap.Name)
	}
}

func (s *PluginSource) protocol() string {
	if s.cap.Protocol != "" {
		return s.cap.Protocol
	}
	return s.cap.Runtime
}

func (s *PluginSource) envelope(configMap map[string]any, deps FetchDeps) pluginEnvelope {
	timeout := sourceTimeout(s.cfg, configMap)
	return pluginEnvelope{
		Protocol:   Protocol,
		CallID:     uuid.NewString(),
		PluginID:   s.cap.PluginID,
		Capability: s.cap.Name,
		Action:     "fetch",
		TimeoutMS:  timeout.Milliseconds(),
		Context: map[string]any{
			"task_id":      deps.CurrentTaskID,
			"run_id":       deps.CurrentRunID,
			"trigger_type": deps.TriggerType,
		},
		Config: configMap,
		Input:  map[string]any{},
	}
}

func (s *PluginSource) fetchHTTP(ctx context.Context, configMap map[string]any, deps FetchDeps) (any, error) {
	timeout := sourceTimeout(s.cfg, configMap)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(s.envelope(configMap, deps))
	if err != nil {
		return nil, fmt.Errorf("marshal plugin request: %w", err)
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, s.cap.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build plugin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call http plugin data source %q: %w", s.cap.Name, err)
	}
	defer resp.Body.Close()

	raw, err := readLimited(resp.Body, maxOutputBytes(s.cfg))
	if err != nil {
		return nil, fmt.Errorf("read http plugin response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http plugin data source %q returned status %d: %s", s.cap.Name, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decodePluginResponse(s.cap.Name, raw)
}

func (s *PluginSource) fetchProcess(ctx context.Context, configMap map[string]any, deps FetchDeps) (any, error) {
	entrypoint, err := resolveEntrypoint(s.cap.ReleasePath, s.cap.Entrypoint)
	if err != nil {
		return nil, err
	}
	timeout := sourceTimeout(s.cfg, configMap)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(s.envelope(configMap, deps))
	if err != nil {
		return nil, fmt.Errorf("marshal plugin request: %w", err)
	}
	cmd := exec.CommandContext(callCtx, entrypoint)
	cmd.Dir = s.cap.ReleasePath
	cmd.Env = filteredEnv(s.cfg.EnvAllowlist)
	cmd.Stdin = bytes.NewReader(body)

	limit := maxOutputBytes(s.cfg)
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("process data source %q timed out after %s", s.cap.Name, timeout)
	}
	if stdout.Overflow() || stderr.Overflow() {
		return nil, fmt.Errorf("process data source %q output exceeded %d bytes", s.cap.Name, limit)
	}
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return nil, fmt.Errorf("process data source %q failed: %s", s.cap.Name, msg)
	}
	return decodePluginResponse(s.cap.Name, stdout.Bytes())
}

func (s *PluginSource) fetchGRPC(ctx context.Context, configMap map[string]any) (any, error) {
	timeout := sourceTimeout(s.cfg, configMap)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := stringValue(configMap, "endpoint")
	service := stringValue(configMap, "service")
	method := stringValue(configMap, "method")
	maxReceiveBytes := intValue(configMap, "max_receive_bytes", 1024*1024)

	conn, err := s.dialGRPC(callCtx, endpoint, maxReceiveBytes, configMap)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	files, err := s.grpcFiles(callCtx, conn, service, configMap)
	if err != nil {
		return nil, err
	}
	methodDesc, err := findMethod(files, service, method)
	if err != nil {
		return nil, err
	}

	reqMsg := dynamicpb.NewMessage(methodDesc.Input())
	requestRaw, err := json.Marshal(configMap["request"])
	if err != nil {
		return nil, fmt.Errorf("marshal grpc request json: %w", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(requestRaw, reqMsg); err != nil {
		return nil, fmt.Errorf("decode grpc request for %s/%s: %w", service, method, err)
	}

	outMsg := dynamicpb.NewMessage(methodDesc.Output())
	invokeCtx := callCtx
	if md := metadataFromConfig(configMap); len(md) > 0 {
		invokeCtx = metadata.NewOutgoingContext(invokeCtx, md)
	}
	if err := conn.Invoke(invokeCtx, "/"+service+"/"+method, reqMsg, outMsg, grpc.MaxCallRecvMsgSize(maxReceiveBytes)); err != nil {
		return nil, fmt.Errorf("grpc data source %q call %s/%s: %w", s.cap.Name, service, method, err)
	}
	responseJSON, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(outMsg)
	if err != nil {
		return nil, fmt.Errorf("encode grpc response: %w", err)
	}
	var result any
	if err := json.Unmarshal(responseJSON, &result); err != nil {
		return nil, fmt.Errorf("decode grpc response json: %w", err)
	}
	return result, nil
}

func (s *PluginSource) dialGRPC(ctx context.Context, endpoint string, maxReceiveBytes int, configMap map[string]any) (*grpc.ClientConn, error) {
	transport, err := s.grpcTransportCredentials(configMap)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.DialContext(
		ctx,
		endpoint,
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxReceiveBytes)),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("dial grpc endpoint %q: %w", endpoint, err)
	}
	return conn, nil
}

func (s *PluginSource) grpcTransportCredentials(configMap map[string]any) (credentials.TransportCredentials, error) {
	tlsCfg, ok := objectValue(configMap, "tls")
	if !ok {
		return insecure.NewCredentials(), nil
	}
	enabled, hasEnabled := boolFromMap(tlsCfg, "enabled", false)
	if hasEnabled && !enabled {
		return insecure.NewCredentials(), nil
	}
	conf := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: stringFromMap(tlsCfg, "server_name"),
	}
	if skip, _ := boolFromMap(tlsCfg, "insecure_skip_verify", false); skip {
		conf.InsecureSkipVerify = true
	}
	if caFile := stringFromMap(tlsCfg, "ca_file"); caFile != "" {
		path := resolveReleasePath(s.cap.ReleasePath, caFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read grpc ca_file %q: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("grpc ca_file %q contains no PEM certificates", caFile)
		}
		conf.RootCAs = pool
	}
	certFile := stringFromMap(tlsCfg, "cert_file")
	keyFile := stringFromMap(tlsCfg, "key_file")
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, fmt.Errorf("grpc tls cert_file and key_file must be configured together")
		}
		cert, err := tls.LoadX509KeyPair(resolveReleasePath(s.cap.ReleasePath, certFile), resolveReleasePath(s.cap.ReleasePath, keyFile))
		if err != nil {
			return nil, fmt.Errorf("load grpc client certificate: %w", err)
		}
		conf.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(conf), nil
}

func (s *PluginSource) grpcFiles(ctx context.Context, conn *grpc.ClientConn, service string, configMap map[string]any) (*protoregistry.Files, error) {
	if path := stringValue(configMap, "proto_descriptor_set"); path != "" {
		return filesFromDescriptorSet(resolveReleasePath(s.cap.ReleasePath, path))
	}
	if path := stringValue(configMap, "descriptor_set"); path != "" {
		return filesFromDescriptorSet(resolveReleasePath(s.cap.ReleasePath, path))
	}
	if protoFiles, ok := stringListValue(configMap["proto_files"]); ok && len(protoFiles) > 0 {
		return filesFromProtoFiles(ctx, s.cap.ReleasePath, protoFiles, configMap)
	}
	if useReflection, _ := boolValue(configMap, "use_reflection", true); !useReflection {
		return nil, fmt.Errorf("grpc data source requires proto_descriptor_set when use_reflection=false")
	}
	return filesFromReflection(ctx, conn, service)
}

func filesFromProtoFiles(ctx context.Context, releasePath string, protoFiles []string, configMap map[string]any) (*protoregistry.Files, error) {
	importPaths := []string{"."}
	if releasePath != "" {
		importPaths = []string{releasePath}
	}
	if extra, ok := stringListValue(configMap["proto_import_paths"]); ok {
		for _, path := range extra {
			importPaths = append(importPaths, resolveReleasePath(releasePath, path))
		}
	}
	files := make([]string, 0, len(protoFiles))
	for _, file := range protoFiles {
		if file == "" {
			continue
		}
		if filepath.IsAbs(file) {
			return nil, fmt.Errorf("grpc proto_files entries must be relative to plugin release path")
		}
		files = append(files, filepath.ToSlash(file))
	}
	compiler := protocompile.Compiler{
		Resolver: &protocompile.SourceResolver{ImportPaths: importPaths},
	}
	compiled, err := compiler.Compile(ctx, files...)
	if err != nil {
		return nil, fmt.Errorf("compile grpc proto_files: %w", err)
	}
	registry := &protoregistry.Files{}
	for _, file := range compiled {
		if err := registry.RegisterFile(file); err != nil {
			return nil, fmt.Errorf("register compiled proto file %s: %w", file.Path(), err)
		}
	}
	return registry, nil
}

func filesFromDescriptorSet(path string) (*protoregistry.Files, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read proto descriptor set %q: %w", path, err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("decode proto descriptor set %q: %w", path, err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("build proto descriptor registry: %w", err)
	}
	return files, nil
}

func filesFromReflection(ctx context.Context, conn *grpc.ClientConn, service string) (*protoregistry.Files, error) {
	client := reflectionv1alpha.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("open grpc reflection stream: %w", err)
	}
	if err := stream.Send(&reflectionv1alpha.ServerReflectionRequest{
		MessageRequest: &reflectionv1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: service,
		},
	}); err != nil {
		return nil, fmt.Errorf("request grpc reflection descriptors: %w", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive grpc reflection descriptors: %w", err)
	}
	fileResp, ok := resp.MessageResponse.(*reflectionv1alpha.ServerReflectionResponse_FileDescriptorResponse)
	if !ok {
		if errResp, ok := resp.MessageResponse.(*reflectionv1alpha.ServerReflectionResponse_ErrorResponse); ok {
			return nil, fmt.Errorf("grpc reflection error %d: %s", errResp.ErrorResponse.ErrorCode, errResp.ErrorResponse.ErrorMessage)
		}
		return nil, fmt.Errorf("grpc reflection returned unexpected response for %q", service)
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, raw := range fileResp.FileDescriptorResponse.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(raw, fd); err != nil {
			return nil, fmt.Errorf("decode reflected file descriptor: %w", err)
		}
		set.File = append(set.File, fd)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("build reflected descriptor registry: %w", err)
	}
	return files, nil
}

func findMethod(files *protoregistry.Files, serviceName, methodName string) (protoreflect.MethodDescriptor, error) {
	desc, err := files.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return nil, fmt.Errorf("find grpc service %q: %w", serviceName, err)
	}
	service, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("grpc descriptor %q is not a service", serviceName)
	}
	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		return nil, fmt.Errorf("find grpc method %q on service %q", methodName, serviceName)
	}
	return method, nil
}

func decodePluginResponse(name string, raw []byte) (any, error) {
	var resp pluginResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse plugin data source %q response: %w", name, err)
	}
	if !resp.OK {
		if resp.Error == nil {
			return nil, fmt.Errorf("plugin data source %q returned ok=false", name)
		}
		if resp.Error.Code != "" {
			return nil, fmt.Errorf("plugin data source %q error %s: %s", name, resp.Error.Code, resp.Error.Message)
		}
		return nil, fmt.Errorf("plugin data source %q error: %s", name, resp.Error.Message)
	}
	return resp.Data, nil
}

func mergeConfig(defaults, override map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(override))
	for key, value := range defaults {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func sourceTimeout(cfg config.PluginsConfig, configMap map[string]any) time.Duration {
	if timeout := stringValue(configMap, "timeout"); timeout != "" {
		if parsed, err := time.ParseDuration(timeout); err == nil {
			return parsed
		}
	}
	if cfg.DefaultTimeout.Duration > 0 {
		return cfg.DefaultTimeout.Duration
	}
	return 30 * time.Second
}

func maxOutputBytes(cfg config.PluginsConfig) int {
	if cfg.MaxOutputBytes > 0 {
		return cfg.MaxOutputBytes
	}
	return 1024 * 1024
}

func readLimited(reader io.Reader, limit int) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, fmt.Errorf("response exceeded %d bytes", limit)
	}
	return raw, nil
}

func resolveEntrypoint(releasePath, entrypoint string) (string, error) {
	if filepath.IsAbs(entrypoint) {
		return "", fmt.Errorf("process entrypoint must be relative to the plugin release path")
	}
	absRelease, err := filepath.Abs(releasePath)
	if err != nil {
		return "", fmt.Errorf("resolve release path: %w", err)
	}
	absEntry, err := filepath.Abs(filepath.Join(absRelease, entrypoint))
	if err != nil {
		return "", fmt.Errorf("resolve process entrypoint: %w", err)
	}
	rel, err := filepath.Rel(absRelease, absEntry)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("process entrypoint must stay inside the plugin release path")
	}
	info, err := os.Stat(absEntry)
	if err != nil {
		return "", fmt.Errorf("stat process entrypoint: %w", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("process entrypoint %q is not executable", entrypoint)
	}
	return absEntry, nil
}

func resolveReleasePath(releasePath, path string) string {
	if path == "" || filepath.IsAbs(path) || releasePath == "" {
		return path
	}
	return filepath.Join(releasePath, path)
}

func filteredEnv(allowlist []string) []string {
	env := make([]string, 0, len(allowlist))
	seen := map[string]struct{}{}
	for _, key := range allowlist {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func metadataFromConfig(configMap map[string]any) metadata.MD {
	raw, ok := objectValue(configMap, "metadata")
	if !ok {
		return nil
	}
	md := metadata.MD{}
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			md.Append(key, typed)
		case []any:
			for _, item := range typed {
				if str, ok := item.(string); ok {
					md.Append(key, str)
				}
			}
		}
	}
	return md
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func objectValue(input map[string]any, key string) (map[string]any, bool) {
	value, ok := input[key].(map[string]any)
	return value, ok
}

func boolValue(input map[string]any, key string, fallback bool) (bool, bool) {
	switch value := input[key].(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed, true
		}
	}
	return fallback, false
}

func boolFromMap(input map[string]any, key string, fallback bool) (bool, bool) {
	return boolValue(input, key, fallback)
}

func stringFromMap(input map[string]any, key string) string {
	return stringValue(input, key)
}

func intValue(input map[string]any, key string, fallback int) int {
	switch value := input[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func stringListValue(value any) ([]string, bool) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, true
		}
		return []string{strings.TrimSpace(typed)}, true
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				out = append(out, strings.TrimSpace(str))
			}
		}
		return out, true
	default:
		return nil, false
	}
}

type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remaining])
			b.overflow = true
		}
	} else {
		b.overflow = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func (b *limitedBuffer) Overflow() bool {
	return b.overflow
}
