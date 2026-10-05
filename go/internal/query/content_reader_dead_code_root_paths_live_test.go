// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestCrossRepoDeadCodeConsumerRootPathsLive proves the test-only-consumers path
// read (#7603) on PostgreSQL 18 with the full bootstrap schema: it returns one
// path per named root, leaves out a root with no row and a root in a repository
// the request did not name, and plans as an index scan on content_entities'
// key (its primary key or its (repo_id, entity_id) index, as on the QA replica)
// against a table too large for a sequential scan to win.
//
// Run with a disposable PostgreSQL 18 administrative database, the same
// variables as TestDeadCodeIncomingEntityIDsActiveRunBoundLive.
func TestCrossRepoDeadCodeConsumerRootPathsLive(t *testing.T) {
	dsn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN")
	optIn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	// 30,000 filler entities (long source_cache, as in a real table), a fifth of
	// them in the consumer repository itself, so the planner prices a keyed
	// lookup against a real repository rather than a two-row one.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO content_entities(entity_id, repo_id, relative_path, entity_type, entity_name,
			start_line, end_line, source_cache, indexed_at)
		SELECT 'filler-' || n, CASE WHEN n % 5 = 0 THEN 'repo-consumer' ELSE 'repo-filler-' || (n % 50) END, 'src/f' || n || '.go', 'Function', 'f' || n,
			1, 2, repeat('x', 400), now()
		FROM generate_series(1, 30000) AS n`); err != nil {
		t.Fatalf("seed filler: %v", err)
	}
	seed := func(entityID, repoID, path string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO content_entities(entity_id, repo_id, relative_path, entity_type, entity_name,
				start_line, end_line, source_cache, indexed_at)
			VALUES ($1, $2, $3, 'Function', 'f', 1, 2, 'func f() {}', now())`, entityID, repoID, path); err != nil {
			t.Fatalf("seed %s: %v", entityID, err)
		}
	}
	seed("root-test", "repo-consumer", "pkg/charge_test.go")
	seed("root-prod", "repo-consumer", "pkg/charge.go")
	seed("root-other-repo", "repo-unrelated", "pkg/other_test.go")
	if _, err := db.ExecContext(ctx, `ANALYZE content_entities`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	got, err := NewContentReader(db).CrossRepoDeadCodeConsumerRootPaths(ctx,
		[]string{"root-test", "root-prod", "root-other-repo", "root-missing"}, []string{"repo-consumer"})
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerRootPaths() error = %v", err)
	}
	if len(got) != 2 || got["root-test"] != "pkg/charge_test.go" || got["root-prod"] != "pkg/charge.go" {
		t.Fatalf("paths = %#v, want only root-test and root-prod", got)
	}

	plan := explainRootPaths(ctx, t, db, []string{"root-test", "root-prod", "root-other-repo", "root-missing"})
	t.Logf("plan:\n%s", plan)
	keyed := strings.Contains(plan, "content_entities_pkey") || strings.Contains(plan, "content_entities_repo_entity_idx")
	if !keyed || !strings.Contains(plan, "entity_id = ANY") || strings.Contains(plan, "Seq Scan") ||
		strings.Contains(plan, "Bitmap") {
		t.Fatalf("plan is not a keyed entity_id lookup:\n%s", plan)
	}
}

func explainRootPaths(ctx context.Context, t *testing.T, db *sql.DB, rootIDs []string) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN "+deadcode.CrossRepoDeadCodeConsumerRootPathsQuery,
		array.Of(rootIDs), array.Of([]string{"repo-consumer"}))
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	return plan.String()
}
