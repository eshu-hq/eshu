// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"strings"
	"testing"
)

// TestJitterStaysWithinABand proves the backoff jitter keeps the wait within a
// quarter of the base either way, so 3 s never becomes 0 s or 6 s.
func TestJitterStaysWithinABand(t *testing.T) {
	base := DefaultRetryBackoff
	lo, hi := base*3/4, base*5/4
	seenDifferent := false
	first := jitterBackoff(base)
	for i := 0; i < 500; i++ {
		got := jitterBackoff(base)
		if got < lo || got > hi {
			t.Fatalf("jitterBackoff(%s) = %s, want within [%s, %s]", base, got, lo, hi)
		}
		if got != first {
			seenDifferent = true
		}
	}
	if !seenDifferent {
		t.Fatal("jitterBackoff returned one value 500 times; there is no jitter")
	}
}

// TestRetryLimitsFromEnv proves the two retry knobs: defaults, overrides, and
// a startup error naming the variable for anything outside the range.
func TestRetryLimitsFromEnv(t *testing.T) {
	limits, err := FromEnv(func(string) string { return "" })
	if err != nil || limits.RetryAttempts != DefaultRetryAttempts || limits.RetrySweepBudget != DefaultRetrySweepBudget {
		t.Fatalf("FromEnv(empty) = %+v, %v; want attempts %d, budget %d", limits, err, DefaultRetryAttempts, DefaultRetrySweepBudget)
	}
	env := map[string]string{"ESHU_GIT_RETRY_ATTEMPTS": " 3 ", "ESHU_GIT_RETRY_SWEEP_BUDGET": "50"}
	limits, err = FromEnv(func(k string) string { return env[k] })
	if err != nil || limits.RetryAttempts != 3 || limits.RetrySweepBudget != 50 {
		t.Fatalf("FromEnv(override) = %+v, %v; want attempts 3, budget 50", limits, err)
	}
	for _, name := range []string{"ESHU_GIT_RETRY_ATTEMPTS", "ESHU_GIT_RETRY_SWEEP_BUDGET"} {
		for _, value := range []string{"0", "-1", "abc", "1.5", "2s", "1001"} {
			_, err := FromEnv(func(k string) string {
				if k == name {
					return value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), value) {
				t.Errorf("FromEnv(%s=%q) error = %v, want one naming the variable and the value", name, value, err)
			}
		}
	}
	if _, err := FromEnv(func(k string) string {
		if k == "ESHU_GIT_RETRY_ATTEMPTS" {
			return "6"
		}
		return ""
	}); err == nil {
		t.Error("FromEnv(attempts=6) error = nil, want a refusal: attempts times the deadline must stay bounded")
	}
}
