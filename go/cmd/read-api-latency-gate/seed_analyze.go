// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// analyzeSeededTables runs ANALYZE on every table this gate bulk-writes, so
// the sweep queries plan against fresh statistics instead of whatever
// autovacuum happened to have computed (or not) at seed time — a source of
// run-to-run plan and latency variance a COPY-then-immediately-sweep gate
// would otherwise carry silently.
func analyzeSeededTables(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "ANALYZE "+strings.Join(analyzedSeededTables(), ", "))
	return err
}

// analyzedSeededTables lists the tables analyzeSeededTables refreshes: every
// table in the exact-count read-back plus content_entities and content_files,
// which are seeded and verified through their own count paths (#7009 pilots).
// A test pins the list to the read-back set so a newly seeded table cannot
// be left unanalyzed once it is in the read-back set.
func analyzedSeededTables() []string {
	return []string{
		"ingestion_scopes", "scope_generations", "fact_work_items", "fact_records",
		"shared_projection_intents", "content_entities", "content_files",
	}
}
