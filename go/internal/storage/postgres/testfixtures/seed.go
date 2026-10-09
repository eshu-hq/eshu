// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// SeedScope inserts one active scope with an active generation the scope's
// own superseded generations must never equal, so the candidate query's
// active_generation_id predicate never accidentally excludes a fixture
// row. It merges the equivalent seedScope (activation/fixtures_test.go)
// and seedRetentionSelectionScope
// (generation_retention_selection_fixture_live_test.go) seeders: same SQL,
// same args (#7648).
func SeedScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	activeGeneration := scopeID + "-active"
	steps := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`,
			[]any{scopeID},
		},
		{
			`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), 'active')`,
			[]any{activeGeneration, scopeID},
		},
		{
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			[]any{activeGeneration, scopeID},
		},
	}
	for _, step := range steps {
		if _, err := database.ExecContext(ctx, step.query, step.args...); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
	}
}

// SeedSupersededGeneration inserts one superseded generation. It merges the
// equivalent seedSupersededGeneration (activation/fixtures_test.go) and
// seedRetentionSelectionSupersededGeneration
// (generation_retention_selection_fixture_live_test.go) seeders: same SQL,
// same args (#7648).
func SeedSupersededGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, supersededAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $3)`,
		generationID, scopeID, supersededAt,
	); err != nil {
		t.Fatalf("seed superseded generation %s: %v", generationID, err)
	}
}
