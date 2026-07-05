// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// deferredPartitionMemoProofSchemaSQL extends deferredPartitionProofSchemaSQL
// (the scope/generation/fact tables) with the tables the FULL deferred backfill
// write path touches: relationship_evidence_facts (evidence persistence),
// graph_projection_phase_state (readiness), and the new
// deferred_backfill_partition_memo (issue #3624 Track 1 / B'). This lets the
// tests in this package drive the REAL BackfillAllRelationshipEvidence end to
// end, exercising both the read-side memo gate and the write-side memo commit
// in one pass, not just the fact-load half.
const deferredPartitionMemoProofSchemaSQL = deferredPartitionProofSchemaSQL + `
CREATE TABLE relationship_evidence_facts (
    evidence_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    source_repo_id TEXT,
    target_repo_id TEXT,
    source_entity_id TEXT,
    target_entity_id TEXT,
    confidence DOUBLE PRECISION NOT NULL,
    rationale TEXT NOT NULL,
    details JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE graph_projection_phase_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);

CREATE TABLE deferred_backfill_partition_memo (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    catalog_fingerprint TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
`

// dsnForDeferredPartitionMemoProof reuses the same proof DSN convention as the
// #3710 partition-source proof so one configured Postgres serves both gates.
func dsnForDeferredPartitionMemoProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DSN (or ESHU_LATEST_GENERATION_PROOF_DSN) to run the deferred backfill partition-memo Postgres proof")
	return ""
}

func openDeferredPartitionMemoProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// Single connection, matching the #3710 partition-source proof
	// (openDeferredPartitionProofDB): the proof schema's search_path is set on
	// this one connection below, so every subsequent query — including the
	// deferred backfill's concurrent per-partition fan-out — must reuse that
	// same connection to see the proof schema rather than "public".
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func provisionDeferredPartitionMemoSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("deferred_partition_memo_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, deferredPartitionMemoProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	return schemaName
}

// memoProofFixture is one scope/generation/repository triple the tests below
// seed and drive through the real backfill.
type memoProofFixture struct {
	scopeID  string
	genID    string
	repoID   string
	repoName string
}

func seedMemoProofScopesAndFacts(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixtures []memoProofFixture,
	crossRefs map[string]string, // sourceRepoID -> target repo alias referenced in content
	base time.Time,
) {
	t.Helper()

	for _, fx := range fixtures {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL) ON CONFLICT DO NOTHING",
			fx.scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", fx.scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
			fx.genID, fx.scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", fx.genID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)
ON CONFLICT DO NOTHING`,
			"repo-fact-"+fx.repoID, fx.scopeID, fx.genID, base,
			fmt.Sprintf(`{"repo_id":%q,"name":%q}`, fx.repoID, fx.repoName)); err != nil {
			t.Fatalf("seed repository fact for %q: %v", fx.repoID, err)
		}
	}

	for sourceRepoID, targetAlias := range crossRefs {
		var scopeID, genID string
		for _, fx := range fixtures {
			if fx.repoID == sourceRepoID {
				scopeID, genID = fx.scopeID, fx.genID
			}
		}
		if scopeID == "" {
			t.Fatalf("no fixture for source repo %q", sourceRepoID)
		}
		factID := "content-" + sourceRepoID
		content := fmt.Sprintf(`{"repo_id":%q,"artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"%s\""}`,
			sourceRepoID, targetAlias)
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)
ON CONFLICT DO NOTHING`,
			factID, scopeID, genID, base, content); err != nil {
			t.Fatalf("seed content fact for %q: %v", sourceRepoID, err)
		}
	}
}

func countMemoRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM deferred_backfill_partition_memo").Scan(&count); err != nil {
		t.Fatalf("count memo rows: %v", err)
	}
	return count
}

func countEvidenceRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM relationship_evidence_facts").Scan(&count); err != nil {
		t.Fatalf("count evidence rows: %v", err)
	}
	return count
}

func evidenceEdgeSet(t *testing.T, ctx context.Context, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT source_repo_id, target_repo_id FROM relationship_evidence_facts")
	if err != nil {
		t.Fatalf("query evidence edges: %v", err)
	}
	defer func() { _ = rows.Close() }()
	edges := make(map[string]bool)
	for rows.Next() {
		var source, target string
		if err := rows.Scan(&source, &target); err != nil {
			t.Fatalf("scan evidence edge: %v", err)
		}
		edges[source+"->"+target] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate evidence edges: %v", err)
	}
	return edges
}
