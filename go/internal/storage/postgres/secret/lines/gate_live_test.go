// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestDeferredSessionWritesDeriveNothingLive proves the bulk-load gate: a
// content write from a DeferredSessionSQL connection leaves the side table
// untouched (both the insert and the update trigger skip), while the same write
// from an ordinary connection keeps deriving.
func TestDeferredSessionWritesDeriveNothingLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	deferred := openDeferredPool(t, config)

	writeRecords(ctx, t, deferred, "repo-d", corpus("d", 40))
	if got := sideRowCount(ctx, t, db); got != 0 {
		t.Fatalf("deferred insert produced %d side rows, want 0", got)
	}
	// A changed rewrite (update trigger) from the deferred session also skips.
	changed := corpus("d", 40)
	for i := range changed {
		changed[i].Body += "\nnew_password = \"changed-value-9\"\n"
	}
	writeRecords(ctx, t, deferred, "repo-d", changed)
	if got := sideRowCount(ctx, t, db); got != 0 {
		t.Fatalf("deferred update produced %d side rows, want 0", got)
	}

	writeRecords(ctx, t, db, "repo-o", corpus("o", 40))
	if got := sideRowCount(ctx, t, db); got == 0 {
		t.Fatal("ordinary session derived no side rows; the gate must only skip deferred sessions")
	}
}

// TestBeginDeferralTakesReadinessAndBumpsEpochLive proves the state machine
// entry: a fresh schema is ready, BeginDeferral turns readiness off durably and
// every call bumps the epoch.
func TestBeginDeferralTakesReadinessAndBumpsEpochLive(t *testing.T) {
	ctx, db, _ := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}

	if state, epoch := stateRow(ctx, t, db); state != StateReady || epoch != 1 {
		t.Fatalf("fresh schema state = %q epoch %d, want ready epoch 1", state, epoch)
	}
	if ready, err := Ready(ctx, db); err != nil || !ready {
		t.Fatalf("Ready() on a fresh schema = %v, %v, want true", ready, err)
	}
	first, err := BeginDeferral(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if state, epoch := stateRow(ctx, t, db); state != StateNotBuilt || epoch != first {
		t.Fatalf("after BeginDeferral state = %q epoch %d, want not_built epoch %d", state, epoch, first)
	}
	if ready, err := Ready(ctx, db); err != nil || ready {
		t.Fatalf("Ready() after BeginDeferral = %v, %v, want false", ready, err)
	}
	second, err := BeginDeferral(ctx, sqlDB)
	if err != nil || second != first+1 {
		t.Fatalf("second BeginDeferral epoch = %d, %v, want %d", second, err, first+1)
	}
}
