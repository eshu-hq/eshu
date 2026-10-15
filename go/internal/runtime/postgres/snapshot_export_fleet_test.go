// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var patchedBuild = []SnapshotExportBuild{{ServerVersion: "18.6 (eshu-pg r2)", Evidence: "sha256:abc"}}

// TestFleetSnapshotSetsLandOnlyOnQualifiedMember is the #7957 fleet-site
// regression: with one declared and one undeclared member, every snapshot set
// lands on the declared member, and the undeclared member is never borrowed,
// begun, exported, or imported for a set.
func TestFleetSnapshotSetsLandOnlyOnQualifiedMember(t *testing.T) {
	declared, undeclared := &exportSpyStats{}, &exportSpyStats{}
	observer := newExportSpyObserver()
	access, ctx := newExportSpyFleetAccess(t, patchedBuild, observer,
		exportSpyMember{stats: declared, version: "18.6 (eshu-pg r2)", versionNum: 180006},
		exportSpyMember{stats: undeclared, version: "18.6", versionNum: 180006},
	)
	beginner := fencedQueryer{access: access}
	for i := range 6 {
		set, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
		if err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
		if err := set.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := undeclared.snapshot(); got != [6]int64{} {
		t.Fatalf("undeclared member received statements [dials identity begins exports imports business]=%v", got)
	}
	if got := declared.snapshot(); got[3] != 6 || got[4] != 18 {
		t.Fatalf("declared member [dials identity begins exports imports business]=%v, want 6 exports and 18 imports", got)
	}
	if _, _, denials := observer.counts(); len(denials) != 0 {
		t.Fatalf("served sets counted denials %v", denials)
	}
	assertAllocatorIdle(t, access)
}

// TestFleetSnapshotSetRefusedWhenNoMemberQualifies proves the fleet refusal
// happens before any allocator reservation or borrow.
func TestFleetSnapshotSetRefusedWhenNoMemberQualifies(t *testing.T) {
	first, second := &exportSpyStats{}, &exportSpyStats{}
	observer := newExportSpyObserver()
	access, ctx := newExportSpyFleetAccess(t, nil, observer,
		exportSpyMember{stats: first, version: "18.6", versionNum: 180006},
		exportSpyMember{stats: second, version: "17.10", versionNum: 170010},
	)
	set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
	if set != nil || !errors.Is(err, db.ErrSnapshotExportUnqualified) {
		t.Fatalf("unqualified fleet set = %v, err = %v", set, err)
	}
	if first.snapshot() != [6]int64{} || second.snapshot() != [6]int64{} {
		t.Fatalf("refused fleet issued statements: %v %v", first.snapshot(), second.snapshot())
	}
	borrows, business, denials := observer.counts()
	if borrows != 0 || business != 0 || !slices.Equal(denials, []string{snapshotExportReasonNoDeclaration}) {
		t.Fatalf("refused fleet observed borrows=%d business=%d denials=%v", borrows, business, denials)
	}
	assertAllocatorIdle(t, access)
}

// TestFleetPerBorrowDenialExcludesMemberAndUsesSibling proves the per-borrow
// defense on the fleet site: a member whose stale verdict still says qualified
// but whose connection reports an undeclared build is excluded for the call
// before any BEGIN or export, its verdict flips, and the qualified sibling
// serves the set. With every member stale, the sentinel is returned.
func TestFleetPerBorrowDenialExcludesMemberAndUsesSibling(t *testing.T) {
	t.Run("sibling serves", func(t *testing.T) {
		stale, healthy := &exportSpyStats{}, &exportSpyStats{}
		observer := newExportSpyObserver()
		access, ctx := newExportSpyFleetAccess(t, patchedBuild, observer,
			exportSpyMember{stats: stale, version: "18.6", versionNum: 180006},
			exportSpyMember{stats: healthy, version: "18.6 (eshu-pg r2)", versionNum: 180006},
		)
		access.readerMembers[0].snapshotExport.observe("18.6 (eshu-pg r2)", 180006, true)
		access.nextReader.Store(0)
		set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
		if err != nil {
			t.Fatalf("sibling did not serve: %v", err)
		}
		if err := set.Close(); err != nil {
			t.Fatal(err)
		}
		if got := stale.snapshot(); got[1] != 1 || got[2] != 0 || got[3] != 0 || got[4] != 0 {
			t.Fatalf("stale member [dials identity begins exports imports business]=%v, want one identity check only", got)
		}
		if access.readerMembers[0].snapshotExport.qualified() {
			t.Fatal("stale member verdict did not flip")
		}
		if got := healthy.snapshot(); got[3] != 1 || got[4] != 3 {
			t.Fatalf("healthy member [dials identity begins exports imports business]=%v", got)
		}
		if _, _, denials := observer.counts(); len(denials) != 0 {
			t.Fatalf("served set counted denials %v", denials)
		}
		assertAllocatorIdle(t, access)
	})
	t.Run("every member stale", func(t *testing.T) {
		first, second := &exportSpyStats{}, &exportSpyStats{}
		observer := newExportSpyObserver()
		access, ctx := newExportSpyFleetAccess(t, nil, observer,
			exportSpyMember{stats: first, version: "18.6", versionNum: 180006},
			exportSpyMember{stats: second, version: "18.6", versionNum: 180006},
		)
		for i := range access.readerMembers {
			access.readerMembers[i].snapshotExport.observe("18.7", 180007, true)
		}
		set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
		if set != nil || !errors.Is(err, db.ErrSnapshotExportUnqualified) {
			t.Fatalf("all-stale fleet set = %v, err = %v", set, err)
		}
		for i, stats := range []*exportSpyStats{first, second} {
			if got := stats.snapshot(); got[2] != 0 || got[3] != 0 || got[4] != 0 {
				t.Fatalf("member %d began or exported: %v", i, got)
			}
		}
		if _, _, denials := observer.counts(); !slices.Equal(denials, []string{snapshotExportReasonNoDeclaration}) {
			t.Fatalf("denials = %v", denials)
		}
		assertAllocatorIdle(t, access)
	})
}

// TestFleetConcurrentSetsReadsAndVerdictFlipsAreRaceFree runs snapshot sets,
// plain reads, and forced verdict flips concurrently under -race. The member
// that reports an undeclared build must never export, whatever its holder
// says at the moment a set starts, and no reservation or connection leaks.
func TestFleetConcurrentSetsReadsAndVerdictFlipsAreRaceFree(t *testing.T) {
	undeclared, declared := &exportSpyStats{}, &exportSpyStats{}
	access, ctx := newExportSpyFleetAccess(t, patchedBuild, newExportSpyObserver(),
		exportSpyMember{stats: undeclared, version: "18.6", versionNum: 180006},
		exportSpyMember{stats: declared, version: "18.6 (eshu-pg r2)", versionNum: 180006},
	)
	reader := fencedQueryer{access: access}
	var workers sync.WaitGroup
	var failures sync.Map
	for worker := range 6 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 25 {
				switch worker % 3 {
				case 0:
					set, err := reader.BeginReadOnlySnapshotSet(ctx, 4)
					if err == nil {
						if closeErr := set.Close(); closeErr != nil {
							failures.Store("close", closeErr)
						}
					} else if !errors.Is(err, db.ErrSnapshotExportUnqualified) && !errors.Is(err, db.ErrSnapshotReservationCapacity) && !errors.Is(err, ErrReaderUnavailable) {
						failures.Store("set", err)
					}
				case 1:
					var value int
					if err := reader.QueryRowContext(ctx, "SELECT 1").Scan(&value); err != nil && !errors.Is(err, ErrReaderUnavailable) {
						failures.Store("read", err)
					}
				default:
					// A stale holder claims the undeclared member qualified.
					access.readerMembers[0].snapshotExport.observe("18.6 (eshu-pg r2)", 180006, true)
				}
			}
		}()
	}
	workers.Wait()
	failures.Range(func(key, value any) bool {
		t.Errorf("%v: %v", key, value)
		return true
	})
	if got := undeclared.exports.Load() + undeclared.imports.Load(); got != 0 {
		t.Fatalf("undeclared member exported or imported %d times", got)
	}
	if declared.exports.Load() == 0 {
		t.Fatal("declared member never served a set")
	}
	assertAllocatorIdle(t, access)
}

// TestFleetPerBorrowExclusionKeepsSiblingCapacityClassification proves an
// export exclusion is not a setup failure: when the stale member is excluded
// and the qualified sibling's admission wait then expires, the caller still
// gets the capacity marker (its fenced single-statement path), not the
// permanent refusal and not an unclassified failure.
func TestFleetPerBorrowExclusionKeepsSiblingCapacityClassification(t *testing.T) {
	stale, busy := &exportSpyStats{}, &exportSpyStats{}
	access, ctx := newExportSpyFleetAccess(t, patchedBuild, newExportSpyObserver(),
		exportSpyMember{stats: stale, version: "18.6", versionNum: 180006},
		exportSpyMember{stats: busy, version: "18.6 (eshu-pg r2)", versionNum: 180006},
	)
	access.replayTimeout = 60 * time.Millisecond
	access.readerMembers[0].snapshotExport.observe("18.6 (eshu-pg r2)", 180006, true)
	held, err := access.allocator.reserve(ctx, []int{1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	access.nextReader.Store(0)
	set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
	held.Release()
	if set != nil || !errors.Is(err, db.ErrSnapshotReservationCapacity) || errors.Is(err, db.ErrSnapshotExportUnqualified) {
		t.Fatalf("stale member plus saturated sibling: set=%v err=%v, want capacity marker only", set, err)
	}
	if stale.exports.Load() != 0 || busy.exports.Load() != 0 {
		t.Fatalf("exports stale=%d busy=%d", stale.exports.Load(), busy.exports.Load())
	}
	assertAllocatorIdle(t, access)
}
