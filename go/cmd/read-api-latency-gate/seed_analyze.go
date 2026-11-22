// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// sqlExecer is the part of pgxpool.Pool the seed maintenance step needs, so the
// step can be tested with a recording stand-in.
type sqlExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// seededTablesMaintenanceSQL is the one statement the seed ends with:
// VACUUM (ANALYZE) over every table this gate bulk-writes. ANALYZE gives the
// sweep fresh statistics instead of whatever autovacuum had computed at seed
// time. VACUUM sets the visibility map, which an Index Only Scan reads: without
// it the entity search walk read between 32 and 512 buffers depending on how
// far autovacuum had got when the sweep started (#11224; the EXPLAIN figures are
// in docs/internal/evidence/11170-latency-gate-core-leg.md). Production reads a
// steady-state database whose visibility map autovacuum has set, so the gate
// measures that state on purpose. Not VACUUM FULL: that rewrites the tables.
func seededTablesMaintenanceSQL() string {
	return "VACUUM (ANALYZE) " + strings.Join(analyzedSeededTables(), ", ")
}

// vacuumAnalyzeSeededTables runs seededTablesMaintenanceSQL. VACUUM cannot run
// inside a transaction block, so it is one plain Exec on the pool.
func vacuumAnalyzeSeededTables(ctx context.Context, db sqlExecer) error {
	_, err := db.Exec(ctx, seededTablesMaintenanceSQL())
	return err
}

// analyzedSeededTables lists the tables seededTablesMaintenanceSQL vacuums and
// analyzes: every table in the exact-count read-back, content_entities and
// content_files (seeded and verified through their own count paths, #7009
// pilots), and the two read-model tables the seed fills before the maintenance
// step: infra_resource_entities (written by the infra backfill stand-in) and
// supply_chain_impact_canonical_winners (written by ResweepWinners). Budgeted
// routes read both, so they need the same steady state (#11224). A test pins
// the list to the read-back set so a newly seeded table cannot be left out once
// it is in the read-back set. The evidence for the visibility-map cause is in
// docs/internal/evidence/11170-latency-gate-core-leg.md.
func analyzedSeededTables() []string {
	return []string{
		"ingestion_scopes", "scope_generations", "fact_work_items", "fact_records",
		"shared_projection_intents", "content_entities", "content_files",
		"infra_resource_entities", "supply_chain_impact_canonical_winners",
	}
}
