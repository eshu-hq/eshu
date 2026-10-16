// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

const deferredPartitionMemoProofSchemaSQL = testfixtures.DeferredPartitionMemoProofSchemaSQL

func openDeferredPartitionMemoProofDB(t *testing.T, dsn string) *sql.DB {
	return testfixtures.OpenDeferredPartitionMemoProofDB(t, dsn)
}

func provisionDeferredPartitionMemoSchema(t *testing.T, db *sql.DB) string {
	return testfixtures.ProvisionDeferredPartitionMemoSchema(t, db)
}

// memoProofFixture is one scope/generation/repository triple the tests below
// seed and drive through the real backfill.
type memoProofFixture struct {
	scopeID  string
	genID    string
	repoID   string
	repoName string
}

func seedMemoProofScopesAndFacts(t *testing.T, ctx context.Context, db *sql.DB,
	fixtures []memoProofFixture, crossRefs map[string]string, base time.Time,
) {
	shared := make([]testfixtures.MemoProofFixture, len(fixtures))
	for i, fixture := range fixtures {
		shared[i] = testfixtures.MemoProofFixture{
			ScopeID: fixture.scopeID, GenID: fixture.genID,
			RepoID: fixture.repoID, RepoName: fixture.repoName,
		}
	}
	testfixtures.SeedMemoProofScopesAndFacts(t, ctx, db, shared, crossRefs, base)
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
