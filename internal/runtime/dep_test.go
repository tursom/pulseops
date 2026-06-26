package runtime

import (
	"testing"

	"pulseops/internal/store"
)

func TestMatchCondition(t *testing.T) {
	t.Parallel()

	record := store.RunRecord{
		CheckStatus: "fail",
		RunStatus:   "success",
	}

	t.Run("empty condition always matches", func(t *testing.T) {
		if !matchCondition("", record) {
			t.Fatal("empty condition should match")
		}
	})

	t.Run("check_status equality", func(t *testing.T) {
		if !matchCondition("check_status == 'fail'", record) {
			t.Fatal("check_status == 'fail' should match")
		}
		if matchCondition("check_status == 'pass'", record) {
			t.Fatal("check_status == 'pass' should not match when check_status is fail")
		}
	})

	t.Run("run_status equality", func(t *testing.T) {
		if !matchCondition("run_status == 'success'", record) {
			t.Fatal("run_status == 'success' should match")
		}
		if matchCondition("run_status == 'failed'", record) {
			t.Fatal("run_status == 'failed' should not match when run_status is success")
		}
	})

	t.Run("unknown field never matches", func(t *testing.T) {
		if matchCondition("unknown_field == 'value'", record) {
			t.Fatal("unknown field should not match")
		}
	})

	t.Run("whitespace is trimmed", func(t *testing.T) {
		if !matchCondition("  check_status ==  'fail'  ", record) {
			t.Fatal("whitespace should be trimmed")
		}
	})
}
