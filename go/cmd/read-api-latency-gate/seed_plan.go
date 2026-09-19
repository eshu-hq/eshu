// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// SeedPlanOptions configures BuildSeedPlan.
type SeedPlanOptions struct {
	// TotalScopes is the total number of ingestion_scopes rows to plan.
	// Zero (or negative) produces an empty plan.
	TotalScopes int
}

// SeedScope is one planned ingestion_scopes row.
type SeedScope struct {
	ScopeID            string
	CollectorKind      scope.CollectorKind
	ActiveGenerationID string
}

// SeedGeneration is one planned scope_generations row.
type SeedGeneration struct {
	GenerationID string
	ScopeID      string
	// Active reports whether this is the scope's current active generation
	// (scope_generations.status = 'active'); every other generation for the
	// scope is superseded, modeling re-ingestion churn.
	Active bool
}

// SeedWorkItem is one planned fact_work_items row.
type SeedWorkItem struct {
	WorkItemID   string
	ScopeID      string
	GenerationID string
	Stage        string
	Domain       string
	Status       string
}

// SeedPlan is the deterministic, pure output of BuildSeedPlan: every row the
// live seeder (seed_postgres.go) will insert, computed without touching a
// database so the distribution logic is unit-testable on its own.
type SeedPlan struct {
	Scopes                []SeedScope
	GenerationsByScope    map[string][]SeedGeneration
	WorkItemsByGeneration map[string][]SeedWorkItem
}

// factWorkItemStatuses is the status mix seeded per generation, in the
// proportions fact_work_items actually carries in a live deployment: most
// work reaches "done", a shrinking tail sits in-flight or retrying, and a
// small tail is stuck (failed/dead_letter). activeFactWorkItemsCTE
// (go/internal/storage/postgres/reducer_generation_filter_sql.go) has to scan
// every one of these regardless of status, so the mix — not just the count —
// drives its cost.
//
// "claimed" and "running" are deliberately excluded here: those two statuses
// are exactly what fact_work_items_reducer_live_lease_uniq
// (migration 005_fact_work_items.sql) restricts to at most one row per scope
// (this seeder always uses the default conflict_domain='scope' with no
// conflict_key, so every reducer row for a scope COALESCEs to the same index
// key). injectOneReducerLease adds exactly one such row per scope instead —
// see its doc comment.
var factWorkItemStatuses = []struct {
	status string
	weight int
}{
	{"done", 12},
	{"pending", 3},
	{"retrying", 3},
	{"failed", 1},
	{"dead_letter", 1},
}

// leaseStatuses alternates which of the two lease-holding statuses
// injectOneReducerLease uses, so both appear somewhere in a large plan (kept
// for TestBuildSeedPlanGeneratesGenerationChurnAndWorkItemStatusMix) without
// ever putting more than one on the same scope.
var leaseStatuses = []string{"claimed", "running"}

// workItemDomainsPerStage lists the fact domains seeded per scope generation.
// stage/domain values mirror the reducer's real fact_work_items rows so the
// planned corpus exercises the same partitions status/readiness routes read.
var workItemDomainsPerStage = []struct {
	stage  string
	domain string
}{
	{"reducer", "repository"},
	{"reducer", "code_import_repo_edge"},
	{"reducer", "package_ownership"},
}

// BuildSeedPlan deterministically distributes opts.TotalScopes ingestion
// scopes across every collector kind scope.AllCollectorKinds reports, so a
// new collector kind is picked up automatically instead of drifting out of
// sync with a hardcoded list (issue #6797). Every scope gets at least one
// generation; a subset gets multiple generations (superseded plus one active)
// to exercise the active-generation self-join re-ingestion-churn cost
// activeFactWorkItemsCTE pays. Every generation gets a fixed set of
// fact_work_items across workItemDomainsPerStage, each drawn from
// factWorkItemStatuses so the seeded corpus carries a realistic status mix.
//
// The plan is pure and deterministic: the same opts always produce the same
// scope/generation/work-item IDs, so a caller can diff two runs or replay a
// plan without touching a database.
func BuildSeedPlan(opts SeedPlanOptions) SeedPlan {
	plan := SeedPlan{
		GenerationsByScope:    map[string][]SeedGeneration{},
		WorkItemsByGeneration: map[string][]SeedWorkItem{},
	}
	if opts.TotalScopes <= 0 {
		return plan
	}

	kinds := scope.AllCollectorKinds()
	counts := distributeScopeCounts(opts.TotalScopes, len(kinds))

	scopeIndex := 0
	for ki, kind := range kinds {
		for i := 0; i < counts[ki]; i++ {
			s := SeedScope{
				ScopeID:       fmt.Sprintf("seed-scope-%s-%04d", kind, i),
				CollectorKind: kind,
			}

			// Every 5th scope gets re-ingestion churn: two superseded
			// generations plus the active one, instead of a single
			// generation. This keeps the corpus dominated by the common
			// case (one generation) while still exercising the self-join
			// cost on a meaningful minority.
			generationCount := 1
			if scopeIndex%5 == 0 {
				generationCount = 3
			}

			gens := make([]SeedGeneration, 0, generationCount)
			var activeGenerationID string
			for g := 0; g < generationCount; g++ {
				genID := fmt.Sprintf("%s-gen-%d", s.ScopeID, g)
				active := g == generationCount-1
				gens = append(gens, SeedGeneration{
					GenerationID: genID,
					ScopeID:      s.ScopeID,
					Active:       active,
				})
				if active {
					activeGenerationID = genID
				}
				plan.WorkItemsByGeneration[genID] = buildWorkItems(s.ScopeID, genID)
			}
			s.ActiveGenerationID = activeGenerationID
			injectOneReducerLease(plan.WorkItemsByGeneration[activeGenerationID], scopeIndex)

			plan.Scopes = append(plan.Scopes, s)
			plan.GenerationsByScope[s.ScopeID] = gens
			scopeIndex++
		}
	}

	return plan
}

// distributeScopeCounts splits total across n buckets as evenly as possible,
// giving every bucket at least one when total >= n. Remainder scopes go to
// the earliest buckets (kinds earlier in scope.AllCollectorKinds' order) so
// the distribution is deterministic.
func distributeScopeCounts(total, n int) []int {
	counts := make([]int, n)
	if n == 0 {
		return counts
	}
	if total < n {
		// Not enough scopes to cover every kind at least once: give the
		// first `total` kinds exactly one scope each. Callers that need
		// every kind represented should pass TotalScopes >= number of
		// collector kinds.
		for i := 0; i < total; i++ {
			counts[i] = 1
		}
		return counts
	}
	base := total / n
	remainder := total % n
	for i := range counts {
		counts[i] = base
		if i < remainder {
			counts[i]++
		}
	}
	return counts
}

// injectOneReducerLease overwrites the status of the first "reducer"-stage
// work item in items to a lease-holding status ("claimed" or "running",
// alternating by scopeIndex). It is a no-op if items has no reducer-stage
// row. Called once per scope, on that scope's active generation only, so at
// most one row per scope ever holds status IN ('claimed','running') —
// required by fact_work_items_reducer_live_lease_uniq, which does not
// distinguish which of the two statuses a row holds (see
// TestBuildSeedPlanRespectsReducerLiveLeaseUniqueness).
func injectOneReducerLease(items []SeedWorkItem, scopeIndex int) {
	for i := range items {
		if items[i].Stage != "reducer" {
			continue
		}
		items[i].Status = leaseStatuses[scopeIndex%len(leaseStatuses)]
		return
	}
}

// buildWorkItems returns the fact_work_items rows for one scope generation
// across workItemDomainsPerStage, cycling through factWorkItemStatuses so the
// generation carries every status at least once when it has enough rows.
func buildWorkItems(scopeID, generationID string) []SeedWorkItem {
	statusCycle := make([]string, 0, len(factWorkItemStatuses))
	for _, s := range factWorkItemStatuses {
		for i := 0; i < s.weight; i++ {
			statusCycle = append(statusCycle, s.status)
		}
	}

	items := make([]SeedWorkItem, 0, len(workItemDomainsPerStage)*len(statusCycle))
	n := 0
	for _, sd := range workItemDomainsPerStage {
		for _, status := range statusCycle {
			items = append(items, SeedWorkItem{
				WorkItemID:   fmt.Sprintf("%s-wi-%d", generationID, n),
				ScopeID:      scopeID,
				GenerationID: generationID,
				Stage:        sd.stage,
				Domain:       sd.domain,
				Status:       status,
			})
			n++
		}
	}
	return items
}
