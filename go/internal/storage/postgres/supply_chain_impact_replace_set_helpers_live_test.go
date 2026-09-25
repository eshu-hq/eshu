// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

var replaceSetLiveNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// replaceSetWriter builds the production writer over the live isolated schema.
func replaceSetWriter(db *sql.DB) reducer.PostgresSupplyChainImpactWriter {
	return reducer.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
}

func replaceSetSeedScope(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic',
          $1, $2, $2, 'active', '{}'::jsonb)`,
		replaceSetLiveScope, replaceSetLiveNow,
	); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	for _, generation := range []struct{ id, status string }{
		{replaceSetLiveGeneration + ":prior", "superseded"},
		{replaceSetLiveGeneration, "active"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', $3, $3, $4, $3, '{}'::jsonb)`,
			generation.id, replaceSetLiveScope, replaceSetLiveNow, generation.status,
		); err != nil {
			t.Fatalf("insert generation %s: %v", generation.id, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		replaceSetLiveGeneration, replaceSetLiveScope,
	); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
}

// replaceSetPlantRow inserts one active fact row directly, bypassing the
// writer, so a test can place a row the pass under test must leave alone.
func replaceSetPlantRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID, scopeID, generationID, factKind string,
	fencingToken int64,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload, fencing_token
) VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, $5, $5, FALSE, '{}'::jsonb, $6)`,
		factID, scopeID, generationID, factKind, replaceSetLiveNow, fencingToken,
	); err != nil {
		t.Fatalf("plant row %s: %v", factID, err)
	}
}

// replaceSetActiveRepositories returns the repository_id of every active
// finding row in the test's (scope, generation), sorted.
func replaceSetActiveRepositories(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT COALESCE(payload->>'repository_id', '')
FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3
  AND is_tombstone = FALSE
ORDER BY 1, fact_id`,
		replaceSetLiveScope, replaceSetLiveGeneration, facts.ReducerSupplyChainImpactFindingFactKind,
	)
	if err != nil {
		t.Fatalf("list active findings: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var repositoryID string
		if err := rows.Scan(&repositoryID); err != nil {
			t.Fatalf("scan active finding: %v", err)
		}
		out = append(out, repositoryID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active findings: %v", err)
	}
	return out
}

func replaceSetTombstoneCount(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3 AND is_tombstone = TRUE`,
		replaceSetLiveScope, replaceSetLiveGeneration, facts.ReducerSupplyChainImpactFindingFactKind,
	).Scan(&count); err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	return count
}

func replaceSetIsTombstone(t *testing.T, ctx context.Context, db *sql.DB, factID string) bool {
	t.Helper()
	var tombstone bool
	if err := db.QueryRowContext(ctx,
		`SELECT is_tombstone FROM fact_records WHERE fact_id = $1`, factID,
	).Scan(&tombstone); err != nil {
		t.Fatalf("read row %s: %v", factID, err)
	}
	return tombstone
}
