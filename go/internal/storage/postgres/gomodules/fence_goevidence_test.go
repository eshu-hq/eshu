// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestFenceTakesTheExclusiveGoEvidenceLockBeforeAnythingElse pins the #11327 fence
// order: timeouts, then the exclusive go-evidence advisory lock keyed by the derived
// scope, then the readiness check, and only then the detach. Nothing that locks a row
// or changes data may run before the advisory lock.
func TestFenceTakesTheExclusiveGoEvidenceLockBeforeAnythingElse(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{holders: []string{"holder-a"}}
	if _, err := database.store().FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow); err != nil {
		t.Fatalf("FenceStaleGoEvidence() error = %v", err)
	}
	if len(database.statements) < 3 || database.statements[0] != fenceTimeoutsQuery ||
		database.statements[1] != fenceGoEvidenceLockQuery || database.statements[2] != fenceTestReadySQL {
		t.Fatalf("statements = %q, want timeouts, the exclusive lock, then the readiness check", database.statements)
	}
	scope := testFences[0].ScopeID
	if got, want := database.args[1], []any{supplychaincore.SupplyChainImpactGoEvidenceLockKey(scope)}; !slices.Equal(got, want) {
		t.Fatalf("lock args = %v, want %v", got, want)
	}
}

// TestFenceLockTimeoutDetachesNothingAndIsObserved pins ruling 2.3 on the fence side:
// a 55P03 on the exclusive lock returns the error with nothing detached, nothing
// reopened and nothing committed (the coordinator's existing retry), and the observer
// is told it was a timeout. A different lock error is not counted as a timeout.
func TestFenceLockTimeoutDetachesNothingAndIsObserved(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err         error
		wantTimeout bool
	}{
		"lock timeout": {&pgconn.PgError{Code: "55P03"}, true},
		"other error":  {errors.New("connection reset"), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := &fakeFenceDB{holders: []string{"holder-a"}, failOn: fenceGoEvidenceLockQuery, failErr: tc.err}
			var waits []time.Duration
			var timeouts []bool
			store := database.store().WithGoEvidenceLockObserver(func(_ context.Context, wait time.Duration, timedOut bool) {
				waits = append(waits, wait)
				timeouts = append(timeouts, timedOut)
			})
			result, err := store.FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want it to wrap %v", err, tc.err)
			}
			if result.Fenced != 0 || database.commits != 0 || database.rollbacks != 1 || len(database.reopened) != 0 {
				t.Fatalf("result = %+v commits = %d rollbacks = %d reopened = %q, want nothing detached, reopened or committed",
					result, database.commits, database.rollbacks, database.reopened)
			}
			want := []string{fenceTimeoutsQuery, fenceGoEvidenceLockQuery}
			if !slices.Equal(database.statements, want) {
				t.Fatalf("statements = %q, want only the timeouts and the failed lock %q", database.statements, want)
			}
			if len(timeouts) != 1 || timeouts[0] != tc.wantTimeout || waits[0] < 0 {
				t.Fatalf("observed timeouts = %v waits = %v, want one observation with timedOut = %v", timeouts, waits, tc.wantTimeout)
			}
		})
	}
}

// TestFenceObservesAGrantedLockWait pins that a granted lock is observed too, so the
// wait histogram has the whole distribution, and that a store without an observer is
// safe.
func TestFenceObservesAGrantedLockWait(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{}
	var observed int
	store := database.store().WithGoEvidenceLockObserver(func(_ context.Context, _ time.Duration, timedOut bool) {
		observed++
		if timedOut {
			t.Error("a granted lock was observed as a timeout")
		}
	})
	if _, err := store.FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow); err != nil {
		t.Fatalf("FenceStaleGoEvidence() error = %v", err)
	}
	if observed != 1 {
		t.Fatalf("observations = %d, want 1", observed)
	}
	if _, err := (&fakeFenceDB{}).store().FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow); err != nil {
		t.Fatalf("no-observer FenceStaleGoEvidence() error = %v", err)
	}
}
