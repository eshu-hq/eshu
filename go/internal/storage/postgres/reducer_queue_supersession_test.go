// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// staleGateClaimWants are the #10863 hand-out fence pieces both claim
// statements must carry: the read-only stale set, the generation guard
// against the once-per-statement active generation, and the Crossplane
// carrier rule (#10106/#10269), with no terminalizing write anywhere.
var staleGateClaimWants = []string{
	"claim_active_generations AS MATERIALIZED (",
	"stale_reducer_candidates AS MATERIALIZED (",
	"FROM stale_reducer_candidates AS superseded",
	"superseded.work_item_id = fact_work_items.work_item_id",
	"stale.status IN ('pending', 'retrying')",
	"stale.generation_id <> scope.active_generation_id",
	"stale_generation.ingested_at < scope.active_ingested_at",
	"stale_generation.generation_id < scope.active_generation_id",
	"AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))",
	// #10106/#10269: scope-keyed Crossplane SATISFIED_BY rows stay claimable
	// for the runtime rebind unless a newer pending/retrying sibling for the
	// same scope carries the debt forward.
	"stale.domain <> 'crossplane_satisfied_by_materialization'",
	"carrier.domain = 'crossplane_satisfied_by_materialization'",
	"carrier.status IN ('pending', 'retrying', 'claimed', 'running')",
	"carrier_generation.ingested_at > stale_generation.ingested_at",
	"carrier_generation.generation_id > stale_generation.generation_id",
}

var staleGateClaimBanned = []string{
	"UPDATE fact_work_items AS stale",
	"status = 'superseded'",
	"superseded_stale_reducer_locked",
	"superseded_stale_reducer_generations",
}

func TestReducerQueueClaimGatesInactiveGenerationReducerWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 3, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range staleGateClaimWants {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing inactive-generation gate %q:\n%s", want, query)
		}
	}
	for _, banned := range staleGateClaimBanned {
		if strings.Contains(query, banned) {
			t.Fatalf("claim query regained in-claim supersession %q (#10863):\n%s", banned, query)
		}
	}
}

func TestReducerQueueBatchClaimGatesInactiveGenerationReducerWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 15, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		database:      db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intents, err := queue.ClaimBatch(context.Background(), 8)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch() returned %d intents, want 0 from empty rows", len(intents))
	}

	query := db.queries[0].query
	for _, want := range staleGateClaimWants {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing inactive-generation gate %q:\n%s", want, query)
		}
	}
	for _, banned := range staleGateClaimBanned {
		if strings.Contains(query, banned) {
			t.Fatalf("batch claim query regained in-claim supersession %q (#10863):\n%s", banned, query)
		}
	}
	// The gate exclusion applies exactly once in base's WHERE clause (the
	// "superseded"-aliased NOT EXISTS asserted above) plus once per lock arm
	// as a locked-row recheck ("gate_stale"); every downstream
	// representative CTE (reps_ranked, reps) derives from base, and the
	// conflict-key representative is the reps.same_rn = 1 row selected in the
	// candidate CTE, so a stale row can never become a representative in the
	// first place (#3624 rank-once shape). Confirm the shapes that make the
	// base-level exclusion binding for the representative.
	for _, want := range []string{
		"reps_ranked AS MATERIALIZED (",
		"FROM base\n    WHERE readiness_ok",
		"WHERE reps.same_rn = 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing rank-once representative %q deriving from the gate-filtered base:\n%s", want, query)
		}
	}
	if got := strings.Count(query, "FROM stale_reducer_candidates AS gate_stale"); got != 2 {
		t.Fatalf("batch claim lock-arm gate rechecks = %d, want 2 (reserved + rest)", got)
	}
}

// TestReducerStaleSweepSupersedesInactiveGenerationReducerWork pins the
// terminalizing half of the #10863 split: the standalone periodic sweep
// statement keeps the shipped supersession semantics (predicate, carrier
// coalescing, failure markers) the claims used to run inline.
func TestReducerStaleSweepSupersedesInactiveGenerationReducerWork(t *testing.T) {
	t.Parallel()

	query := supersedeStaleReducerGenerationsQuery
	for _, want := range []string{
		"superseded_stale_reducer_generations AS (",
		"UPDATE fact_work_items AS stale",
		"status = 'superseded'",
		"failure_class = 'reducer_superseded_by_newer_active_generation'",
		"failure_message = 'reducer work superseded by newer active generation'",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"scope.active_generation_id = active_generation.generation_id",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))",
		// #10106/#10269: scope-keyed Crossplane SATISFIED_BY rows are
		// terminalized only when a newer pending/retrying sibling in the
		// locked set carries the debt forward.
		"AND (stale.domain <> 'crossplane_satisfied_by_materialization'",
		"superseded_crossplane_newest_carrier AS MATERIALIZED (",
		"carrier.domain = 'crossplane_satisfied_by_materialization'",
		"FROM superseded_crossplane_newest_carrier AS carrier",
		"carrier.status IN ('pending', 'retrying')",
		"carrier.generation_ingested_at > carrier_stale_generation.ingested_at",
		"carrier.generation_id > carrier_stale_generation.generation_id",
		"stale.generation_id = scope.active_generation_id",
		"LIMIT $3",
		`stale.work_item_id COLLATE "C" > $4::text`,
		"FOR NO KEY UPDATE OF stale SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("standalone sweep query missing inactive-generation supersession %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "stale.status IN ('pending', 'retrying', 'claimed', 'running'") {
		t.Fatalf("standalone sweep must not silently supersede live claimed/running reducer work:\n%s", query)
	}
}
