// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

// The guards TestCrossRepoDeadCodeConsumerEvidencePageBoundLive runs. They live
// beside the driver rather than in it so neither file approaches the 500-line
// cap; the EXPLAIN plumbing they read plans through is in
// code_dead_code_cross_repo_page_bound_live_plan_test.go.

// runCrossRepoDeadCodeConsumerPageIndexGuard fails when the shipped migrations
// did not leave the page's ordering index behind with its key columns in the
// order the statement asks for. Key order is the whole claim: the same seven
// columns in any other order still answers correctly and still makes the read
// rank the group first.
func runCrossRepoDeadCodeConsumerPageIndexGuard(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	t.Run("the migrations leave the page's ordering index", func(t *testing.T) {
		var definition string
		err := db.QueryRowContext(
			ctx,
			"SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1",
			crossRepoDeadCodeConsumerPageRankIndex,
		).Scan(&definition)
		if err != nil {
			t.Fatalf("look up %s: %v", crossRepoDeadCodeConsumerPageRankIndex, err)
		}
		const wantKey = "(entity_id, confidence DESC, depth, repository_id, root_entity_id, scope_id, generation_id)"
		if !strings.Contains(definition, wantKey) {
			t.Fatalf("%s is defined as %q, want its key to be %s -- the page's ORDER BY with entity_id pinned per page entity, ending in the scope and generation that make the order total",
				crossRepoDeadCodeConsumerPageRankIndex, definition, wantKey)
		}
	})
}

// runCrossRepoDeadCodeConsumerPageAnswerGuard reads the page through the
// shipped reader as an unscoped caller -- the read that takes the per-entity
// lateral -- and requires the rows and the per-entity truncation marker the
// route contracts for.
func runCrossRepoDeadCodeConsumerPageAnswerGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
) {
	t.Helper()

	t.Run("the page's answer is unchanged", func(t *testing.T) {
		reader := NewContentReader(db)
		evidence, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
			ctx, "repo-producer", page,
			code.CrossRepoDeadCodeConsumerReads{},
		)
		if err != nil {
			t.Fatalf("read cross-repo consumer evidence: %v", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden consumers = %d, want 0; no probe was asked for", len(hidden))
		}
		// The ordinary entities sort before the busy one, so the read returns
		// every row they have and then spends the rest of its cap inside
		// ent-hot. Each has one consumer row per ordinary repository, and none
		// of them can be truncated.
		rows := 0
		for _, entityID := range page {
			items := evidence[entityID]
			truncated := 0
			for _, item := range items {
				if item.Reason == "consumer_evidence_truncated" {
					truncated++
					continue
				}
				rows++
			}
			switch entityID {
			case "ent-hot":
				if truncated != 1 {
					t.Errorf("ent-hot carries %d truncation markers, want 1; its fan-in cannot fit the page", truncated)
				}
			default:
				if truncated != 0 {
					t.Errorf("%s carries %d truncation markers, want 0; the read moved past it", entityID, truncated)
				}
				if len(items) != crossRepoDeadCodeConsumerPageOrdinaryConsumers {
					t.Errorf("%s has %d consumer rows, want %d", entityID, len(items), crossRepoDeadCodeConsumerPageOrdinaryConsumers)
				}
			}
		}
		if rows != maxCrossRepoDeadCodeConsumerEvidenceRows {
			t.Errorf("the page returned %d consumer rows, want %d", rows, maxCrossRepoDeadCodeConsumerEvidenceRows)
		}
	})
}

// crossRepoDeadCodeConsumerPageWorkBudget bounds the entries the ranking scan
// may walk for one page. ceiling is the most it may walk; floor, when non-zero,
// is what it must walk MORE than, so an arm whose fixture stopped carrying the
// work it exists to measure fails instead of passing vacuously.
type crossRepoDeadCodeConsumerPageWorkBudget struct {
	ceiling float64
	floor   float64
}

// runCrossRepoDeadCodeConsumerPageWorkGuard plans the page under both plan
// modes and requires the shape that keeps the read bounded by the page rather
// than by a producer entity's fan-in or by the number of ingestion scopes.
func runCrossRepoDeadCodeConsumerPageWorkGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
	grant []string,
	budget crossRepoDeadCodeConsumerPageWorkBudget,
) {
	t.Helper()

	for _, mode := range crossRepoDeadCodeProbePlanModes {
		t.Run(mode.name, func(t *testing.T) {
			plan, raw := crossRepoDeadCodeConsumerPagePlan(ctx, t, db, mode, page, grant)
			failures, walked := crossRepoDeadCodeConsumerPageShapeFailures(plan, budget)
			t.Logf("ranking scan walked %.0f entries (ceiling %.0f, floor %.0f)", walked, budget.ceiling, budget.floor)
			if len(failures) > 0 {
				t.Errorf("the page read is not bounded by its page:\n  %s\nplan:\n%s", strings.Join(failures, "\n  "), raw)
			}
		})
	}
}

// crossRepoDeadCodeConsumerPageShapeFailures reads one plan, names every way it
// departs from the bounded shape, and returns the entries the ranking scan
// walked. Each check fails to a different mutation:
//
//   - the ranking scan is an Index Only Scan on migration 103's index whose
//     parent is the per-entity Limit. A Sort anywhere between them -- the
//     node this route's #6527 work removed, and the one a lateral planned from
//     an average fan-in puts back -- reads an entity's whole group before its
//     first row. An Index Only Scan, not an Index Scan, because the heap-free
//     walk is what makes the ordered path cheaper than "another index plus a
//     sort" whatever fan-in the planner assumes for a lateral parameter.
//   - every sort above the ranking scan is an Incremental Sort presorted on the
//     page's entity id, which sorts one entity's at most cap+1 rows at a time
//     and lets the outer LIMIT stop the page early. A full Sort there runs every
//     entity's lateral before the LIMIT can stop anything.
//   - the entries the ranking scan walked stay inside the budget. The liveness
//     test is a per-entry filter, so the scan walks one entry per retained
//     generation per position; the budget is the cap times (1 + retained).
//   - every other scan of code_reachability_rows -- the per-row column fetch --
//     reads at most one entry per loop. That holds because the reducer writes
//     at most one row per entity per (scope, generation, repository) snapshot.
//   - ingestion_scopes and scope_generations are read only inside SubPlans,
//     per walked entry. Read as a join they can drive the plan: the shipped
//     join form flipped, at 250 page entities on the QA replica's statistics,
//     to probing the primary key once per active ingestion scope.
func crossRepoDeadCodeConsumerPageShapeFailures(
	plan crossRepoDeadCodeConsumerPagePlanNode,
	budget crossRepoDeadCodeConsumerPageWorkBudget,
) ([]string, float64) {
	var failures []string
	ranked := 0
	walked := 0.0
	crossRepoDeadCodeConsumerPagePlanVisit(plan, nil, func(node crossRepoDeadCodeConsumerPagePlanNode, ancestors []crossRepoDeadCodeConsumerPagePlanNode) {
		inSubPlan := node.ParentRelationship == "SubPlan"
		for _, ancestor := range ancestors {
			inSubPlan = inSubPlan || ancestor.ParentRelationship == "SubPlan"
		}
		switch {
		case node.RelationName == "ingestion_scopes" || node.RelationName == "scope_generations":
			if !inSubPlan {
				failures = append(failures, fmt.Sprintf("%s is read by a %s outside a SubPlan, so the liveness tables can drive the page", node.RelationName, node.NodeType))
			}
		case node.RelationName != "code_reachability_rows" || inSubPlan:
		case node.NodeType == "Index Only Scan" && node.IndexName == crossRepoDeadCodeConsumerPageRankIndex:
			ranked++
			walked += node.entries()
			if parent := ancestors[len(ancestors)-1]; parent.NodeType != "Limit" {
				failures = append(failures, fmt.Sprintf("a %s sits between the per-entity Limit and the ranking scan, so an entity's whole group is read before its first row", parent.NodeType))
			}
			for _, ancestor := range ancestors {
				if ancestor.NodeType == "Sort" || (ancestor.NodeType == "Incremental Sort" && len(ancestor.PresortedKey) != 1) {
					failures = append(failures, fmt.Sprintf("a %s (presorted on %v) above the ranking scan sorts more than one entity's capped rows before the page LIMIT", ancestor.NodeType, ancestor.PresortedKey))
				}
			}
		case node.ActualLoops > 0 && node.ActualRows+node.RowsRemovedByFilter > 1:
			failures = append(failures, fmt.Sprintf("the column fetch (%s on %s) reads %.2f entries per loop, want at most one", node.NodeType, node.IndexName, node.ActualRows+node.RowsRemovedByFilter))
		}
	})
	if ranked == 0 {
		failures = append(failures, "no Index Only Scan on "+crossRepoDeadCodeConsumerPageRankIndex+" ranks the consumers, so nothing walks the page in its own order")
	}
	if walked > budget.ceiling {
		failures = append(failures, fmt.Sprintf("the ranking scan walked %.0f entries, want at most %.0f", walked, budget.ceiling))
	}
	if budget.floor > 0 && walked <= budget.floor {
		failures = append(failures, fmt.Sprintf("the ranking scan walked %.0f entries, want more than %.0f; the fixture no longer carries the work this arm measures", walked, budget.floor))
	}
	return failures, walked
}

// runCrossRepoDeadCodeConsumerPageCachedPlanGuard reads the plan Postgres's
// plan cache serves after twelve executions and holds it to the same shape. It
// logs which kind of plan the cache settled on, because that is the plan the
// reader's pgx connection actually runs.
func runCrossRepoDeadCodeConsumerPageCachedPlanGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
	grant []string,
	budget crossRepoDeadCodeConsumerPageWorkBudget,
) {
	t.Helper()

	t.Run("the plan cache's choice after twelve executions", func(t *testing.T) {
		plan, raw, generic, custom := crossRepoDeadCodeConsumerPageCachedPlan(ctx, t, db, page, grant)
		t.Logf("pg_prepared_statements after 12 executions: generic_plans=%d custom_plans=%d", generic, custom)
		failures, walked := crossRepoDeadCodeConsumerPageShapeFailures(plan, budget)
		t.Logf("ranking scan walked %.0f entries (ceiling %.0f)", walked, budget.ceiling)
		if len(failures) > 0 {
			t.Errorf("the cached page plan is not bounded by its page:\n  %s\nplan:\n%s", strings.Join(failures, "\n  "), raw)
		}
	})
}
