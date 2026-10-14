// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

func identity(d time.Duration) time.Duration { return d }

func retryable(retryAfter time.Duration) error {
	return rest.NewRetryableError(rest.OperationList, "server_error", 4, retryAfter, errors.New("status 504"))
}

// TestFailuresCountAndBackoff pins D3: the between-cycle wait is 5 s doubling
// to a 60 s cap, a larger provider hint wins, and the hint is capped at 1 h.
func TestFailuresCountAndBackoff(t *testing.T) {
	t.Parallel()

	failures := &rest.Failures{Jitter: identity}
	for i, want := range []time.Duration{5, 10, 20, 40, 60, 60} {
		var typed *rest.RetryableError
		if !errors.As(failures.Account(retryable(0)), &typed) {
			t.Fatalf("cycle %d: not a *RetryableError", i+1)
		}
		if typed.Consecutive != i+1 || typed.RetryAfter != want*time.Second {
			t.Fatalf("cycle %d: Consecutive %d RetryAfter %v, want %d and %v", i+1, typed.Consecutive, typed.RetryAfter, i+1, want*time.Second)
		}
	}
	for _, tc := range []struct{ hint, want time.Duration }{
		{10 * time.Minute, 10 * time.Minute},
		{5 * time.Hour, time.Hour},
		{time.Second, 5 * time.Second},
	} {
		f := &rest.Failures{Jitter: identity}
		var typed *rest.RetryableError
		if !errors.As(f.Account(retryable(tc.hint)), &typed) || typed.RetryAfter != tc.want {
			t.Fatalf("hint %v: RetryAfter = %v, want %v", tc.hint, typed, tc.want)
		}
	}
}

// TestFailuresResetAndPassThrough pins that Reset clears the counter, and that
// a plain error or nil never moves it.
func TestFailuresResetAndPassThrough(t *testing.T) {
	t.Parallel()

	f := &rest.Failures{Jitter: identity}
	f.Account(retryable(0))
	f.Account(retryable(0))
	plain := errors.New("status 401")
	if got := f.Account(plain); got != plain || f.Count() != 2 {
		t.Fatalf("plain error: got %v count %d, want unchanged and 2", got, f.Count())
	}
	if got := f.Account(nil); got != nil || f.Count() != 2 {
		t.Fatalf("nil: got %v count %d, want nil and 2", got, f.Count())
	}
	f.Reset()
	if f.Count() != 0 {
		t.Fatalf("count after Reset = %d, want 0", f.Count())
	}
}

// TestFailuresCapIsFatalAndNotRetryable pins that the 20th consecutive cycle
// becomes a plain error that names the class and count and wraps only the
// cause, so errors.As finds no retryable type.
func TestFailuresCapIsFatalAndNotRetryable(t *testing.T) {
	t.Parallel()

	f := &rest.Failures{Jitter: identity}
	for cycle := 1; cycle < sdk.MaxConsecutiveTransportFailures; cycle++ {
		var typed *rest.RetryableError
		if !errors.As(f.Account(retryable(0)), &typed) {
			t.Fatalf("cycle %d: want a retryable error", cycle)
		}
	}
	err := f.Account(retryable(0))
	var typed *rest.RetryableError
	var marker interface{ Retryable() bool }
	if err == nil || errors.As(err, &typed) || errors.As(err, &marker) {
		t.Fatalf("cap error = %v, want a plain non-retryable error", err)
	}
	for _, fragment := range []string{"server_error", strconv.Itoa(sdk.MaxConsecutiveTransportFailures), "status 504"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("cap error %q does not name %q", err, fragment)
		}
	}
}

// TestRetryBudgetConstantsArePinned pins the fixed budget no operator knob may
// raise, so a permanent failure cannot be hidden.
func TestRetryBudgetConstantsArePinned(t *testing.T) {
	t.Parallel()

	cfg := rest.DefaultConfig()
	if cfg.MaxAttempts != 4 || cfg.BaseBackoff != time.Second || cfg.Jitter == nil {
		t.Fatalf("DefaultConfig() = %+v, want 4 attempts, 1s base, jitter set", cfg)
	}
	if sdk.MaxConsecutiveTransportFailures != 20 {
		t.Fatalf("consecutive-cycle cap = %d, want 20", sdk.MaxConsecutiveTransportFailures)
	}
	if rest.InCallWaitCap != 60*time.Second || rest.RetryAfterCap != time.Hour || rest.RateLimitFloor != time.Minute {
		t.Fatalf("rate-limit caps = %v, %v, %v; want 60s, 1h, 1m", rest.InCallWaitCap, rest.RetryAfterCap, rest.RateLimitFloor)
	}
	for i := 0; i < 200; i++ {
		if got := rest.Jitter(4 * time.Second); got < 3*time.Second || got > 5*time.Second {
			t.Fatalf("Jitter(4s) = %v, want within [3s, 5s]", got)
		}
	}
}

// TestRateLimitHint pins the header rules of D2.
func TestRateLimitHint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		headers   map[string]string
		wait      time.Duration
		known     bool
		signalled bool
	}{
		{"none", nil, 0, false, false},
		{"retry-after seconds", map[string]string{"Retry-After": "7"}, 7 * time.Second, true, true},
		{"retry-after date", map[string]string{"Retry-After": now.Add(9 * time.Second).Format(http.TimeFormat)}, 9 * time.Second, true, true},
		{"remaining zero with reset", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(now.Add(30 * time.Second).Unix())}, 30 * time.Second, true, true},
		{"remaining zero, no reset", map[string]string{"X-RateLimit-Remaining": "0"}, 0, false, true},
		{"remaining above zero with reset", map[string]string{"X-RateLimit-Remaining": "5", "X-RateLimit-Reset": fmt.Sprint(now.Add(30 * time.Second).Unix())}, 0, false, false},
		{"retry-after wins over reset", map[string]string{"Retry-After": "2", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(now.Add(30 * time.Second).Unix())}, 2 * time.Second, true, true},
		{"unparsable retry-after", map[string]string{"Retry-After": "soon"}, 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			headers := http.Header{}
			for k, v := range tc.headers {
				headers.Set(k, v)
			}
			wait, known, signalled := rest.RateLimitHint(headers, now)
			if wait != tc.wait || known != tc.known || signalled != tc.signalled {
				t.Fatalf("= (%v, %v, %v), want (%v, %v, %v)", wait, known, signalled, tc.wait, tc.known, tc.signalled)
			}
		})
	}
}
