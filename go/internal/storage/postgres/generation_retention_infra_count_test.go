// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionRowCountsIncludeInfraMirrorRows pins that the
// admission count has an infra_resource_entities arm, so a generation's
// prospective mirror-row deletes count against BatchRowLimit like its
// content_entities deletes.
func TestGenerationRetentionRowCountsIncludeInfraMirrorRows(t *testing.T) {
	t.Parallel()

	if !strings.Contains(generationRetentionRowCountsQuery,
		"SELECT candidate.generation_id, 'infra_resource_entities'") {
		t.Fatalf("row-count query has no infra_resource_entities arm:\n%s", generationRetentionRowCountsQuery)
	}
}

// TestGenerationRetentionInfraMirrorCountLive proves the arm counts exactly
// the mirror rows the retention prune will delete: rows of prunable content
// entities only, not rows whose entity a retained generation still holds.
// Set ESHU_GENERATION_RETENTION_PROOF_DSN to run it.
func TestGenerationRetentionInfraMirrorCountLive(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_RETENTION_PROOF_DSN to run the retention mirror-count proof")
	}

	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	schemaName := fmt.Sprintf("retention_infra_count_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	defer func() { _, _ = database.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := database.ExecContext(ctx, generationRetentionProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	// gen-old holds e1, e2, e3; gen-new still holds e3, so only e1 and e2 are
	// prunable. Mirror rows exist for e1 (infra), e3 (infra), and not for e2
	// (a non-infra entity). The count must be 1: e1's mirror row.
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, active_generation_id, scope_kind, source_system, collector_kind, source_key, observed_at
) VALUES ('scope-infra', 'gen-new', 'repository', 'github', 'git', 'example/infra', now());
INSERT INTO scope_generations (generation_id, scope_id, status, observed_at, superseded_at)
VALUES ('gen-old', 'scope-infra', 'superseded', now() - interval '30 days', now() - interval '29 days'),
       ('gen-new', 'scope-infra', 'active', now(), NULL);
INSERT INTO fact_records (fact_id, generation_id, fact_kind, payload) VALUES
    ('f1', 'gen-old', 'content_entity', '{"repo_id":"r1","entity_id":"e1"}'),
    ('f2', 'gen-old', 'content_entity', '{"repo_id":"r1","entity_id":"e2"}'),
    ('f3', 'gen-old', 'content_entity', '{"repo_id":"r1","entity_id":"e3"}'),
    ('f4', 'gen-new', 'content_entity', '{"repo_id":"r1","entity_id":"e3"}');
INSERT INTO content_entities (repo_id, entity_id) VALUES ('r1', 'e1'), ('r1', 'e2'), ('r1', 'e3');
INSERT INTO infra_resource_entities (entity_id, repo_id) VALUES ('e1', 'r1'), ('e3', 'r1');
`); err != nil {
		t.Fatalf("seed proof data: %v", err)
	}

	adapter := SQLDB{DB: database}
	tx, err := adapter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	totals, perGeneration, _, err := NewGenerationRetentionStore(adapter).countRows(ctx, tx, []string{"gen-old"})
	if err != nil {
		t.Fatalf("countRows() error = %v", err)
	}
	if got := perGeneration["gen-old"]["content_entities"]; got != 2 {
		t.Fatalf("content_entities count = %d, want 2 (e1, e2)", got)
	}
	if got := perGeneration["gen-old"]["infra_resource_entities"]; got != 1 {
		t.Fatalf("infra_resource_entities count = %d, want 1 (only e1's mirror row is prunable)", got)
	}
	if got := totals["infra_resource_entities"]; got != 1 {
		t.Fatalf("infra_resource_entities total = %d, want 1", got)
	}
}
