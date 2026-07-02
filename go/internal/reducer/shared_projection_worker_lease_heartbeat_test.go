// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestProcessPartitionOnceHeartbeatsLeaseDuringSlowWrite reproduces #4449:
// ProcessPartitionOnce claims a partition lease once and holds it passively
// through selection/retract/edge-write/mark-completed with no renewal. A
// slow backend or large partition whose processing exceeds the lease TTL can
// let the lease be reclaimed by another worker while the original holder is
// still writing, causing a double-write. Sibling runners (code call
// projection, repo dependency projection) already renew their partition
// lease at TTL/2; this closes the same gap for the generic shared-projection
// partition path.
//
// LeaseTTL is set far below the edge writer's artificial delay so the
// passive (unpatched) behavior would let the lease go stale mid-cycle. The
// lease manager records every ClaimPartitionLease call after the first
// (the initial claim); the fix must issue at least one renewal claim while
// WriteEdges is still blocked, proving the lease is kept alive by heartbeat
// renewal rather than being held passively for the whole cycle.
func TestProcessPartitionOnceHeartbeatsLeaseDuringSlowWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Minute)

	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-slow-1",
				ProjectionDomain: "platform_infra",
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
				CreatedAt:        t0,
			},
		},
	}

	lease := &heartbeatCountingLeaseManager{claimResult: true}
	renewed := make(chan struct{})
	var closeOnce sync.Once
	lease.onRenew = func(count int) {
		if count == 1 {
			closeOnce.Do(func() { close(renewed) })
		}
	}

	edges := &slowEdgeWriter{
		writeBlock: renewed,
	}

	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		// A short TTL relative to the write-side block below: the periodic
		// ticker interval derived from this TTL must fire and renew the
		// lease while WriteEdges is still blocked, or the test times out.
		LeaseTTL:   40 * time.Millisecond,
		BatchLimit: 100,
	}

	done := make(chan error, 1)
	go func() {
		_, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessPartitionOnce() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessPartitionOnce did not complete: no lease renewal observed while the edge write was blocked, partition lease is not heartbeated")
	}

	if got := lease.renewCalls(); got < 1 {
		t.Fatalf("lease renewal calls = %d, want at least 1", got)
	}
	if !lease.released {
		t.Error("lease was not released")
	}
}

// TestProcessPartitionOnceReleasesLeaseWithLiveContext reproduces a bug
// introduced by the #4449 heartbeat fix itself (flagged in PR #4524 review):
// ProcessPartitionOnce reassigns its ctx variable to the heartbeat-derived
// leaseCtx, and the deferred ReleasePartitionLease call closes over that
// same ctx variable. stopHeartbeat() cancels leaseCtx before the deferred
// release runs, so the release call was handed an already-cancelled
// context -- ctxCheckingLeaseManager.ReleasePartitionLease below fails fast
// on ctx.Err() the way a real Postgres ExecContext call would, leaving the
// lease held until its own TTL expiry instead of being released promptly.
// That defeats the purpose of releasing early: other workers cannot pick up
// the partition until the stale lease naturally times out.
//
// This test uses a normal (fast) cycle -- no slow WriteEdges, no lease
// renewal needed -- specifically to isolate the release-context bug from
// the renewal behavior already covered by
// TestProcessPartitionOnceHeartbeatsLeaseDuringSlowWrite above.
func TestProcessPartitionOnceReleasesLeaseWithLiveContext(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 13, 14, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Minute)

	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-release-1",
				ProjectionDomain: "platform_infra",
				PartitionKey:     "pk-a",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
				CreatedAt:        t0,
			},
		},
	}

	lease := &ctxCheckingLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	lookup := acceptedGenerationFixed("gen-1", true)

	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
	}

	if _, err := ProcessPartitionOnce(context.Background(), now, cfg, lease, reader, edges, lookup, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}

	if lease.releaseCtxErr != nil {
		t.Fatalf("ReleasePartitionLease observed a cancelled context (%v): the lease release runs after the heartbeat context is cancelled and stays held until TTL expiry instead of releasing promptly", lease.releaseCtxErr)
	}
	if !lease.released {
		t.Error("lease was not released")
	}
}

// ctxCheckingLeaseManager records whether the context passed to
// ReleasePartitionLease was already cancelled, the way a real
// sql.DB.ExecContext call would observe and fail fast on a dead context.
type ctxCheckingLeaseManager struct {
	mu            sync.Mutex
	claimResult   bool
	released      bool
	releaseCtxErr error
}

func (l *ctxCheckingLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	return l.claimResult, nil
}

func (l *ctxCheckingLeaseManager) ReleasePartitionLease(ctx context.Context, _ string, _, _ int, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = true
	l.releaseCtxErr = ctx.Err()
	if l.releaseCtxErr != nil {
		return l.releaseCtxErr
	}
	return nil
}

// heartbeatCountingLeaseManager wraps stubLeaseManager's claim/release
// contract but distinguishes the initial claim from later renewal claims so
// tests can assert a heartbeat loop is actually renewing the lease rather
// than holding it passively for the whole processing cycle.
type heartbeatCountingLeaseManager struct {
	mu          sync.Mutex
	claimResult bool
	claimCount  int
	released    bool
	onRenew     func(count int)
}

func (l *heartbeatCountingLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	l.claimCount++
	count := l.claimCount
	l.mu.Unlock()

	if count > 1 && l.onRenew != nil {
		l.onRenew(count - 1)
	}
	return l.claimResult, nil
}

func (l *heartbeatCountingLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = true
	return nil
}

func (l *heartbeatCountingLeaseManager) renewCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.claimCount == 0 {
		return 0
	}
	return l.claimCount - 1
}

// slowEdgeWriter blocks inside WriteEdges until writeBlock is closed,
// simulating a large partition or slow backend write that exceeds a
// partition lease's TTL without renewal.
type slowEdgeWriter struct {
	writeBlock <-chan struct{}
}

func (s *slowEdgeWriter) RetractEdges(_ context.Context, _ string, _ []SharedProjectionIntentRow, _ string) error {
	return nil
}

func (s *slowEdgeWriter) WriteEdges(ctx context.Context, _ string, _ []SharedProjectionIntentRow, _ string) error {
	select {
	case <-s.writeBlock:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
