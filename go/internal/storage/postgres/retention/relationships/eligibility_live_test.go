// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof of the relationship retention sweep eligibility matrix E1 to E8 through the real batch statements: only generations with no scope_generations row, no scope active pointer and no active generation row lose their evidence, candidate and generation rows (a no-generation-row evidence generation included); a generation row another session holds is skipped with every child row intact and swept once released; resolved rows are never touched; the census counts distinct generations per table; a second pass is a no-op; a fault between statements leaves the generation whole
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestEligibilityMatrixLive seeds E1 to E8 and sweeps. Rule A removes E2 and E5
// (and E8 once its row lock is released); everything else keeps every row.
func TestEligibilityMatrixLive(t *testing.T) {
	f := openLiveFixture(t)
	ids := f.eligibilityMatrix(t)
	release := f.holdGeneration(t, ids["e8"])

	// The census, with deletes off, lists exactly the eligible generations and
	// counts distinct generations (not rows) per table.
	census := f.census(t)
	if want := []string{ids["e2"], ids["e5"], ids["e8"]}; !slices.Equal(census.Census.Generations, want) {
		t.Fatalf("census listed %v, want %v", census.Census.Generations, want)
	}
	wantByTable := map[string]int64{
		relationshipretention.EvidenceTable:    3, // e2, e5, e8: 3 rows each, counted once per generation
		relationshipretention.CandidatesTable:  3,
		relationshipretention.GenerationsTable: 2, // e2, e8 (e5 has no generation row)
	}
	if !reflect.DeepEqual(census.Census.ByTable, wantByTable) {
		t.Fatalf("census per table = %v, want %v", census.Census.ByTable, wantByTable)
	}
	for name, id := range ids {
		if got := f.of(t, id); got.evidence != 3 {
			t.Fatalf("census-only pass changed %s: %+v", name, got)
		}
	}

	result := f.sweep(t)
	if result.GenerationsSwept != 2 || result.Skipped[relationshipretention.SkipLockBusy] != 1 {
		t.Fatalf("pass 1 swept %d skipped %v, want e2 and e5 swept and e8 lock_busy", result.GenerationsSwept, result.Skipped)
	}
	if result.Capped != "" {
		t.Fatalf("pass 1 capped %q, want a clean pass", result.Capped)
	}
	want := map[string]counts{
		"e1": {3, 2, 1, 0}, // in the scope window
		"e2": {0, 0, 0, 2}, // swept; its resolved rows stay (this slice never deletes one)
		"e3": {3, 2, 1, 0}, // stuck active generation row
		"e4": {3, 2, 1, 0}, // a scope pointer names it
		"e5": {0, 0, 0, 0}, // no generation row: swept through the no_rg branch
		"e6": {3, 0, 0, 0}, // pending scope_generations row
		"e7": {3, 2, 1, 0}, // refinalize in flight
		"e8": {3, 2, 1, 0}, // generation row held by another session: every child intact
	}
	for name, wantCounts := range want {
		if got := f.of(t, ids[name]); got != wantCounts {
			t.Errorf("after pass 1 %s = %+v, want %+v", name, got, wantCounts)
		}
	}

	release()
	result = f.sweep(t)
	if result.GenerationsSwept != 1 || result.Skipped[relationshipretention.SkipLockBusy] != 0 {
		t.Fatalf("pass 2 swept %d skipped %v, want only e8", result.GenerationsSwept, result.Skipped)
	}
	if got := f.of(t, ids["e8"]); got != (counts{}) {
		t.Fatalf("e8 after release = %+v, want fully swept", got)
	}

	// Idempotent: a repeat finds nothing and changes nothing.
	again := f.sweep(t)
	if again.GenerationsSwept != 0 || again.Batches != 0 || len(again.Census.Generations) != 0 {
		t.Fatalf("pass 3 = %+v, want a no-op", again)
	}
	for name, wantCounts := range want {
		if name == "e5" || name == "e8" {
			continue
		}
		if got := f.of(t, ids[name]); got != wantCounts {
			t.Errorf("after pass 3 %s = %+v, want %+v", name, got, wantCounts)
		}
	}
}

// TestSweepFaultBetweenStatementsLeavesTheGenerationWholeLive is the live proof
// of the atomic batch: a failure injected after the evidence delete (the place a
// split-transaction batch would commit a partial state) rolls everything back,
// so the generation keeps its evidence, candidates and generation row. A later
// clean pass then sweeps it.
func TestSweepFaultBetweenStatementsLeavesTheGenerationWholeLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-f")
	f.generation(t, "scope-f", "gen-f", "superseded")
	f.evidence(t, "gen-f", 4)
	f.candidates(t, "gen-f", 3)

	fault := errors.New("injected failure after the evidence delete")
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(stage string) error {
		if stage == relationshipretention.StageDeleteEvidence {
			return fault
		}
		return nil
	})
	if _, err := relationshipretention.Sweep(f.ctx, f.database, opts); !errors.Is(err, fault) {
		t.Fatalf("Sweep error = %v, want the injected fault", err)
	}
	if got := f.of(t, "gen-f"); got != (counts{4, 3, 1, 0}) {
		t.Fatalf("after the fault gen-f = %+v, want every row still present", got)
	}

	result := f.sweep(t)
	if result.GenerationsSwept != 1 {
		t.Fatalf("clean pass swept %d, want 1", result.GenerationsSwept)
	}
	if got := f.of(t, "gen-f"); got != (counts{}) {
		t.Fatalf("after the clean pass gen-f = %+v, want swept", got)
	}
}

// TestChildrenWithoutAGenerationRowAreRelistedLive pins the no_rg branch: rows
// whose generation row is already gone (the state a crash between a split
// transaction's two halves would leave) are listed by the census and removed,
// so no partial state is permanent.
func TestChildrenWithoutAGenerationRowAreRelistedLive(t *testing.T) {
	f := openLiveFixture(t)
	f.evidence(t, "gen-lonely-evidence", 2)
	f.candidates(t, "gen-lonely-candidates", 2)

	census := f.census(t)
	if want := []string{"gen-lonely-candidates", "gen-lonely-evidence"}; !slices.Equal(census.Census.Generations, want) {
		t.Fatalf("census listed %v, want both child-only generations %v", census.Census.Generations, want)
	}
	f.sweep(t)
	for _, id := range []string{"gen-lonely-evidence", "gen-lonely-candidates"} {
		if got := f.of(t, id); got != (counts{}) {
			t.Fatalf("%s = %+v after the sweep, want removed", id, got)
		}
	}
}
