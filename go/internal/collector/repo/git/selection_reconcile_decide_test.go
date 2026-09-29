// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestReconcilePolicyDecide pins every decide reason and the exact Interval and
// Interval/4 boundaries of the #7288 sweep rule.
func TestReconcilePolicyDecide(t *testing.T) {
	t.Parallel()

	policy := reconcilePolicy{Interval: 24 * time.Hour}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-48 * time.Hour)
	latest := func(age time.Duration, status scope.GenerationStatus, projected bool) scope.FullReconcileState {
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: stale,
			HasLatestFull: true, LatestFullAt: now.Add(-age),
			LatestFullStatus: status, LatestFullProjected: projected,
		}
	}
	cases := []struct {
		name       string
		state      scope.FullReconcileState
		wantDue    bool
		wantReason string
	}{
		{"projected full just inside interval is fresh", projectedFullState(now.Add(-24*time.Hour + time.Nanosecond)), false, reconcileReasonFresh},
		{"projected full exactly at interval is due", projectedFullState(now.Add(-24 * time.Hour)), true, reconcileReasonIntervalElapsed},
		{"future projected full (clock skew) is fresh", projectedFullState(now.Add(time.Hour)), false, reconcileReasonFresh},
		{"no full generation at all", scope.FullReconcileState{}, true, reconcileReasonNeverReconciled},
		{"pending full 30m old is in flight", latest(30*time.Minute, scope.GenerationStatusPending, false), false, reconcileReasonInFlight},
		{"pending full just inside interval is in flight", latest(24*time.Hour-time.Nanosecond, scope.GenerationStatusPending, false), false, reconcileReasonInFlight},
		{"pending full exactly at interval has expired", latest(24*time.Hour, scope.GenerationStatusPending, false), true, reconcileReasonInFlightExpired},
		{"pending full 25h old has expired", latest(25*time.Hour, scope.GenerationStatusPending, false), true, reconcileReasonInFlightExpired},
		{"future pending full (clock skew) is in flight", latest(-time.Hour, scope.GenerationStatusPending, false), false, reconcileReasonInFlight},
		{"failed full 1h old backs off", latest(time.Hour, scope.GenerationStatusFailed, false), false, reconcileReasonRetryBackoff},
		{"failed full just inside interval/4 backs off", latest(6*time.Hour-time.Nanosecond, scope.GenerationStatusFailed, false), false, reconcileReasonRetryBackoff},
		{"failed full exactly at interval/4 retries", latest(6*time.Hour, scope.GenerationStatusFailed, false), true, reconcileReasonRetryAfterUnprojected},
		{"failed full 7h old retries", latest(7*time.Hour, scope.GenerationStatusFailed, false), true, reconcileReasonRetryAfterUnprojected},
		{"superseded-unactivated full 1h old backs off", latest(time.Hour, scope.GenerationStatusSuperseded, false), false, reconcileReasonRetryBackoff},
		{"superseded-unactivated full 7h old retries", latest(7*time.Hour, scope.GenerationStatusSuperseded, false), true, reconcileReasonRetryAfterUnprojected},
		{
			"never projected and latest full failed long ago",
			scope.FullReconcileState{HasLatestFull: true, LatestFullAt: now.Add(-7 * time.Hour), LatestFullStatus: scope.GenerationStatusFailed},
			true, reconcileReasonRetryAfterUnprojected,
		},
		{
			"never projected and latest full pending",
			scope.FullReconcileState{HasLatestFull: true, LatestFullAt: now.Add(-time.Hour), LatestFullStatus: scope.GenerationStatusPending},
			false, reconcileReasonInFlight,
		},
		{"latest full projected but stale is due by interval", latest(30*time.Hour, scope.GenerationStatusSuperseded, true), true, reconcileReasonIntervalElapsed},
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

// TestReconcilePolicyDecideGraphDirty pins the #7389 graph_dirty reason: only
// the shared throttle holds it off, and a fresh full never does.
func TestReconcilePolicyDecideGraphDirty(t *testing.T) {
	t.Parallel()
	policy := reconcilePolicy{Interval: 24 * time.Hour}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	latest := func(age time.Duration, status scope.GenerationStatus) scope.FullReconcileState {
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: fresh,
			HasLatestFull: true, LatestFullAt: now.Add(-age), LatestFullStatus: status,
		}
	}
	for _, tc := range []struct {
		name       string
		state      scope.FullReconcileState
		wantDue    bool
		wantReason string
	}{
		{"pending full younger than interval holds it", latest(30*time.Minute, scope.GenerationStatusPending), false, reconcileReasonInFlight},
		{"failed full younger than interval/4 holds it", latest(time.Hour, scope.GenerationStatusFailed), false, reconcileReasonRetryBackoff},
		{"fresh projected full does not hold it", projectedFullState(fresh), true, reconcileReasonGraphDirty},
		{"no full at all", scope.FullReconcileState{}, true, reconcileReasonGraphDirty},
		{"pending full at interval no longer holds it", latest(24*time.Hour, scope.GenerationStatusPending), true, reconcileReasonGraphDirty},
		{"failed full at interval/4 no longer holds it", latest(6*time.Hour, scope.GenerationStatusFailed), true, reconcileReasonGraphDirty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			due, reason := policy.decideGraphDirty(now, tc.state)
			if due != tc.wantDue || reason != tc.wantReason {
				t.Fatalf("decideGraphDirty() = (%v, %q), want (%v, %q)", due, reason, tc.wantDue, tc.wantReason)
			}
		})
	}
}
