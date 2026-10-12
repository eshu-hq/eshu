// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestReindexWatermarkPrecedence is the review r2 N1 regression for ruling
// a9-125: an explicit reindex must reach the store as a reindex reason (which
// stores the re-parse), so a pending watermark beats interval_elapsed and
// graph_dirty, and it is satisfied only by a STORED full ingested at or after
// it, never by a re-projection Ack (reconcile_projected_at).
func TestReindexWatermarkPrecedence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	watermark := now.Add(-time.Minute)
	stored := now.Add(-48 * time.Hour)
	activeFull := func(lastStored, projected time.Time) scope.FullReconcileState {
		last := lastStored
		if projected.After(last) {
			last = projected
		}
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: last, LastStoredFullAt: lastStored,
			HasLatestFull: true, LatestFullAt: lastStored,
			LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
			ReconcileRequestedAt: projected.Add(-time.Minute), ReconcileProjectedAt: projected,
		}
	}
	var zero time.Time
	writer := []scope.UncoveredProjectionWriter{{GenerationID: "gen-writer", Status: scope.GenerationStatusSuperseded, ProjectionWriteStartedAt: stored.Add(time.Hour)}}
	cases := []struct {
		name       string
		interval   time.Duration
		state      scope.FullReconcileState
		writers    []scope.UncoveredProjectionWriter
		wantDue    bool
		wantReason string
	}{
		{"sweep on, interval elapsed, watermark pending", 24 * time.Hour, activeFull(stored, zero), nil, true, scope.ReconcileReasonReindexRequested},
		{"sweep on, graph_dirty, watermark pending", 24 * time.Hour, activeFull(now.Add(-time.Hour), zero), writer, true, scope.ReconcileReasonReindexRequested},
		{"re-projection acked after the watermark does not satisfy it (sweep on)", 24 * time.Hour, activeFull(stored, now.Add(-30*time.Second)), nil, true, scope.ReconcileReasonReindexRequested},
		{"re-projection acked after the watermark does not satisfy it (sweep off)", 0, activeFull(stored, now.Add(-30*time.Second)), nil, true, scope.ReconcileReasonReindexRequested},
		{"stored full at the watermark satisfies it", 24 * time.Hour, activeFull(watermark, zero), nil, false, reconcileReasonFresh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := &stubBaselineResolver{state: tc.state, writers: tc.writers}
			due, reason := ReconcileSweepDecision(context.Background(), resolver, tc.interval, watermark, time.Time{}, now, "git-repository-scope:proof", discardLogger())
			if due != tc.wantDue || reason != tc.wantReason {
				t.Fatalf("ReconcileSweepDecision() = (%v, %q), want (%v, %q)", due, reason, tc.wantDue, tc.wantReason)
			}
		})
	}
	// No loop (a9-125): the full the reindex stored is ingested at the cycle
	// start, at or after the watermark, so the next cycle reads fresh.
	afterStore := activeFull(now, zero)
	resolver := &stubBaselineResolver{state: afterStore}
	if due, reason := ReconcileSweepDecision(context.Background(), resolver, 24*time.Hour, watermark, time.Time{}, now.Add(6*time.Minute), "git-repository-scope:proof", discardLogger()); due {
		t.Fatalf("next cycle after the reindex store = (%v, %q), want held", due, reason)
	}
}
