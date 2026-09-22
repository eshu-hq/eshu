// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func testSharedIntentPlan(t *testing.T, total, pending int) SharedIntentPlan {
	t.Helper()
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 40})
	intents, err := BuildSharedIntentPlan(plan, SharedIntentPlanOptions{Total: total, Pending: pending})
	if err != nil {
		t.Fatalf("BuildSharedIntentPlan: %v", err)
	}
	return intents
}

// TestSharedIntentPlanIsProductionShaped pins the corpus shape #6820 asks for:
// every reducer projection domain carries intents, completed rows dominate,
// and the pending rows are the newest ones, as they are on a live instance
// where the worker drains the queue in created_at order.
func TestSharedIntentPlanIsProductionShaped(t *testing.T) {
	t.Parallel()

	const total, pending = 5_000, 50
	intents := testSharedIntentPlan(t, total, pending)
	if got := intents.Len(); got != total {
		t.Fatalf("Len() = %d, want %d", got, total)
	}

	domains := map[contract.Domain]int{}
	ids := map[string]struct{}{}
	pendingSeen := 0
	var lastCreated time.Time
	for i := 0; i < intents.Len(); i++ {
		row := intents.Row(i)
		if _, dup := ids[row.IntentID]; dup {
			t.Fatalf("duplicate intent id %q at %d", row.IntentID, i)
		}
		ids[row.IntentID] = struct{}{}
		domains[row.ProjectionDomain]++
		if i > 0 && !row.CreatedAt.After(lastCreated) {
			t.Fatalf("row %d created_at %s is not after row %d", i, row.CreatedAt, i-1)
		}
		lastCreated = row.CreatedAt
		if row.CompletedAt == nil {
			pendingSeen++
			if i < total-pending {
				t.Fatalf("row %d is pending but is not among the newest %d rows", i, pending)
			}
		} else if !row.CompletedAt.After(row.CreatedAt) {
			t.Fatalf("row %d completed_at %s is not after created_at %s", i, *row.CompletedAt, row.CreatedAt)
		}
		if row.ScopeID == "" || row.GenerationID == "" || row.RepositoryID == "" || row.SourceRunID == "" || row.PartitionKey == "" {
			t.Fatalf("row %d has an empty identity field: %+v", i, row)
		}
	}
	if pendingSeen != pending {
		t.Fatalf("pending rows = %d, want %d", pendingSeen, pending)
	}
	for _, domain := range contract.ProjectionDomains() {
		if domains[domain] == 0 {
			t.Errorf("projection domain %q has no seeded intents", domain)
		}
	}
	if len(domains) != len(contract.ProjectionDomains()) {
		t.Fatalf("seeded %d domains, want exactly the %d reducer projection domains", len(domains), len(contract.ProjectionDomains()))
	}
}

// TestSharedIntentPlanIsDeterministic guards the replayability the other seed
// planners promise: the same inputs always yield the same rows.
func TestSharedIntentPlanIsDeterministic(t *testing.T) {
	t.Parallel()

	a := testSharedIntentPlan(t, 1_000, 10)
	b := testSharedIntentPlan(t, 1_000, 10)
	for i := 0; i < a.Len(); i++ {
		ra, rb := a.Row(i), b.Row(i)
		if ra.IntentID != rb.IntentID || ra.ProjectionDomain != rb.ProjectionDomain || ra.GenerationID != rb.GenerationID || !ra.CreatedAt.Equal(rb.CreatedAt) {
			t.Fatalf("row %d differs between identical plans:\n%+v\n%+v", i, ra, rb)
		}
	}
}

// TestSharedIntentPlanRejectsImpossibleOptions keeps the seed from silently
// producing a corpus that cannot prove anything.
func TestSharedIntentPlanRejectsImpossibleOptions(t *testing.T) {
	t.Parallel()

	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 4})
	for name, opts := range map[string]SharedIntentPlanOptions{
		"pending exceeds total": {Total: 10, Pending: 11},
		"negative total":        {Total: -1, Pending: 0},
	} {
		if _, err := BuildSharedIntentPlan(plan, opts); err == nil {
			t.Errorf("%s: BuildSharedIntentPlan accepted %+v", name, opts)
		}
	}
	if _, err := BuildSharedIntentPlan(SeedPlan{}, SharedIntentPlanOptions{Total: 10}); err == nil {
		t.Error("BuildSharedIntentPlan accepted a seed plan with no generations")
	}
	empty, err := BuildSharedIntentPlan(plan, SharedIntentPlanOptions{})
	if err != nil || empty.Len() != 0 {
		t.Fatalf("zero total: plan=%v err=%v, want an empty plan and no error", empty.Len(), err)
	}
}

// TestSharedIntentCopySourceStreamsEveryRow proves the COPY source hands
// pgx exactly Len() rows with one value per column, without materializing
// the corpus: a 2.5M-row seed must not hold 2.5M row slices in memory.
func TestSharedIntentCopySourceStreamsEveryRow(t *testing.T) {
	t.Parallel()

	intents := testSharedIntentPlan(t, 300, 3)
	source := newSharedIntentCopySource(intents)
	rows := 0
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			t.Fatalf("Values() at row %d: %v", rows, err)
		}
		if len(values) != len(sharedIntentColumns) {
			t.Fatalf("row %d has %d values, want %d columns", rows, len(values), len(sharedIntentColumns))
		}
		rows++
	}
	if err := source.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if rows != intents.Len() {
		t.Fatalf("streamed %d rows, want %d", rows, intents.Len())
	}
}

// TestExpectedRelationalCountsIncludeSharedIntents keeps the exact-count
// read-back honest for the new table: a seed that writes fewer intents than
// planned must fail the gate instead of sweeping a shrunken corpus.
func TestExpectedRelationalCountsIncludeSharedIntents(t *testing.T) {
	t.Parallel()

	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 8})
	intents := testSharedIntentPlan(t, 120, 2)
	counts := expectedRelationalCounts(plan, nil)
	addSharedIntentCounts(counts, intents)
	if got := counts["shared_projection_intents"]; got != 120 {
		t.Fatalf("expected shared_projection_intents = %d, want 120", got)
	}
}
