// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSnapshotSetRefusedOnUnqualifiedSinglePoolBeforeAnyBorrow is the #7957
// regression for the single-pool export site: an undeclared reader below 18.7
// must refuse the snapshot set with the typed sentinel before any reader
// borrow, gate wait, permit, transaction, export, or import, and count the
// denial with its closed reason.
func TestSnapshotSetRefusedOnUnqualifiedSinglePoolBeforeAnyBorrow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		builds     []SnapshotExportBuild
		warm       bool
		wantReason string
	}{
		{name: "observed 18.6 with no declaration", warm: true, wantReason: snapshotExportReasonNoDeclaration},
		{name: "observed 18.6 with another build declared", warm: true, builds: []SnapshotExportBuild{{ServerVersion: "18.6 (eshu-pg r2)", Evidence: "sha256:abc"}}, wantReason: snapshotExportReasonUndeclared},
		{name: "no build observed yet", wantReason: snapshotExportReasonNoDeclaration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := &exportSpyStats{}
			observer := newExportSpyObserver()
			access, ctx := newExportSpySingleAccess(t, stats, "18.6", 180006, tc.builds, observer)
			if tc.warm {
				// A plain fenced read reports the build through checkReader,
				// the way the dial validator does at Open.
				rows, err := access.Reader().QueryContext(ctx, "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				_ = rows.Close()
				if access.snapshotExport.qualified() || access.snapshotExport.current() == nil {
					t.Fatalf("plain read did not record the unqualified build: %+v", access.snapshotExport.current())
				}
			}
			beforeStats := stats.snapshot()
			beforeBorrows, beforeBusiness, _ := observer.counts()
			beforeOpen := access.reader.Stats().OpenConnections

			set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
			if set != nil || !errors.Is(err, db.ErrSnapshotExportUnqualified) {
				if set != nil {
					_ = set.Close()
				}
				t.Fatalf("snapshot set = %v, err = %v; want ErrSnapshotExportUnqualified", set, err)
			}
			if errors.Is(err, db.ErrSnapshotReservationCapacity) || errors.Is(err, db.ErrReaderUnavailable) {
				t.Fatalf("denial carries a fallback or retry marker: %v", err)
			}
			if after := stats.snapshot(); after != beforeStats {
				t.Fatalf("refused set issued statements: before [dials identity begins exports imports business]=%v after=%v", beforeStats, after)
			}
			borrows, business, denials := observer.counts()
			if borrows != beforeBorrows || business != beforeBusiness {
				t.Fatalf("refused set observed reader_borrow %d->%d business_query %d->%d", beforeBorrows, borrows, beforeBusiness, business)
			}
			if !slices.Equal(denials, []string{tc.wantReason}) {
				t.Fatalf("denials = %v, want [%s]", denials, tc.wantReason)
			}
			if access.reader.Stats().InUse != 0 || access.reader.Stats().OpenConnections != beforeOpen ||
				len(access.readerPermits) != 4 || len(access.snapshotSetGate) != 1 {
				t.Fatalf("refusal touched reader resources: in_use=%d open=%d->%d permits=%d gate=%d",
					access.reader.Stats().InUse, beforeOpen, access.reader.Stats().OpenConnections, len(access.readerPermits), len(access.snapshotSetGate))
			}
		})
	}
}

// TestSnapshotSetOpensOneSnapshotOnDeclaredSinglePool is the positive control:
// the same reader build, once declared exactly, opens the set with exactly
// one export and one import per worker.
func TestSnapshotSetOpensOneSnapshotOnDeclaredSinglePool(t *testing.T) {
	stats := &exportSpyStats{}
	observer := newExportSpyObserver()
	builds := []SnapshotExportBuild{{ServerVersion: "18.6", Evidence: "unpatched CI mirror; guard plumbing only"}}
	access, ctx := newExportSpySingleAccess(t, stats, "18.6", 180006, builds, observer)
	access.snapshotExport.observe("18.6", 180006, false)
	set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("declared build refused: %v", err)
	}
	if got := stats.snapshot(); got[2] != 4 || got[3] != 1 || got[4] != 3 {
		t.Fatalf("declared set [dials identity begins exports imports business]=%v, want 4 begins, 1 export, 3 imports", got)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, denials := observer.counts(); len(denials) != 0 {
		t.Fatalf("declared build counted denials %v", denials)
	}
	if access.reader.Stats().InUse != 0 || len(access.readerPermits) != 4 || len(access.snapshotSetGate) != 1 {
		t.Fatal("declared set leaked reader resources")
	}
}

// TestSnapshotSetRechecksEachBorrowedConnection proves the per-borrow defense:
// a pool verdict that still says qualified does not let a connection whose
// own identity reports an unqualified build begin or export. The first borrow
// refuses, releases its connection and permit, and flips the pool verdict.
func TestSnapshotSetRechecksEachBorrowedConnection(t *testing.T) {
	stats := &exportSpyStats{}
	observer := newExportSpyObserver()
	access, ctx := newExportSpySingleAccess(t, stats, "18.6", 180006, nil, observer)
	access.snapshotExport.observe("18.7", 180007, false) // stale verdict from before an image change
	set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
	if set != nil || !errors.Is(err, db.ErrSnapshotExportUnqualified) {
		t.Fatalf("stale-verdict set = %v, err = %v; want ErrSnapshotExportUnqualified", set, err)
	}
	if got := stats.snapshot(); got[1] != 1 || got[2] != 0 || got[3] != 0 || got[4] != 0 || got[5] != 0 {
		t.Fatalf("stale-verdict set [dials identity begins exports imports business]=%v, want one identity check and nothing else", got)
	}
	if access.snapshotExport.qualified() {
		t.Fatal("unqualified borrow did not flip the pool verdict")
	}
	if _, _, denials := observer.counts(); !slices.Equal(denials, []string{snapshotExportReasonNoDeclaration}) {
		t.Fatalf("denials = %v", denials)
	}
	if access.reader.Stats().InUse != 0 || len(access.readerPermits) != 4 || len(access.snapshotSetGate) != 1 {
		t.Fatalf("per-borrow refusal leaked: in_use=%d permits=%d gate=%d", access.reader.Stats().InUse, len(access.readerPermits), len(access.snapshotSetGate))
	}
}

// TestSnapshotSetRefusalLeavesPlainReadsServing proves the refusal is scoped to
// shared-snapshot export: single fenced reads and single snapshots still work
// on the unqualified build, so MaxReadConnections stays pure capacity.
func TestSnapshotSetRefusalLeavesPlainReadsServing(t *testing.T) {
	stats := &exportSpyStats{}
	access, ctx := newExportSpySingleAccess(t, stats, "18.6", 180006, nil, newExportSpyObserver())
	reader := fencedQueryer{access: access}
	if got := reader.MaxReadConnections(); got != 4 {
		t.Fatalf("MaxReadConnections = %d, want unchanged capacity 4", got)
	}
	if _, err := reader.BeginReadOnlySnapshotSet(ctx, 4); !errors.Is(err, db.ErrSnapshotExportUnqualified) {
		t.Fatalf("set err = %v", err)
	}
	var value int
	if err := reader.QueryRowContext(ctx, "SELECT 1").Scan(&value); err != nil || value != 42 {
		t.Fatalf("plain read after refusal = %d, %v", value, err)
	}
	tx, err := reader.BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatalf("single snapshot after refusal: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if stats.exports.Load() != 0 || stats.imports.Load() != 0 {
		t.Fatal("plain reads exported or imported a snapshot")
	}
}

// TestSnapshotSetDenialAndCancellationLeaveNoReservation checks the gate,
// permits, allocator, and pools after a refusal, a canceled request, and a
// served set, on both export sites.
func TestSnapshotSetDenialAndCancellationLeaveNoReservation(t *testing.T) {
	declared := []SnapshotExportBuild{{ServerVersion: "18.6", Evidence: "ci"}}
	for _, tc := range []struct {
		name     string
		builds   []SnapshotExportBuild
		cancel   bool
		wantErr  error
		wantOpen bool
	}{
		{name: "refused", wantErr: db.ErrSnapshotExportUnqualified},
		{name: "refused while canceled", cancel: true, wantErr: db.ErrSnapshotExportUnqualified},
		{name: "qualified but canceled", builds: declared, cancel: true, wantErr: context.Canceled},
		{name: "served", builds: declared, wantOpen: true},
	} {
		t.Run("single/"+tc.name, func(t *testing.T) {
			stats := &exportSpyStats{}
			access, ctx := newExportSpySingleAccess(t, stats, "18.6", 180006, tc.builds, newExportSpyObserver())
			access.snapshotExport.observe("18.6", 180006, false)
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
			if tc.wantOpen {
				if err != nil {
					t.Fatal(err)
				}
				if err := set.Close(); err != nil {
					t.Fatal(err)
				}
			} else if set != nil || !errors.Is(err, tc.wantErr) {
				t.Fatalf("set=%v err=%v, want %v", set, err, tc.wantErr)
			}
			if stats.exports.Load() > 1 || (!tc.wantOpen && stats.exports.Load() != 0) {
				t.Fatalf("exports=%d", stats.exports.Load())
			}
			if access.reader.Stats().InUse != 0 || len(access.readerPermits) != 4 || len(access.snapshotSetGate) != 1 {
				t.Fatalf("leaked: in_use=%d permits=%d gate=%d", access.reader.Stats().InUse, len(access.readerPermits), len(access.snapshotSetGate))
			}
		})
		t.Run("fleet/"+tc.name, func(t *testing.T) {
			stats := &exportSpyStats{}
			access, ctx := newExportSpyFleetAccess(t, tc.builds, newExportSpyObserver(),
				exportSpyMember{stats: stats, version: "18.6", versionNum: 180006})
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
			if tc.wantOpen {
				if err != nil {
					t.Fatal(err)
				}
				if err := set.Close(); err != nil {
					t.Fatal(err)
				}
			} else if set != nil || !errors.Is(err, tc.wantErr) {
				t.Fatalf("set=%v err=%v, want %v", set, err, tc.wantErr)
			}
			if !tc.wantOpen && stats.exports.Load() != 0 {
				t.Fatalf("exports=%d", stats.exports.Load())
			}
			assertAllocatorIdle(t, access)
		})
	}
}
