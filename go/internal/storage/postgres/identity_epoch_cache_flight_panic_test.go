// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestIdentityEpochCacheLeaderPanicReleasesWaiters proves a panic inside the
// load does not strand the callers parked on the flight: they receive an error,
// the in-flight marker is cleared so the next call can lead, and the panic
// still reaches the leader's goroutine.
func TestIdentityEpochCacheLeaderPanicReleasesWaiters(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	q.loadPanic = true
	store := newFactStoreWithCache(q, 0)

	leaderPanic := make(chan any, 1)
	go func() {
		defer func() { leaderPanic <- recover() }()
		_, _ = store.ListActiveContainerImageIdentityFacts(context.Background())
	}()
	awaitLoadStarted(t, q)
	waiter := startFlightWaiters(t, store, 1)[0]
	close(q.gate)

	if got := <-leaderPanic; got != "identity load exploded" {
		t.Fatalf("leader recovered %v, want the load's panic to propagate", got)
	}
	got := collectFlightCaller(t, "waiter", waiter)
	if got.err == nil || !strings.Contains(got.err.Error(), "aborted") {
		t.Fatalf("waiter err = %v, want the flight-aborted error", got.err)
	}
	store.identityCache.mu.Lock()
	defer store.identityCache.mu.Unlock()
	if store.identityCache.loading != nil {
		t.Fatal("in-flight marker still set after the leader panicked")
	}
}
