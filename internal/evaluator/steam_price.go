package evaluator

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type SteamGamePriceConsistency struct{}

func (SteamGamePriceConsistency) Name() string {
	return "steam_game_price_consistency"
}

func (SteamGamePriceConsistency) Evaluate(_ context.Context, input Input) (Result, error) {
	findings := make([]map[string]any, 0)
	checkedCount := 0
	errorCount := 0
	mismatchCount := 0

	for idx, fanout := range input.FanoutItems {
		itemID := firstString(fanout.Item, "goods_id", "id", "sku_id", "item_id")
		if itemID == "" {
			itemID = fmt.Sprintf("index-%d", idx)
		}
		if fanout.Error != "" {
			errorCount++
			findings = append(findings, map[string]any{
				"sample_id": itemID,
				"reason":    "fanout_error",
				"error":     fanout.Error,
			})
			continue
		}
		expected, expectedOK := firstNumber(fanout.Item,
			"price", "display_price", "sale_price", "steam_price", "goods_price",
		)
		actual, actualOK := firstNumber(fanout.Detail,
			"price", "display_price", "sale_price", "steam_price", "goods_price",
			"data.price", "data.display_price", "data.sale_price", "data.goods.price",
		)
		if !expectedOK || !actualOK {
			errorCount++
			findings = append(findings, map[string]any{
				"sample_id": itemID,
				"reason":    "price_field_missing",
				"expected":  expected,
				"actual":    actual,
			})
			continue
		}
		checkedCount++
		if math.Abs(expected-actual) > 0.0001 {
			mismatchCount++
			findings = append(findings, map[string]any{
				"sample_id":    itemID,
				"reason":       "price_mismatch",
				"expected":     expected,
				"actual":       actual,
				"is_dlc":       firstBool(fanout.Item, "is_dlc"),
				"package_id":   firstStringFromAny(fanout.Detail, "package_id", "data.package_id"),
				"display_name": firstString(fanout.Item, "name", "title"),
			})
		}
	}

	checkStatus := "pass"
	if mismatchCount > 0 || errorCount > 0 {
		checkStatus = "fail"
	}
	return Result{
		CheckStatus: checkStatus,
		Summary: map[string]any{
			"sample_count":   len(input.SampledItems),
			"checked_count":  checkedCount,
			"mismatch_count": mismatchCount,
			"error_count":    errorCount,
		},
		Findings: findings,
	}, nil
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text := stringify(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstStringFromAny(value any, paths ...string) string {
	for _, path := range paths {
		if found, ok := lookupPath(value, path); ok {
			if text := stringify(found); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstBool(values map[string]any, key string) bool {
	raw, ok := values[key]
	if !ok {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func firstNumber(value any, paths ...string) (float64, bool) {
	for _, path := range paths {
		if found, ok := lookupPath(value, path); ok {
			number, parsed := toFloat(found)
			if parsed {
				return number, true
			}
		}
	}
	return 0, false
}

func lookupPath(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := object[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func stringify(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case jsonNumberLike:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

type jsonNumberLike interface {
	Float64() (float64, error)
}
