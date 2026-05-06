package evaluator

import (
	"context"
	"testing"
)

func TestSteamGamePriceConsistencyEvaluate(t *testing.T) {
	t.Parallel()

	result, err := SteamGamePriceConsistency{}.Evaluate(context.Background(), Input{
		SampledItems: []map[string]any{
			{"goods_id": "1"},
			{"goods_id": "2"},
			{"goods_id": "3"},
		},
		FanoutItems: []FanoutItem{
			{
				Item:   map[string]any{"goods_id": "1", "price": 100, "name": "Alpha"},
				Detail: map[string]any{"data": map[string]any{"price": 100, "package_id": "pkg-1"}},
			},
			{
				Item:   map[string]any{"goods_id": "2", "price": 200, "is_dlc": true, "name": "Bravo"},
				Detail: map[string]any{"data": map[string]any{"price": 260, "package_id": "pkg-2"}},
			},
			{
				Item:  map[string]any{"goods_id": "3", "price": 300},
				Error: "downstream timeout",
			},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.CheckStatus != "fail" {
		t.Fatalf("expected fail, got %q", result.CheckStatus)
	}
	if result.Summary["sample_count"] != 3 {
		t.Fatalf("expected sample_count=3, got %#v", result.Summary)
	}
	if result.Summary["checked_count"] != 2 {
		t.Fatalf("expected checked_count=2, got %#v", result.Summary)
	}
	if result.Summary["mismatch_count"] != 1 {
		t.Fatalf("expected mismatch_count=1, got %#v", result.Summary)
	}
	if result.Summary["error_count"] != 1 {
		t.Fatalf("expected error_count=1, got %#v", result.Summary)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected two findings, got %#v", result.Findings)
	}
	if result.Findings[0]["reason"] != "price_mismatch" {
		t.Fatalf("expected first finding to be mismatch, got %#v", result.Findings[0])
	}
	if result.Findings[1]["reason"] != "fanout_error" {
		t.Fatalf("expected second finding to be fanout_error, got %#v", result.Findings[1])
	}
}

func TestFirstNumberSupportsNestedAndStringValues(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"data": map[string]any{
			"price": "123.45",
		},
	}
	number, ok := firstNumber(value, "data.price")
	if !ok {
		t.Fatalf("expected nested price to parse")
	}
	if number != 123.45 {
		t.Fatalf("expected 123.45, got %v", number)
	}
}
