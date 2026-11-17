// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof, with the real generation retention prune, that a superseded generation with a claimed work item stays out of both the prune (live work) and the relationship sweep (its scope generation row still exists), and that once the work item finishes and the prune deletes the scope generation row the sweep removes the generation's evidence, candidate and generation rows
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestClaimedWorkKeepsTheGenerationOutOfPruneAndSweepLive walks one old
// superseded generation through the two retention stages that own it. While a
// work item is claimed for it the prune keeps it (live work) and, because its
// scope generation row therefore still exists, the sweep keeps every
// relationship row too. After the work item finishes the prune deletes the
// scope generation row, the relationship rows become orphans, and only then does
// the sweep remove them. A newer superseded generation of the same scope, kept
// by the minimum-retained rule, is never touched by either.
func TestClaimedWorkKeepsTheGenerationOutOfPruneAndSweepLive(t *testing.T) {
	f := openLiveFixture(t)
	now := time.Now().UTC()
	old, newer := now.Add(-10*24*time.Hour), now.Add(-9*24*time.Hour)
	const scope, oldGen, newGen = "scope-c", "gen-c-old", "gen-c-new"
	testfixtures.SeedScope(t, f.ctx, f.db, scope)
	testfixtures.SeedSupersededGeneration(t, f.ctx, f.db, scope, oldGen, old)
	testfixtures.SeedSupersededGeneration(t, f.ctx, f.db, scope, newGen, newer)
	f.exec(t, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ('work-c-old', $1, $2, 'reducer', 'domain', 'claimed', now(), now())`, scope, oldGen)
	for _, generation := range []string{oldGen, newGen} {
		f.generation(t, scope, generation, "superseded")
		f.evidence(t, generation, 3)
		f.candidates(t, generation, 2)
	}

	store := postgres.NewGenerationRetentionStore(f.database)
	store.Now = func() time.Time { return now }
	policy := postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "10823-live",
	}

	// Stage 1: the claimed work item keeps the old generation in both.
	pruned, err := store.PruneSupersededGenerations(f.ctx, policy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations: %v", err)
	}
	if pruned.GenerationsPruned != 0 {
		t.Fatalf("the prune removed %d generations with live work claimed, want 0", pruned.GenerationsPruned)
	}
	if result := f.sweep(t); result.GenerationsSwept != 0 || len(result.Census.Generations) != 0 {
		t.Fatalf("the sweep listed %v and swept %d while the scope generation row exists, want nothing",
			result.Census.Generations, result.GenerationsSwept)
	}
	for _, generation := range []string{oldGen, newGen} {
		if got := f.of(t, generation); got != (counts{3, 2, 1, 0}) {
			t.Fatalf("%s = %+v while its work item is claimed, want every row intact", generation, got)
		}
	}

	// Stage 2: the work item finishes; the prune deletes the scope generation row.
	f.exec(t, `UPDATE fact_work_items SET status = 'succeeded', updated_at = now() WHERE work_item_id = 'work-c-old'`)
	pruned, err = store.PruneSupersededGenerations(f.ctx, policy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations after the work item finished: %v", err)
	}
	if pruned.GenerationsPruned != 1 {
		t.Fatalf("the prune removed %d generations after the work item finished, want the old one", pruned.GenerationsPruned)
	}
	if got := f.of(t, oldGen); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("%s = %+v right after the prune, want the relationship rows left for the sweep (the prune never removes them)", oldGen, got)
	}

	// Stage 3: now they are orphans, and only the old generation is.
	census := f.census(t)
	if got := census.Census.Generations; len(got) != 1 || got[0] != oldGen {
		t.Fatalf("census listed %v, want [%s]", got, oldGen)
	}
	if result := f.sweep(t); result.GenerationsSwept != 1 {
		t.Fatalf("the sweep removed %d generations, want the old one", result.GenerationsSwept)
	}
	if got := f.of(t, oldGen); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("%s = %+v after the sweep, want it gone", oldGen, got)
	}
	if got := f.of(t, newGen); got != (counts{3, 2, 1, 0}) {
		t.Fatalf("%s = %+v after both stages, want the retained generation untouched", newGen, got)
	}
}
