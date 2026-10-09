// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Live joint scope-selector proof (#7732): seed two scopes where scope B's
// source key equals scope A's scope id, and prove both admin routes fail
// closed with 409 instead of silently acting on one scope (reopen, lowest
// id) or across both scopes (skip, no scope bound). Uses the same
// disposable-database harness as TestAdminHandler_ReopenLive; without a DSN
// it skips. Run it locally with, for example:
//
//	ESHU_ADMIN_REOPEN_PROOF_DSN=postgres://eshu:eshu@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE=1 \
//	go test ./internal/query/admin/ -run TestAdminHandler_ScopeSelectorCollisionLive -count=1
func TestAdminHandler_ScopeSelectorCollisionLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_ADMIN_REOPEN_PROOF_DSN"),
		os.Getenv("ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := pgstatus.ApplyBootstrapWithoutContentSearchIndexes(ctx, pgstatus.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	suffix := time.Now().UTC().UnixNano()
	scopeA := fmt.Sprintf("scope-collide-a-%d", suffix)
	scopeB := fmt.Sprintf("scope-collide-b-%d", suffix)
	// The collision: B's source key is A's scope id.
	sourceKeyB := scopeA
	generationA := fmt.Sprintf("generation-collide-a-%d", suffix)
	pendingA := fmt.Sprintf("w-collide-pending-a-%d", suffix)
	pendingB := fmt.Sprintf("w-collide-pending-b-%d", suffix)
	succeededA := fmt.Sprintf("w-collide-succeeded-a-%d", suffix)

	seedScopeCollisionFixture(t, ctx, db, scopeA, scopeB, sourceKeyB, generationA, pendingA, pendingB, succeededA)

	h := &admin.Handler{Store: store.NewStore(db), Audit: &testutil.FakeGovernanceAuditAppender{}}
	mux := testutil.MountAdminHandler(h)

	// Skip with the colliding selector: must 409, and both pending rows
	// must stay pending.
	w := testutil.PostJSON(mux, "/api/v0/admin/skip", map[string]any{
		"repository_id": scopeA,
		"operator_note": "collision proof",
	})
	if w.Code != http.StatusConflict {
		t.Errorf("skip colliding selector status = %d, want 409; body: %s; row A status = %s, row B status = %s",
			w.Code, w.Body.String(),
			collisionRowStatus(t, ctx, db, pendingA), collisionRowStatus(t, ctx, db, pendingB))
	} else {
		if got := collisionRowStatus(t, ctx, db, pendingA); got != "pending" {
			t.Errorf("scope A pending row status = %q, want pending (skip must not act on ambiguity)", got)
		}
		if got := collisionRowStatus(t, ctx, db, pendingB); got != "pending" {
			t.Errorf("scope B pending row status = %q, want pending (skip must not act on ambiguity)", got)
		}
	}

	// Reopen with the colliding selector: must 409, and the succeeded row
	// must stay succeeded.
	w = testutil.PostJSON(mux, "/api/v0/admin/reopen", map[string]any{
		"domain":          "workload_materialization",
		"scope_id":        scopeA,
		"reason":          "collision proof: must refuse, not pick the lowest scope id",
		"idempotency_key": fmt.Sprintf("collide-live-%d", suffix),
	})
	if w.Code != http.StatusConflict {
		t.Errorf("reopen colliding selector status = %d, want 409; body: %s; succeeded row status = %s",
			w.Code, w.Body.String(), collisionRowStatus(t, ctx, db, succeededA))
	} else if got := collisionRowStatus(t, ctx, db, succeededA); got != "succeeded" {
		t.Errorf("succeeded row status = %q, want succeeded (reopen must not act on ambiguity)", got)
	}
}

func seedScopeCollisionFixture(t *testing.T, ctx context.Context, db *sql.DB, scopeA, scopeB, sourceKeyB, generationA, pendingA, pendingB, succeededA string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)
	for _, scope := range []struct{ id, key string }{
		{scopeA, fmt.Sprintf("key-collide-a-%d", now.UnixNano())},
		{scopeB, sourceKeyB},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $2, 'git', $2, $3, $3, 'active', '{}'::jsonb)
`, scope.id, scope.key, now); err != nil {
			t.Fatalf("insert scope %s: %v", scope.id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'test', $3, $3, 'active', '{}'::jsonb)
`, generationA, scopeA, now); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeA, generationA); err != nil {
		t.Fatalf("pin active generation: %v", err)
	}
	for _, row := range []struct{ id, scope, status string }{
		{pendingA, scopeA, "pending"},
		{pendingB, scopeB, "pending"},
		{succeededA, scopeA, "succeeded"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, created_at, updated_at, payload
) VALUES ($1, $2, $3, 'reducer', 'workload_materialization', $4, 1, $5, $5, '{}'::jsonb)
`, row.id, row.scope, generationA, row.status, past); err != nil {
			t.Fatalf("insert row %s: %v", row.id, err)
		}
	}
}

func collisionRowStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read row %s status: %v", workItemID, err)
	}
	return status
}
