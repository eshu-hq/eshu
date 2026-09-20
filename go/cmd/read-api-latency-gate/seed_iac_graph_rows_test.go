// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestBuildIaCGraphNodeRowsCorrelateWithFacts guards the real bug this fixes
// (issue #6797 live-gate incident): go/internal/query/iac/resources.go
// hydrates Postgres-selected IaC inventory candidates against the graph via
// `MATCH (n:<label>) WHERE n.uid IN $candidate_ids`, then
// searchHydrationMatches 500s ("current inventory and graph projection
// disagree") unless every candidate has a graph row whose uid, id, name, and
// generation_id all match. Postgres' InventoryCandidate is keyed on
// entity_id/entity_name/generation_id (go/internal/query/iac/inventory_postgres.go),
// which BuildIaCFacts writes as EntityID/EntityName/GenerationID — this test
// pins that buildIaCGraphNodeRows carries those same values through
// unchanged, one row per fact, so the two independently-seeded backends
// (Postgres via SeedIaCFacts, the graph via SeedIaCGraphNodes) actually
// agree.
func TestBuildIaCGraphNodeRowsCorrelateWithFacts(t *testing.T) {
	facts := BuildIaCFacts("seed-scope-terraform_state-0000", "seed-scope-terraform_state-0000-gen-0", 30)
	rows := buildIaCGraphNodeRows(facts)

	if len(rows) != len(facts) {
		t.Fatalf("len(rows) = %d, want %d (one graph row per Postgres fact)", len(rows), len(facts))
	}
	for i, f := range facts {
		r := rows[i]
		if r.Label != f.EntityType {
			t.Errorf("rows[%d].Label = %q, want %q (must match the graph label resources.go's kind filter scans)", i, r.Label, f.EntityType)
		}
		if r.UID != f.EntityID {
			t.Errorf("rows[%d].UID = %q, want %q (the WHERE n.uid IN $candidate_ids predicate)", i, r.UID, f.EntityID)
		}
		if r.ID != f.EntityID {
			t.Errorf("rows[%d].ID = %q, want %q (the RETURNed id field searchHydrationMatches compares)", i, r.ID, f.EntityID)
		}
		if r.Name != f.EntityName {
			t.Errorf("rows[%d].Name = %q, want %q (searchHydrationMatches compares this)", i, r.Name, f.EntityName)
		}
		if r.GenerationID != f.GenerationID {
			t.Errorf("rows[%d].GenerationID = %q, want %q (searchHydrationMatches compares this)", i, r.GenerationID, f.GenerationID)
		}
	}
}

// TestBuildIaCGraphNodeRowsGroupedByLabelCoversAllEntityTypes guards that
// grouping rows by label (for one UNWIND CREATE per label, matching this
// gate's bulk-write convention — see AGENTS.md) does not silently drop an
// entity type: every entity type BuildIaCFacts cycles through must appear as
// its own label group.
func TestBuildIaCGraphNodeRowsGroupedByLabelCoversAllEntityTypes(t *testing.T) {
	facts := BuildIaCFacts("scope-1", "gen-1", 300)
	rows := buildIaCGraphNodeRows(facts)

	byLabel := groupIaCGraphNodeRowsByLabel(rows)
	for _, entityType := range iacEntityTypes {
		if len(byLabel[entityType]) == 0 {
			t.Errorf("groupIaCGraphNodeRowsByLabel has no rows for label %q", entityType)
		}
	}
	total := 0
	for _, group := range byLabel {
		total += len(group)
	}
	if total != len(rows) {
		t.Fatalf("grouped row total = %d, want %d — grouping must not drop or duplicate rows", total, len(rows))
	}
}

// TestBatchIaCGraphNodeRowsCoversEveryRowOnceWithinCap guards the chunking that
// keeps SeedIaCGraphNodes from sending one enormous UNWIND parameter list per
// label. A single ~50k-row UNWIND $rows into a label with a uid UNIQUE
// constraint stalled NornicDB at ~100% CPU for 15+ minutes in a live run
// (issue #6797), so rows must be split into bounded batches without dropping
// or duplicating any.
func TestBatchIaCGraphNodeRowsCoversEveryRowOnceWithinCap(t *testing.T) {
	rows := buildIaCGraphNodeRows(BuildIaCFacts("scope-1", "gen-1", 1050))

	batches := batchIaCGraphNodeRows(rows, 400)

	if len(batches) != 3 {
		t.Fatalf("len(batches) = %d, want 3 for 1050 rows at cap 400", len(batches))
	}
	seen := make(map[string]int, len(rows))
	for i, b := range batches {
		if len(b) == 0 || len(b) > 400 {
			t.Fatalf("batches[%d] has %d rows, want 1..400", i, len(b))
		}
		for _, r := range b {
			seen[r.UID]++
		}
	}
	if len(seen) != len(rows) {
		t.Fatalf("batched %d distinct uids, want %d", len(seen), len(rows))
	}
	for uid, n := range seen {
		if n != 1 {
			t.Fatalf("uid %q appears %d times across batches, want exactly 1", uid, n)
		}
	}
}

func TestBatchIaCGraphNodeRowsHandlesEmptyAndNonPositiveCap(t *testing.T) {
	if got := batchIaCGraphNodeRows(nil, 100); len(got) != 0 {
		t.Fatalf("empty input produced %d batches, want 0", len(got))
	}
	rows := buildIaCGraphNodeRows(BuildIaCFacts("s", "g", 5))
	if got := batchIaCGraphNodeRows(rows, 0); len(got) != 1 || len(got[0]) != 5 {
		t.Fatalf("non-positive cap must fall back to a single batch of all rows, got %d batches", len(got))
	}
}
