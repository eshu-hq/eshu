// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7929 disposable PostgreSQL 18 proof that the loader's per-run max(completed_at) probe serves from the acceptance-completed covering index and returns exactly the pre-#7547 oracle rows; enrolled in the live-postgres-readiness runner. Upper-bound timing: 7.3s cold including one-time schema bootstrap and the 200k-row fixture, on local postgres:18
//eshu:readiness:env ESHU_POSTGRES_DSN
//eshu:readiness:env-optional ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DISPOSABLE ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// completedIntentsIndexName is the #7929 covering index for the loader's
// per-run max(completed_at) LATERAL probe. It lives in the bootstrap
// migrations, not in the loader SQL, so the probe text cannot name it.
const completedIntentsIndexName = "shared_projection_intents_acceptance_completed_idx"

// seedCompletedIntentsRun seeds one complete full run (active generation,
// both reducer materialization work items succeeded, no pending intent, no
// watermark) carrying n completed intents split across the two traversed
// domains, then ANALYZEs the table so the planner prices the covering index.
// It returns the scope id; sibling ids follow seedLoaderGateRun's key scheme.
func seedCompletedIntentsRun(t *testing.T, suffix, name string, n int) string {
	t.Helper()
	ctx, db := openCompletedIndexLiveDB(t)
	scope := seedLoaderGateRun(t, ctx, db, suffix, loaderGateRun{name: name, completedAt: loaderGateBase})
	key := name + "-" + suffix
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_projection_intents
	  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	   source_run_id, generation_id, payload, created_at, completed_at)
	  SELECT 'bulk-' || $5 || '-' || g,
	         CASE WHEN (g % 2) = 0 THEN 'code_calls' ELSE 'inheritance_edges' END,
	         'part-' || (g % 64),
	         $1, $2, $2, $3, $4, '{}'::jsonb,
	         $6::timestamptz - interval '3 hours' + ((g % 7200) || ' seconds')::interval,
	         $6::timestamptz - interval '2 hours' + ((g % 3600) || ' seconds')::interval
	  FROM generate_series(1, $7) AS g`,
		"scope-"+key, "repo-"+key, "run-"+key, "gen-"+key, key, loaderGateBase, n); err != nil {
		t.Fatalf("seed bulk completed intents: %v", err)
	}
	// VACUUM (not just ANALYZE) so the bulk-loaded pages are all-visible
	// and the planner can price the covering index as an index-only scan,
	// the shape the production corpus holds under autovacuum.
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE shared_projection_intents`); err != nil {
		t.Fatalf("vacuum analyze intents: %v", err)
	}
	return scope
}

// openCompletedIndexLiveDB opens the disposable PostgreSQL 18 database this
// proof enrolls in the live-postgres-readiness runner under, skipping
// without the runner's DSN pair (same convention as the edges-scope proof):
//
//	ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/code/reachability/ -run TestCodeReachabilityLoaderUsesCompletedIntentsIndex
func openCompletedIndexLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DSN")
	optIn := os.Getenv("ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DISPOSABLE")
	if dsn == "" || optIn != "1" {
		t.Skip("ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DSN/ESHU_REACHABILITY_COMPLETED_INDEX_PROOF_DISPOSABLE not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	return ctx, db
}

// TestCodeReachabilityLoaderUsesCompletedIntentsIndex is the #7929 live
// proof: on a run with thousands of completed intents the shipped candidate
// statement serves the per-run max(completed_at) probe from the covering
// index instead of heap-walking every completed intent, and the returned
// rows still equal the pre-#7547 oracle exactly.
func TestCodeReachabilityLoaderUsesCompletedIntentsIndex(t *testing.T) {
	ctx, db := openCompletedIndexLiveDB(t)
	suffix := routeLivenessTestSuffix(t)
	epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch

	// 200,000 completed intents on one run: at a few thousand rows the
	// planner legitimately serves the probe from the old domain-first
	// bitmap, so the fixture holds a retention-scale history pile where
	// the covering index is the cheaper path, as on the #7929 corpus.
	scope := seedCompletedIntentsRun(t, suffix, "bulk", 200000)
	scopes := map[string]bool{scope: true}

	var valid, ready bool
	err := db.QueryRowContext(ctx, `
SELECT i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
WHERE c.relname = $1`, completedIntentsIndexName).Scan(&valid, &ready)
	if err != nil {
		t.Fatalf("index %s missing: %v (run the #7929 bootstrap migration)", completedIntentsIndexName, err)
	}
	if !valid || !ready {
		t.Fatalf("index %s valid=%t ready=%t, want true/true", completedIntentsIndexName, valid, ready)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Staged planner: check index servability, not natural planner choice
	// (proven separately at corpus scale). A bitmap alternative can otherwise
	// hide the covering path when old snapshots prevent all-visible pages.
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable sequential scan for plan proof: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_bitmapscan = off`); err != nil {
		t.Fatalf("disable bitmap scan for plan proof: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN `+reachabilitystore.ListPendingCodeReachabilityInputsSQL, 100, epoch)
	if err != nil {
		t.Fatalf("explain candidates: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	if !strings.Contains(plan.String(), completedIntentsIndexName) {
		t.Fatalf("candidate probe did not use %s:\n%s", completedIntentsIndexName, plan.String())
	}

	want := queryLoaderGateRows(t, ctx, db, legacyListPendingCodeReachabilityInputsSQL, 100000, epoch, scopes)
	got := queryLoaderGateRows(t, ctx, db, reachabilitystore.ListPendingCodeReachabilityInputsSQL, 100000, epoch, scopes)
	if len(got) != len(want) {
		t.Fatalf("row count changed under the index: got %d, want %d\ngot %+v\nwant %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i].ScopeID != want[i].ScopeID || got[i].SourceRunID != want[i].SourceRunID ||
			got[i].GenerationID != want[i].GenerationID || got[i].RepositoryID != want[i].RepositoryID ||
			!got[i].CompletedAt.Equal(want[i].CompletedAt) {
			t.Fatalf("row %d changed under the index: got %+v want %+v", i, got[i], want[i])
		}
	}
	if len(got) != 1 || got[0].ScopeID != scope {
		t.Fatalf("want only the bulk run %q, got %+v", scope, got)
	}
}
