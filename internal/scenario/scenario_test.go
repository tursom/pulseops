package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"pulseops/internal/evaluator"
)

func TestExtractItemsSupportsJSONPathStyle(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"data": map[string]any{
			"goods": []any{
				map[string]any{"goods_id": "1"},
				map[string]any{"goods_id": "2"},
			},
		},
	}

	items, err := extractItems(payload, "$.data.goods")
	if err != nil {
		t.Fatalf("extract items: %v", err)
	}
	if len(items) != 2 || items[0]["goods_id"] != "1" || items[1]["goods_id"] != "2" {
		t.Fatalf("unexpected items: %#v", items)
	}
}

func TestSampleItemsRandomFixedSeedIsDeterministic(t *testing.T) {
	t.Parallel()

	items := []map[string]any{
		{"goods_id": "1"},
		{"goods_id": "2"},
		{"goods_id": "3"},
		{"goods_id": "4"},
	}
	cfg := SampleConfig{
		Strategy: "random",
		Count:    3,
		SeedMode: "fixed",
		Seed:     42,
	}

	first, seedA := sampleItems(items, cfg)
	second, seedB := sampleItems(items, cfg)

	if seedA != 42 || seedB != 42 {
		t.Fatalf("expected fixed seed 42, got %d and %d", seedA, seedB)
	}
	if len(first) != len(second) {
		t.Fatalf("expected same sample size")
	}
	for i := range first {
		if first[i]["goods_id"] != second[i]["goods_id"] {
			t.Fatalf("expected deterministic samples, got %#v vs %#v", first, second)
		}
	}
}

func TestSampleItemsFixedIDs(t *testing.T) {
	t.Parallel()

	items := []map[string]any{
		{"goods_id": "1"},
		{"goods_id": "2"},
		{"goods_id": "3"},
	}

	sampled, _ := sampleItems(items, SampleConfig{
		Strategy: "fixed_ids",
		Count:    2,
		FixedIDs: []string{"2", "3"},
	})

	got := []string{sampled[0]["goods_id"].(string), sampled[1]["goods_id"].(string)}
	if !slices.Equal(got, []string{"2", "3"}) {
		t.Fatalf("unexpected fixed-id sample order: %#v", got)
	}
}

func TestApplyThresholdsEscalatesFailure(t *testing.T) {
	t.Parallel()

	status := applyThresholds("pass", map[string]any{
		"mismatch_count": 1,
		"error_count":    0,
	}, Thresholds{MaxMismatchCount: 0, MaxErrorCount: 0})

	if status != "fail" {
		t.Fatalf("expected fail, got %q", status)
	}
}

func TestExecutorRunEndToEnd(t *testing.T) {
	t.Parallel()

	registry := evaluator.NewRegistry()
	if err := registry.Register(evaluator.SteamGamePriceConsistency{}); err != nil {
		t.Fatalf("register evaluator: %v", err)
	}

	executor := Executor{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/source":
				return jsonResponse(t, http.StatusOK, map[string]any{
					"data": map[string]any{
						"goods": []map[string]any{
							{"goods_id": "1", "price": 100, "name": "A"},
							{"goods_id": "2", "price": 200, "name": "B"},
						},
					},
				}), nil
			case "/detail":
				switch req.URL.Query().Get("goods_id") {
				case "1":
					return jsonResponse(t, http.StatusOK, map[string]any{
						"data": map[string]any{"price": 100},
					}), nil
				case "2":
					return jsonResponse(t, http.StatusOK, map[string]any{
						"data": map[string]any{"price": 250, "package_id": "pkg-2"},
					}), nil
				default:
					return textResponse(http.StatusNotFound, "not found"), nil
				}
			default:
				t.Fatalf("unexpected request path %q", req.URL.Path)
				return nil, nil
			}
		})},
		Evaluators: registry,
	}
	output, err := executor.Run(context.Background(), "price-check", Params{
		Source: HTTPJSONRequest{
			Method:    http.MethodGet,
			URL:       "http://scenario.test/source",
			ItemsPath: "$.data.goods",
		},
		Sample: SampleConfig{
			Strategy: "fixed_ids",
			Count:    2,
			FixedIDs: []string{"1", "2"},
		},
		Fanout: HTTPJSONFanout{
			Method:        http.MethodGet,
			URL:           "http://scenario.test/detail",
			QueryTemplate: map[string]string{"goods_id": "{{ item.goods_id }}"},
			Concurrency:   2,
		},
		Evaluator: EvaluatorConfig{
			Name: "steam_game_price_consistency",
		},
		Thresholds: Thresholds{
			MaxMismatchCount: 1,
			MaxErrorCount:    0,
		},
	})
	if err != nil {
		t.Fatalf("executor run: %v", err)
	}
	if output.CheckStatus != "fail" {
		t.Fatalf("expected evaluator failure to be preserved, got %q", output.CheckStatus)
	}
	if output.Summary["mismatch_count"] != 1 {
		t.Fatalf("expected one mismatch, got %#v", output.Summary)
	}
	if output.Summary["sample_count"] != 2 {
		t.Fatalf("expected two sampled items, got %#v", output.Summary)
	}
	findings, ok := output.Payload["findings"].([]map[string]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("expected one finding, got %#v", output.Payload["findings"])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(t *testing.T, status int, value any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
