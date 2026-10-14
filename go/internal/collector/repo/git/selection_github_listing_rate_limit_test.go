// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
)

// TestListingRetryHonoursShortRateLimitWaitsInCall pins D2: a retry-after or
// x-ratelimit-reset hint of at most 60 s replaces the backoff and consumes
// one attempt.
func TestListingRetryHonoursShortRateLimitWaitsInCall(t *testing.T) {
	t.Parallel()

	rec := newListRetryRecorder()
	cases := []struct {
		name     string
		status   int
		headers  map[string]string
		wantWait time.Duration
	}{
		{"429 Retry-After 2", http.StatusTooManyRequests, map[string]string{"Retry-After": "2"}, 2 * time.Second},
		{"403 Retry-After 3", http.StatusForbidden, map[string]string{"Retry-After": "3"}, 3 * time.Second},
		{"429 Retry-After 60 is the cap", http.StatusTooManyRequests, map[string]string{"Retry-After": "60"}, 60 * time.Second},
		{"403 remaining 0 with reset in 30 s", http.StatusForbidden, map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     strconv.FormatInt(rec.now.Add(30*time.Second).Unix(), 10),
		}, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			local := newListRetryRecorder()
			server, pageOne := listRetryFlakyServer(t, 1, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				http.Error(w, "{}", tc.status)
			})
			records, complete, err := listRetryRun(t, server, nil, local.config(4))
			if err != nil || len(records) != 1 || !complete {
				t.Fatalf("= %d records complete %v err %v, want recovery", len(records), complete, err)
			}
			if got := local.recorded(); len(got) != 1 || got[0] != tc.wantWait {
				t.Fatalf("waits = %v, want [%v]", got, tc.wantWait)
			}
			if *pageOne != 2 {
				t.Fatalf("page-1 attempts = %d, want 2", *pageOne)
			}
		})
	}
}

// TestListingRetryDefersLongRateLimitWaitsToTheCycle pins the other half of
// D2: a wait above 60 s ends the call at once, sleeps nothing, and hands the
// wait to the collector loop capped at one hour.
func TestListingRetryDefersLongRateLimitWaitsToTheCycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		retryAfter string
		want       time.Duration
	}{
		{"403 Retry-After 3600", http.StatusForbidden, "3600", time.Hour},
		{"429 Retry-After 61", http.StatusTooManyRequests, "61", 61 * time.Second},
		{"429 Retry-After above the cap", http.StatusTooManyRequests, "86400", time.Hour},
		{"429 Retry-After that overflows a duration", http.StatusTooManyRequests, "99999999999", time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				attempts++
				mu.Unlock()
				w.Header().Set("Retry-After", tc.retryAfter)
				http.Error(w, "{}", tc.status)
			}))
			t.Cleanup(server.Close)
			rec := newListRetryRecorder()
			_, complete, err := listRetryRun(t, server, nil, rec.config(4))
			if complete {
				t.Fatal("complete = true on a failed listing")
			}
			typed := listRetryAsDiscovery(t, err)
			if typed.RetryAfter != tc.want || typed.Class != "rate_limited" || typed.Operation != "list" {
				t.Fatalf("typed = {op %q class %q retryAfter %v}, want list rate_limited %v", typed.Operation, typed.Class, typed.RetryAfter, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
			if got := rec.recorded(); len(got) != 0 {
				t.Fatalf("waits = %v, want none in-call", got)
			}
		})
	}
}

// TestListingRetryRateLimitWithoutAHintWaitsOneMinuteBetweenCycles pins the
// GitHub guidance floor (at least one minute with no header) without holding
// the cycle for it.
func TestListingRetryRateLimitWithoutAHintWaitsOneMinuteBetweenCycles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	rec := newListRetryRecorder()
	_, _, err := listRetryRun(t, server, nil, rec.config(4))
	typed := listRetryAsDiscovery(t, err)
	if typed.RetryAfter != time.Minute || len(rec.recorded()) != 0 {
		t.Fatalf("RetryAfter %v waits %v, want 1m and no in-call wait", typed.RetryAfter, rec.recorded())
	}
}

// TestListingRetryJitterSpreadsBackoff pins the 75-125% jitter shape and that
// the production budget wires it (Every shard lists the whole org, so
// lockstep retries multiply load on the provider).
func TestListingRetryJitterSpreadsBackoff(t *testing.T) {
	t.Parallel()

	if rest.DefaultConfig().Jitter == nil {
		t.Fatal("rest.DefaultConfig().Jitter = nil, want the production jitter")
	}
	for i := 0; i < 200; i++ {
		got := rest.DefaultConfig().Jitter(4 * time.Second)
		if got < 3*time.Second || got > 5*time.Second {
			t.Fatalf("jitter(4s) = %v, want within [3s, 5s]", got)
		}
	}

	server, _ := listRetryFlakyServer(t, 2, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusBadGateway)
	})
	rec := newListRetryRecorder()
	retry := rec.config(4)
	retry.Jitter = func(d time.Duration) time.Duration { return d + d/4 } // top of the band
	if _, _, err := listRetryRun(t, server, nil, retry); err != nil {
		t.Fatalf("listing error = %v", err)
	}
	want := []time.Duration{1250 * time.Millisecond, 2500 * time.Millisecond}
	if got := rec.recorded(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}

// TestListingRetryShutdownDuringBackoffIsNotRetryable pins that a cancelled
// context ends the wait with the context error, never the typed retryable
// error, so shutdown never becomes a skipped cycle.
func TestListingRetryShutdownDuringBackoffIsNotRetryable(t *testing.T) {
	t.Parallel()

	server, _ := listRetryFlakyServer(t, 10, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusBadGateway)
	})
	ctx, cancel := context.WithCancel(context.Background())
	retry := newListRetryRecorder().config(4)
	retry.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, _, err := listGitHubOrgRepositoriesFromWithRetry(ctx, server.Client(), server.URL, "acme", 100, "token-1", retry)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if collector.IsRetryable(err) {
		t.Fatalf("error %v is retryable during shutdown", err)
	}
}
