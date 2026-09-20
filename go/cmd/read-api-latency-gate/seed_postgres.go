// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedPostgres bulk-inserts plan's ingestion_scopes, scope_generations, and
// fact_work_items rows into pool via COPY, in that dependency order.
// COPY (not per-row INSERT) is what keeps an 800-scope, tens-of-thousands-
// of-work-item corpus inside the CI job's time budget (issue #6797).
//
// SeedPostgres assumes a fresh database: it does not delete or upsert, so
// re-running it against an already-seeded database fails on the primary key.
// The live gate always seeds into a compose stack it just brought up.
func SeedPostgres(ctx context.Context, pool *pgxpool.Pool, plan SeedPlan) error {
	now := time.Now().UTC()

	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"ingestion_scopes"},
		scopeColumns,
		pgx.CopyFromRows(scopeRows(plan.Scopes, now)),
	); err != nil {
		return fmt.Errorf("seed ingestion_scopes: %w", err)
	}

	generations := flattenGenerations(plan)
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"scope_generations"},
		generationColumns,
		pgx.CopyFromRows(generationRows(generations, now)),
	); err != nil {
		return fmt.Errorf("seed scope_generations: %w", err)
	}

	workItems := flattenWorkItems(plan, generations)
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"fact_work_items"},
		workItemColumns,
		pgx.CopyFromRows(workItemRows(workItems, now)),
	); err != nil {
		return fmt.Errorf("seed fact_work_items: %w", err)
	}

	return nil
}

// flattenGenerations returns every planned generation across every scope, in
// scope order (plan.Scopes' order), so the flattened slice stays
// deterministic even though plan.GenerationsByScope is a map.
func flattenGenerations(plan SeedPlan) []SeedGeneration {
	var generations []SeedGeneration
	for _, s := range plan.Scopes {
		generations = append(generations, plan.GenerationsByScope[s.ScopeID]...)
	}
	return generations
}

// flattenWorkItems returns every planned work item across every generation,
// in generations' order.
func flattenWorkItems(plan SeedPlan, generations []SeedGeneration) []SeedWorkItem {
	var items []SeedWorkItem
	for _, g := range generations {
		items = append(items, plan.WorkItemsByGeneration[g.GenerationID]...)
	}
	return items
}
