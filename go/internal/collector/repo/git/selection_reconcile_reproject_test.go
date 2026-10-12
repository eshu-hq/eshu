// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestReconcilePolicyDecideReprojectionRequest pins the #7978 rule (ruling
// a9-117 Q3): an unchanged reconcile re-opens the active generation instead of
// storing a duplicate, so the sweep must treat an unacknowledged re-projection
// request as in flight for one interval, and then as stale: a stale or
// dead-lettered request forces a full that the store stores
// (scope.ReconcileReasonRequestStale), today's heal bounded to one per
// interval.
func TestReconcilePolicyDecideReprojectionRequest(t *testing.T) {
	t.Parallel()

	policy := reconcilePolicy{Interval: 24 * time.Hour}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	ingested := now.Add(-72 * time.Hour)
	state := func(requested, projected, deadLettered time.Time) scope.FullReconcileState {
		last := ingested
		if projected.After(last) {
			last = projected
		}
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: last,
			HasLatestFull: true, LatestFullAt: ingested,
			LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
			ReconcileRequestedAt: requested, ReconcileProjectedAt: projected,
			ReconcileDeadLetteredAt: deadLettered,
		}
	}
	var zero time.Time
	cases := []struct {
		name       string
		state      scope.FullReconcileState
		wantDue    bool
		wantReason string
	}{
		{"request 1h old, not acked: in flight", state(now.Add(-time.Hour), zero, zero), false, reconcileReasonInFlight},
		{"request just inside interval: in flight", state(now.Add(-24*time.Hour+time.Nanosecond), zero, zero), false, reconcileReasonInFlight},
		{"request exactly at interval, never acked: stale", state(now.Add(-24*time.Hour), zero, zero), true, scope.ReconcileReasonRequestStale},
		{"request dead-lettered: stale at once", state(now.Add(-time.Hour), zero, now.Add(-30*time.Minute)), true, scope.ReconcileReasonRequestStale},
		{"older dead letter does not stale a newer request", state(now.Add(-time.Hour), zero, now.Add(-2*time.Hour)), false, reconcileReasonInFlight},
		{"request acked 1h ago: fresh", state(now.Add(-2*time.Hour), now.Add(-time.Hour), zero), false, reconcileReasonFresh},
		{"request acked 25h ago: due by interval", state(now.Add(-26*time.Hour), now.Add(-25*time.Hour), zero), true, reconcileReasonIntervalElapsed},
		{"no request at all: due by interval", state(zero, zero, zero), true, reconcileReasonIntervalElapsed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			due, reason := policy.decide(now, tc.state)
			if due != tc.wantDue || reason != tc.wantReason {
				t.Fatalf("decide() = (%v, %q), want (%v, %q)", due, reason, tc.wantDue, tc.wantReason)
			}
		})
	}
}

// TestReconcileDecisionCarriesStaleReasonToGeneration proves the stale reason
// reaches the generation unchanged, which is what makes the store store it.
func TestReconcileDecisionCarriesStaleReasonToGeneration(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.Reconcile = true
	snapshot.ReconcileReason = scope.ReconcileReasonRequestStale
	collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-stale",
		time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), snapshot, false, "")
	drainFactChannel(collected.Facts)
	if got := collected.Generation.ReconcileReason; got != scope.ReconcileReasonRequestStale {
		t.Fatalf("ReconcileReason = %q, want %q", got, scope.ReconcileReasonRequestStale)
	}
}

// TestRecordReconciliationWarnsOnStaleRequest pins the operator signal for a
// stale re-projection request (ruling a9-117 Q3): the forced-reconcile log is
// WARN and names the scope and the request, Ack, and dead-letter times.
func TestRecordReconciliationWarnsOnStaleRequest(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	requested := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	gitDeltaBaseline{}.recordReconciliation(context.Background(), logger, reconcileDecision{
		Due: true, Reason: scope.ReconcileReasonRequestStale, ScopeID: "git-repository-scope:proof",
		State: scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: requested.Add(-48 * time.Hour),
			ReconcileRequestedAt: requested, ReconcileDeadLetteredAt: requested.Add(time.Minute),
		},
	})
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("decode log %q: %v", logs.String(), err)
	}
	if record["level"] != "WARN" || record["msg"] != "git_reconcile_forced" || record["reason"] != scope.ReconcileReasonRequestStale {
		t.Fatalf("log = %v, want WARN git_reconcile_forced reason=%s", record, scope.ReconcileReasonRequestStale)
	}
	for _, key := range []string{"reconcile_requested_at", "reconcile_dead_lettered_at"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("log = %v, missing %s", record, key)
		}
	}
}

// TestReconcilePolicyStaleRequestForcesOncePerInterval proves the stale path
// cannot loop (lead ask on ruling a9-117 Q3): after a stale request forced a
// full, that full is the newest full generation. While it is pending and
// younger than Interval, or failed and inside the backoff, the sweep holds the
// scope even though the active generation still carries the stale request, so
// a scope gets at most one stale-forced full per Interval, not one per cycle.
func TestReconcilePolicyStaleRequestForcesOncePerInterval(t *testing.T) {
	t.Parallel()
	policy := reconcilePolicy{Interval: 24 * time.Hour}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	staleActive := scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour),
		HasLatestFull: true, LatestFullAt: now.Add(-72 * time.Hour),
		LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
		ReconcileRequestedAt: now.Add(-25 * time.Hour),
	}
	if due, reason := policy.decide(now, staleActive); !due || reason != scope.ReconcileReasonRequestStale {
		t.Fatalf("cycle 0 decide() = (%v, %q), want the stale force", due, reason)
	}
	afterMint := staleActive
	afterMint.LatestFullAt = now.Add(time.Minute)
	afterMint.LatestFullStatus = scope.GenerationStatusPending
	afterMint.LatestFullProjected = false
	for _, cycle := range []time.Duration{6 * time.Minute, 12 * time.Hour, 24*time.Hour - time.Second} {
		if due, reason := policy.decide(now.Add(cycle), afterMint); due || reason != reconcileReasonInFlight {
			t.Fatalf("decide() %s after the stale full = (%v, %q), want held as %q", cycle, due, reason, reconcileReasonInFlight)
		}
	}
	failedMint := afterMint
	failedMint.LatestFullStatus = scope.GenerationStatusFailed
	if due, reason := policy.decide(now.Add(time.Hour), failedMint); due || reason != reconcileReasonRetryBackoff {
		t.Fatalf("decide() after the stale full failed = (%v, %q), want %q", due, reason, reconcileReasonRetryBackoff)
	}
}

// TestExpiredPendingHoldCells pins ruling a9-126 (review r2 N2): a forced
// in_flight_expired or reconcile_request_stale decision over a pending full
// whose work is live holds the scope when the remote head equals that full's
// commit (no snapshot, no parse, no budget slot). It is forced when the head
// moved or the work is gone, so a lost work row cannot wedge the scope.
func TestExpiredPendingHoldCells(t *testing.T) {
	t.Parallel()
	policy := reconcilePolicy{Interval: 24 * time.Hour}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	wedged := func(sha string, live bool) scope.FullReconcileState {
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour), LastStoredFullAt: now.Add(-72 * time.Hour),
			HasLatestFull: true, LatestFullAt: now.Add(-30 * time.Hour),
			LatestFullStatus: scope.GenerationStatusPending, LatestFullSourceCommitSHA: sha, LatestFullWorkLive: live,
		}
	}
	stale := wedged("sha-x", true)
	stale.ReconcileRequestedAt = now.Add(-73 * time.Hour)
	cases := []struct {
		name     string
		state    scope.FullReconcileState
		remote   string
		wantHeld bool
	}{
		{"pending 30h, same head, live work", wedged("sha-x", true), "sha-x", true},
		{"pending 30h, same head, work gone", wedged("sha-x", false), "sha-x", false},
		{"pending 30h, head moved", wedged("sha-x", true), "sha-y", false},
		{"stale request, pending same head, live", stale, "sha-x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			due, reason := policy.decide(now, tc.state)
			if !due {
				t.Fatalf("decide() = (%v, %q), want a forced decision before the head check", due, reason)
			}
			head := expiredPendingHoldHead(reason, tc.state)
			if held := head != "" && head == tc.remote; held != tc.wantHeld {
				t.Fatalf("held = %v (reason %q, hold head %q, remote %q), want %v", held, reason, head, tc.remote, tc.wantHeld)
			}
		})
	}
	if head := expiredPendingHoldHead(reconcileReasonIntervalElapsed, wedged("sha-x", true)); head != "" {
		t.Fatalf("interval_elapsed hold head = %q, want none", head)
	}
}
