package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"pulseops/internal/config"
)

type HTTPCheckParams struct {
	URL                string            `json:"url"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	Body               map[string]any    `json:"body"`
	ExpectStatus       []int             `json:"expect_status"`
	ExpectBodyContains string            `json:"expect_body_contains"`
}

type HTTPCheckDriver struct{}

func (HTTPCheckDriver) Kind() string {
	return "http_check"
}

func (HTTPCheckDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[HTTPCheckParams](spec.Params)
	if err != nil {
		return err
	}
	if params.URL == "" {
		return fmt.Errorf("http_check requires params.url")
	}
	return nil
}

func (HTTPCheckDriver) NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error) {
	params, err := DecodeParams[HTTPCheckParams](spec.Params)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(params.Method)
	if method == "" {
		method = http.MethodGet
	}
	return &httpCheckRunner{client: deps.HTTPClient, params: params, method: method}, nil
}

type httpCheckRunner struct {
	client *http.Client
	params HTTPCheckParams
	method string
}

func (r *httpCheckRunner) Run(ctx context.Context, _ TriggerType) (Result, error) {
	var bodyReader io.Reader
	if len(r.params.Body) > 0 && r.method != http.MethodGet {
		raw, err := json.Marshal(r.params.Body)
		if err != nil {
			return Result{}, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.params.URL, bodyReader)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range r.params.Headers {
		req.Header.Set(key, value)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("read response: %w", err)
	}
	checkStatus := "pass"
	if len(r.params.ExpectStatus) > 0 && !containsInt(r.params.ExpectStatus, resp.StatusCode) {
		checkStatus = "fail"
	}
	if r.params.ExpectBodyContains != "" && !strings.Contains(string(raw), r.params.ExpectBodyContains) {
		checkStatus = "fail"
	}
	return Result{
		CheckStatus: checkStatus,
		Summary: map[string]any{
			"status_code": resp.StatusCode,
		},
		Payload: map[string]any{
			"body": string(raw),
		},
	}, nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
