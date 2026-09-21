// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// rowCounter returns the number of rows in one seeded table.
type rowCounter func(ctx context.Context, table string) (int, error)

// standingRefreshSeedRows is the value-flow refresh singleton migration 115
// (go/internal/storage/postgres/migrations/115_value_flow_refresh_global_seed.sql)
// leaves on every freshly migrated database: the eshu:global scope, its
// genesis generation, and the succeeded code_value_flow_refresh item. The
// exact-count read-back below must name it, so a future standing row breaks
// this gate loudly instead of passing as surplus.
const standingRefreshSeedRows = 1

// expectedRelationalCounts is the row count each Postgres table this gate
// seeds holds after a correct seed: the plan's scopes, generations and work
// items, one fact_records row per IaC fact, plus the standing refresh seed
// above. Nothing else writes these tables on a freshly migrated database,
// so the counts are exact.
func expectedRelationalCounts(plan SeedPlan, facts []SeedIaCFact) map[string]int {
	return map[string]int{
		"ingestion_scopes":  len(plan.Scopes) + standingRefreshSeedRows,
		"scope_generations": countGenerations(plan) + standingRefreshSeedRows,
		"fact_work_items":   countWorkItems(plan) + standingRefreshSeedRows,
		"fact_records":      len(facts),
	}
}

// verifyRelationalCounts counts each table in expected through count and fails
// when any differs. The work metric only ever fails on MORE reads, so a seed that
// silently writes fewer rows than planned would leave a shrunken corpus reading
// as GREEN; this exact-count read-back is the lower bound the metric lacks, and it
// also fails on a surplus.
func verifyRelationalCounts(ctx context.Context, count rowCounter, expected map[string]int) error {
	actual := make(map[string]int, len(expected))
	for table := range expected {
		n, err := count(ctx, table)
		if err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}
		actual[table] = n
	}
	if mismatches := graphCountMismatches(expected, actual); len(mismatches) > 0 {
		return fmt.Errorf("seeded Postgres tables do not hold the expected row counts: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

// seededRelationalTables is the closed set of tables VerifyRelationalCounts may
// count; a table name is interpolated into SQL, so anything else is refused.
var seededRelationalTables = map[string]bool{
	"ingestion_scopes":  true,
	"scope_generations": true,
	"fact_work_items":   true,
	"fact_records":      true,
}

// VerifyRelationalCounts reads back the row counts of the seeded Postgres tables
// and fails when any differs from expected.
func VerifyRelationalCounts(ctx context.Context, pool *pgxpool.Pool, expected map[string]int) error {
	count := func(ctx context.Context, table string) (int, error) {
		if !seededRelationalTables[table] {
			return 0, fmt.Errorf("table %q is not one this gate seeds", table)
		}
		var n int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return 0, err
		}
		return int(n), nil
	}
	return verifyRelationalCounts(ctx, count, expected)
}
