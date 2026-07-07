// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPingWithRetrySucceedsAfterTransientFailures proves a Postgres that resets
// the first connections (warming up after pg_isready) is retried to success
// rather than killing the process on the first ping.
func TestPingWithRetrySucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()

	transient := errors.New("connection reset by peer")
	var calls int
	ping := func(context.Context) error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pingWithRetry(ctx, ping, 5*time.Millisecond, time.Second); err != nil {
		t.Fatalf("pingWithRetry = %v, want nil after transient failures", err)
	}
	if calls != 3 {
		t.Fatalf("ping called %d times, want 3 (2 transient failures + 1 success)", calls)
	}
}

// TestPingWithRetryReturnsLastErrorWhenBudgetExpires proves a genuinely
// unreachable Postgres still fails (with the last error) once the ctx budget is
// exhausted, rather than retrying forever.
func TestPingWithRetryReturnsLastErrorWhenBudgetExpires(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	var calls int
	ping := func(context.Context) error {
		calls++
		return wantErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := pingWithRetry(ctx, ping, 10*time.Millisecond, time.Second)
	if !errors.Is(err, wantErr) {
		t.Fatalf("pingWithRetry = %v, want %v after budget expiry", err, wantErr)
	}
	if calls == 0 {
		t.Fatal("ping never called")
	}
}

// TestPingWithRetryImmediateSuccess proves the happy path pings exactly once.
func TestPingWithRetryImmediateSuccess(t *testing.T) {
	t.Parallel()

	var calls int
	ping := func(context.Context) error {
		calls++
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pingWithRetry(ctx, ping, 250*time.Millisecond, time.Second); err != nil {
		t.Fatalf("pingWithRetry = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("ping called %d times, want 1 on immediate success", calls)
	}
}
