// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"strings"
	"testing"
	"time"
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

// TestRunRetryingAppliesJitterToTheBackoff proves the loop spreads its wait: a
// command with no Jitter override sleeps within 75% to 125% of the base, and
// the waits are not all one value.
func TestRunRetryingAppliesJitterToTheBackoff(t *testing.T) {
	var waits []time.Duration
	limits := fastLimits
	limits.RetryBackoff = 4 * time.Second
	for i := 0; i < 30; i++ {
		run := &scripted{outcome: []string{dnsStderr, "ok"}}
		command := Command{
			Operation: Fetch, Host: "github.com", Scope: "github/o/r",
			Build:  run.build,
			Stderr: func() string { return run.stderr.String() },
			Sleep:  func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
		}
		if err := RunRetrying(context.Background(), limits, command); err != nil {
			t.Fatalf("RunRetrying() error = %v", err)
		}
	}
	lo, hi := limits.RetryBackoff*3/4, limits.RetryBackoff*5/4
	distinct := map[time.Duration]bool{}
	for _, d := range waits {
		if d < lo || d > hi {
			t.Fatalf("backoff %s outside [%s, %s]", d, lo, hi)
		}
		distinct[d] = true
	}
	if len(waits) != 30 || len(distinct) < 2 {
		t.Fatalf("waits = %d, distinct = %d; want 30 waits that are not one value", len(waits), len(distinct))
	}
}
