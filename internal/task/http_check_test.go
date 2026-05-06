package task

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPCheckRunnerPassesAndSendsBody(t *testing.T) {
	t.Parallel()

	runner := &httpCheckRunner{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("X-Test") != "yes" {
				t.Fatalf("expected propagated header")
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if !strings.Contains(string(raw), `"name":"pulseops"`) {
				t.Fatalf("unexpected request body: %s", string(raw))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"status":"healthy"}`)),
			}, nil
		})},
		method: http.MethodPost,
		params: HTTPCheckParams{
			URL:                "http://check.test",
			Method:             http.MethodPost,
			Headers:            map[string]string{"X-Test": "yes"},
			Body:               map[string]any{"name": "pulseops"},
			ExpectStatus:       []int{http.StatusOK},
			ExpectBodyContains: "healthy",
		},
	}

	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run http check: %v", err)
	}
	if result.CheckStatus != "pass" {
		t.Fatalf("expected pass, got %q", result.CheckStatus)
	}
	if result.Summary["status_code"] != http.StatusOK {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
}

func TestHTTPCheckRunnerFailsUnexpectedResponse(t *testing.T) {
	t.Parallel()

	runner := &httpCheckRunner{
		client: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTeapot,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not healthy")),
			}, nil
		})},
		method: http.MethodGet,
		params: HTTPCheckParams{
			URL:                "http://check.test",
			ExpectStatus:       []int{http.StatusOK},
			ExpectBodyContains: "healthy",
		},
	}

	result, err := runner.Run(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("run http check: %v", err)
	}
	if result.CheckStatus != "fail" {
		t.Fatalf("expected fail, got %q", result.CheckStatus)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
