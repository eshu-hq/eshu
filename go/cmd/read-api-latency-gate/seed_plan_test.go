// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queue"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildSeedPlanCoversEveryCollectorKind(t *testing.T) {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})

	kinds := scope.AllCollectorKinds()
	if len(plan.Scopes) == 0 {
		t.Fatalf("BuildSeedPlan produced no scopes")
	}

	seen := make(map[scope.CollectorKind]int, len(kinds))
	for _, s := range plan.Scopes {
		seen[s.CollectorKind]++
	}
	for _, k := range kinds {
		if seen[k] < 1 {
			t.Errorf("collector kind %q has zero seeded scopes; every enabled collector kind must be represented", k)
		}
	}
}

func TestBuildSeedPlanRespectsTotalScopes(t *testing.T) {
	const total = 800
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: total})
	if len(plan.Scopes) != total {
		t.Fatalf("len(plan.Scopes) = %d, want %d", len(plan.Scopes), total)
	}
}

func TestBuildSeedPlanIsDeterministic(t *testing.T) {
	a := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})
	b := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})
	if len(a.Scopes) != len(b.Scopes) {
		t.Fatalf("non-deterministic scope count: %d vs %d", len(a.Scopes), len(b.Scopes))
	}
	for i := range a.Scopes {
		if a.Scopes[i].ScopeID != b.Scopes[i].ScopeID {
			t.Fatalf("non-deterministic scope id at index %d: %q vs %q", i, a.Scopes[i].ScopeID, b.Scopes[i].ScopeID)
		}
		if a.Scopes[i].CollectorKind != b.Scopes[i].CollectorKind {
			t.Fatalf("non-deterministic collector kind at index %d", i)
		}
	}
}

func TestBuildSeedPlanGeneratesGenerationChurnAndWorkItemStatusMix(t *testing.T) {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})

	multiGen := 0
	statusSeen := map[string]bool{}
	for _, s := range plan.Scopes {
		gens := plan.GenerationsByScope[s.ScopeID]
		if len(gens) == 0 {
			t.Fatalf("scope %q has zero seeded generations", s.ScopeID)
		}
		if len(gens) > 1 {
			multiGen++
		}
		for _, g := range gens {
			for _, wi := range plan.WorkItemsByGeneration[g.GenerationID] {
				statusSeen[wi.Status] = true
			}
		}
	}
	if multiGen == 0 {
		t.Errorf("expected at least some scopes with multiple generations (re-ingestion churn) to exercise the active-generation self-join cost")
	}

	wantStatuses := []string{"pending", "retrying", "claimed", "running", "dead_letter", string(queue.StatusSucceeded)}
	for _, want := range wantStatuses {
		if !statusSeen[want] {
			t.Errorf("fact_work_items status mix missing %q; activeFactWorkItemsCTE cost depends on a realistic status mix", want)
		}
	}
}

// TestBuildSeedPlanStatusesAreValidWorkItemStatuses guards against seeding a
// fabricated status the storage layer never produces. A prior version of this
// seeder wrote "done" — a string that appears nowhere in
// go/internal/queue.WorkItemStatus and nowhere in storage code — which masked
// a real query-plan regression in /collectors (the plan-flip only reproduces
// against the real terminal status, "succeeded", because that is the value
// activeFactWorkItemsCTE's live data actually carries). Check against the
// exported constant set, not a copy of the strings, so this test cannot drift
// out of sync with go/internal/queue the way the seeder itself did.
// queue.StatusFailed is intentionally excluded: it is Deprecated
// (legacy-replay only), so a status mix meant to model what production
// writes going forward must never manufacture it.
func TestBuildSeedPlanStatusesAreValidWorkItemStatuses(t *testing.T) {
	valid := map[string]bool{
		string(queue.StatusPending):    true,
		string(queue.StatusClaimed):    true,
		string(queue.StatusRunning):    true,
		string(queue.StatusRetrying):   true,
		string(queue.StatusSucceeded):  true,
		string(queue.StatusDeadLetter): true,
	}

	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})
	for _, s := range plan.Scopes {
		for _, g := range plan.GenerationsByScope[s.ScopeID] {
			for _, wi := range plan.WorkItemsByGeneration[g.GenerationID] {
				if !valid[wi.Status] {
					t.Errorf("seeded fact_work_items status %q for work item %q is not a member of queue.WorkItemStatus", wi.Status, wi.WorkItemID)
				}
			}
		}
	}
}

// TestBuildSeedPlanRespectsReducerLiveLeaseUniqueness guards against a real
// bug found running this seeder live: fact_work_items_reducer_live_lease_uniq
// (migration 005_fact_work_items.sql) is a unique index on
// (conflict_domain, COALESCE(conflict_key, scope_id)) filtered to
// stage='reducer' AND status IN ('claimed','running') — it does not include
// status as an index column, so it allows AT MOST ONE row total (claimed OR
// running, not one of each) per scope_id when conflict_domain is the default
// 'scope' (this seeder never sets conflict_key, so every row COALESCEs to
// scope_id), across EVERY generation of that scope, not just within one.
// BuildSeedPlan originally cycled the full status set (including
// claimed/running) per domain per generation, so a scope with 3 domains and
// re-ingestion churn produced several claimed/running rows for the same
// scope_id and violated the constraint on the live Postgres COPY.
func TestBuildSeedPlanRespectsReducerLiveLeaseUniqueness(t *testing.T) {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 800})

	leaseHolders := map[string]int{} // scope_id -> count of claimed/running reducer rows
	for _, s := range plan.Scopes {
		for _, g := range plan.GenerationsByScope[s.ScopeID] {
			for _, wi := range plan.WorkItemsByGeneration[g.GenerationID] {
				if wi.Stage == "reducer" && (wi.Status == "claimed" || wi.Status == "running") {
					leaseHolders[wi.ScopeID]++
				}
			}
		}
	}
	for scopeID, count := range leaseHolders {
		if count > 1 {
			t.Errorf("scope %q has %d claimed/running reducer rows, want at most 1 — fact_work_items_reducer_live_lease_uniq allows only one live lease per scope regardless of which of claimed/running it is", scopeID, count)
		}
	}
}

func TestBuildSeedPlanRejectsNonPositiveTotal(t *testing.T) {
	plan := BuildSeedPlan(SeedPlanOptions{TotalScopes: 0})
	if len(plan.Scopes) != 0 {
		t.Fatalf("expected empty plan for TotalScopes=0, got %d scopes", len(plan.Scopes))
	}
}
