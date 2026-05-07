package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type httpCallSource struct{}

func (s *httpCallSource) Name() string { return "http_call" }

func (s *httpCallSource) Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error) {
	url, _ := spec.Config["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("http_call requires a url")
	}

	method, _ := spec.Config["method"].(string)
	if method == "" {
		method = "GET"
	}

	headers := map[string]string{}
	if rawHeaders, ok := spec.Config["headers"].(map[string]any); ok {
		for k, v := range rawHeaders {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	bodyStr, _ := spec.Config["body"].(string)

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = bytes.NewBufferString(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("http_call: invalid request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	timeoutStr, _ := spec.Config["timeout"].(string)
	if timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("http_call: invalid timeout %q: %w", timeoutStr, err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	httpClient := deps.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_call: request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_call: read response body: %w", err)
	}

	var jsonResult map[string]any
	if err := json.Unmarshal(bodyBytes, &jsonResult); err == nil {
		return jsonResult, nil
	}
	return map[string]any{"raw": string(bodyBytes)}, nil
}
