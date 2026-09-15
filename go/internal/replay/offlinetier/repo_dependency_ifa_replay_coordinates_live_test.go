// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func seedRepoDependencyReplayCoordinates(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	rows []reducer.SharedProjectionIntentRow,
) {
	t.Helper()
	now := time.Now().UTC()
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `WITH seeded_scope AS (INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status) VALUES ($1, 'git_repository', 'git', $1, 'git', $1, $2, $2, 'active') ON CONFLICT (scope_id) DO UPDATE SET scope_id = EXCLUDED.scope_id RETURNING scope_id) INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status) SELECT $3, scope_id, 'full', $2, $2, 'active' FROM seeded_scope ON CONFLICT (generation_id) DO NOTHING`, row.ScopeID, now, row.GenerationID); err != nil {
			t.Fatalf("seed workload replay generation %q: %v", row.GenerationID, err)
		}
	}
}
