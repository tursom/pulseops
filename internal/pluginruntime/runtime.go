package pluginruntime

import (
	"bytes"
	"context"
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
	"sync"
	"time"

	"github.com/google/uuid"

	"pulseops/internal/config"
	"pulseops/internal/pluginmodel"
)

const Protocol = "pulseops.plugin/v1"

type Client struct {
	cap pluginmodel.Capability
	cfg config.PluginsConfig
}

type Deps struct {
	HTTPClient    *http.Client
	CurrentRunID  string
	CurrentTaskID string
	TriggerType   string
}

type Request struct {
	Action string
	Config map[string]any
	Input  map[string]any
}

type Envelope struct {
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

type Response struct {
	OK      bool           `json:"ok"`
	Data    any            `json:"data"`
	Summary map[string]any `json:"summary,omitempty"`
	Error   *Error         `json:"error,omitempty"`
}

type Error struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	Details   any    `json:"details,omitempty"`
}

func NewClient(cap pluginmodel.Capability, cfg config.PluginsConfig) *Client {
	return &Client{cap: cap, cfg: cfg}
}

func (c *Client) ValidateAvailable() error {
	switch c.cap.Runtime {
	case "process":
		if !c.cfg.ProcessAllowed() {
			return fmt.Errorf("process plugin runtime is disabled")
		}
		if c.cap.Entrypoint == "" {
			return fmt.Errorf("process capability %q requires an entrypoint", c.cap.Name)
		}
		if c.cap.ReleasePath == "" {
			return fmt.Errorf("process capability %q requires a release path", c.cap.Name)
		}
		_, err := resolveEntrypoint(c.cap.ReleasePath, c.cap.Entrypoint)
		return err
	case "http", "http_plugin":
		if !c.cfg.HTTPAllowed() {
			return fmt.Errorf("http plugin runtime is disabled")
		}
		if c.cap.Endpoint == "" {
			return fmt.Errorf("http capability %q requires an endpoint", c.cap.Name)
		}
		return nil
	default:
		return fmt.Errorf("unsupported plugin runtime %q for capability %q", c.cap.Runtime, c.cap.Name)
	}
}

func (c *Client) Call(ctx context.Context, req Request, deps Deps) (Response, error) {
	if err := c.ValidateAvailable(); err != nil {
		return Response{}, err
	}
	req.Config = ResolveSecretRefs(req.Config)
	release, err := acquireLimiter(ctx, limiterKey(c.cap), c.cfg.MaxConcurrentCalls)
	if err != nil {
		return Response{}, err
	}
	defer release()
	switch c.cap.Runtime {
	case "process":
		return c.callProcess(ctx, req, deps)
	case "http", "http_plugin":
		return c.callHTTP(ctx, req, deps)
	default:
		return Response{}, fmt.Errorf("unsupported plugin runtime %q", c.cap.Runtime)
	}
}

func ResolveSecretRefs(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = resolveSecretValue(value)
	}
	return out
}

func resolveSecretValue(value any) any {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "secret://") {
			return os.Getenv(strings.TrimPrefix(typed, "secret://"))
		}
		return typed
	case map[string]any:
		if ref, ok := typed["secret_ref"].(string); ok && ref != "" {
			return os.Getenv(ref)
		}
		out := make(map[string]any, len(typed))
		for key, inner := range typed {
			out[key] = resolveSecretValue(inner)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, inner := range typed {
			out[i] = resolveSecretValue(inner)
		}
		return out
	default:
		return value
	}
}

var limiterRegistry sync.Map

type limiter struct {
	size int
	ch   chan struct{}
}

func acquireLimiter(ctx context.Context, key string, size int) (func(), error) {
	if size <= 0 {
		size = 32
	}
	value, _ := limiterRegistry.LoadOrStore(key, &limiter{size: size, ch: make(chan struct{}, size)})
	lim := value.(*limiter)
	select {
	case lim.ch <- struct{}{}:
		return func() { <-lim.ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func limiterKey(cap pluginmodel.Capability) string {
	if cap.ID != "" {
		return cap.ID
	}
	return cap.PluginID + ":" + cap.Type + ":" + cap.Name
}

func (c *Client) callHTTP(ctx context.Context, req Request, deps Deps) (Response, error) {
	timeout := Timeout(c.cfg, req.Config)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(c.envelope(req, deps, timeout))
	if err != nil {
		return Response{}, fmt.Errorf("marshal plugin request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.cap.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build plugin request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call http plugin %q: %w", c.cap.Name, err)
	}
	defer resp.Body.Close()

	raw, err := readLimited(resp.Body, MaxOutputBytes(c.cfg))
	if err != nil {
		return Response{}, fmt.Errorf("read http plugin response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("http plugin %q returned status %d: %s", c.cap.Name, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return DecodeResponse(c.cap.Name, raw)
}

func (c *Client) callProcess(ctx context.Context, req Request, deps Deps) (Response, error) {
	entrypoint, err := resolveEntrypoint(c.cap.ReleasePath, c.cap.Entrypoint)
	if err != nil {
		return Response{}, err
	}
	timeout := Timeout(c.cfg, req.Config)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(c.envelope(req, deps, timeout))
	if err != nil {
		return Response{}, fmt.Errorf("marshal plugin request: %w", err)
	}
	cmd := exec.CommandContext(callCtx, entrypoint)
	cmd.Dir = c.cap.ReleasePath
	cmd.Env = filteredEnv(c.cfg.EnvAllowlist)
	cmd.Stdin = bytes.NewReader(body)

	limit := MaxOutputBytes(c.cfg)
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return Response{}, fmt.Errorf("process plugin %q timed out after %s", c.cap.Name, timeout)
	}
	if stdout.Overflow() || stderr.Overflow() {
		return Response{}, fmt.Errorf("process plugin %q output exceeded %d bytes", c.cap.Name, limit)
	}
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return Response{}, fmt.Errorf("process plugin %q failed: %s", c.cap.Name, msg)
	}
	return DecodeResponse(c.cap.Name, stdout.Bytes())
}

func (c *Client) envelope(req Request, deps Deps, timeout time.Duration) Envelope {
	return Envelope{
		Protocol:   Protocol,
		CallID:     uuid.NewString(),
		PluginID:   c.cap.PluginID,
		Capability: c.cap.Name,
		Action:     req.Action,
		TimeoutMS:  timeout.Milliseconds(),
		Context: map[string]any{
			"task_id":      deps.CurrentTaskID,
			"run_id":       deps.CurrentRunID,
			"trigger_type": deps.TriggerType,
		},
		Config: req.Config,
		Input:  req.Input,
	}
}

func DecodeResponse(name string, raw []byte) (Response, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, fmt.Errorf("parse plugin %q response: %w", name, err)
	}
	if !resp.OK {
		if resp.Error == nil {
			return Response{}, fmt.Errorf("plugin %q returned ok=false", name)
		}
		if resp.Error.Code != "" {
			return Response{}, fmt.Errorf("plugin %q error %s: %s", name, resp.Error.Code, resp.Error.Message)
		}
		return Response{}, fmt.Errorf("plugin %q error: %s", name, resp.Error.Message)
	}
	return resp, nil
}

func Timeout(cfg config.PluginsConfig, configMap map[string]any) time.Duration {
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

func MaxOutputBytes(cfg config.PluginsConfig) int {
	if cfg.MaxOutputBytes > 0 {
		return cfg.MaxOutputBytes
	}
	return 1024 * 1024
}

func TruncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
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

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
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

func FloatToInt(value any, fallback int) int {
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
	return fallback
}
