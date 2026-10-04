// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// openRouteLivenessLiveDB and routeLivenessTestSuffix are this file's own
// copies of code_reachability_upgrade_backfill_live_test.go's
// openUpgradeBackfillLiveDB/testSuffix (that file stays in the parent
// postgres package per #6693: testSuffix is also shared by unrelated root
// live tests, so it cannot move here, and Go test-only exports do not cross
// package boundaries). registerRouteLivenessCleanup mirrors that file's
// registerUpgradeBackfillCleanup for the same reason.
func openRouteLivenessLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the code-reachability live proofs")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return ctx, db
}

func routeLivenessTestSuffix(t *testing.T) string {
	return fmt.Sprintf("%s-%d", strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()), time.Now().UnixNano())
}

func registerRouteLivenessCleanup(t *testing.T, db *sql.DB, scopeID, repoID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Child-first so the cleanup is correct even if a table's FK is not
		// ON DELETE CASCADE; ingestion_scopes last cascades any stragglers.
		stmts := []struct {
			q    string
			args []any
		}{
			{`DELETE FROM code_root_verdicts WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM code_reachability_rows WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM code_reachability_repository_watermarks WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM shared_projection_intents WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM shared_projection_acceptance WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM fact_work_items WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM content_entities WHERE repo_id=$1`, []any{repoID}},
			{`DELETE FROM scope_generations WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM ingestion_scopes WHERE scope_id=$1`, []any{scopeID}},
		}
		for _, s := range stmts {
			if _, err := db.ExecContext(ctx, s.q, s.args...); err != nil {
				t.Logf("cleanup %q: %v", s.q, err)
			}
		}
	})
}
