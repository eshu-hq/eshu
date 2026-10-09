// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// seedRetentionSelectionLiveWork gives generationID one running fact_work_items
// row, which the candidate query's live_work CTE must exclude it for.
func seedRetentionSelectionLiveWork(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'domain', 'running', now(), now())`,
		generationID+"-work", scopeID, generationID,
	); err != nil {
		t.Fatalf("seed live work for %s: %v", generationID, err)
	}
}

// retentionSelectionPolicy is a small GenerationRetentionPolicy for the live
// selection proofs: callers vary MinSupersededGenerations, MaxSupersededAge
// and BatchGenerationLimit per fixture; BatchRowLimit stays generous so no
// fixture in this file exercises the row-limit skip (that is section 2's
// proof, not this file's).
func retentionSelectionPolicy(minSuperseded int, maxAge time.Duration, batchLimit int) GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: minSuperseded,
		MaxSupersededAge:         maxAge,
		BatchGenerationLimit:     batchLimit,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7334-selection-live",
	}
}

// remainingScopeGenerations reports which of ids still exist in
// scope_generations, so a test can confirm a candidate that must survive a
// pass is untouched and one that must be pruned is gone.
func remainingScopeGenerations(t *testing.T, ctx context.Context, database *sql.DB, ids []string) map[string]bool {
	t.Helper()
	remaining := make(map[string]bool, len(ids))
	rows, err := database.QueryContext(ctx, `SELECT generation_id FROM scope_generations WHERE generation_id = ANY($1::text[])`, ids)
	if err != nil {
		t.Fatalf("query remaining generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan remaining generation: %v", err)
		}
		remaining[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query remaining generations: %v", err)
	}
	return remaining
}
