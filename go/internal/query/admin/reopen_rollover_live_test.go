// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Live rollover-currency proof (#7734): roll the scope's active generation
// from A to B, then reopen. The reopen must act on B — the generation that
// is active when its transaction locks the scope — reopening B's succeeded
// row and leaving A's row untouched. The deterministic interleave (rollover
// landing between the pre-transaction resolve and the commit) is pinned by
// TestReopenCompletedWorkSeesRolloverBetweenResolveAndCommit; this test
// proves the fence statements run against the real schema with a moved
// pointer. Uses the disposable-database harness; without a DSN it skips.
func TestAdminHandler_ReopenRolloverActsOnCurrentGenerationLive(t *testing.T) {
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
	scope := fmt.Sprintf("scope-rollover-%d", suffix)
	generationA := fmt.Sprintf("generation-rollover-a-%d", suffix)
	generationB := fmt.Sprintf("generation-rollover-b-%d", suffix)
	succeededA := fmt.Sprintf("w-rollover-a-%d", suffix)
	succeededB := fmt.Sprintf("w-rollover-b-%d", suffix)

	seedRolloverFixture(t, ctx, db, scope, generationA, generationB, succeededA, succeededB)

	h := &admin.Handler{Store: store.NewStore(db), Audit: &testutil.FakeGovernanceAuditAppender{}}
	mux := testutil.MountAdminHandler(h)

	w := testutil.PostJSON(mux, "/api/v0/admin/reopen", map[string]any{
		"domain":          "workload_materialization",
		"scope_id":        scope,
		"reason":          "rollover proof: must repair the current generation",
		"idempotency_key": fmt.Sprintf("rollover-live-%d", suffix),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reopen after rollover status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		GenerationID         string `json:"generation_id"`
		ReopenedReducerCount int    `json:"reopened_reducer_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode reopen body: %v: %s", err, w.Body.String())
	}
	if body.GenerationID != generationB {
		t.Fatalf("reopened generation = %q, want %q (the current active generation)", body.GenerationID, generationB)
	}
	if body.ReopenedReducerCount != 1 {
		t.Fatalf("reopened_reducer_count = %d, want 1", body.ReopenedReducerCount)
	}
	if got := rolloverRowStatus(t, ctx, db, succeededB); got != "pending" {
		t.Fatalf("generation B row status = %q, want pending (reopened)", got)
	}
	if got := rolloverRowStatus(t, ctx, db, succeededA); got != "succeeded" {
		t.Fatalf("generation A row status = %q, want succeeded (untouched old generation)", got)
	}
}

// seedRolloverFixture seeds a scope on generation A with one succeeded row,
// then rolls the scope to generation B the way activation does: insert the
// new generation, move the active-generation pointer, mark A superseded,
// and leave one succeeded row on B.
func seedRolloverFixture(t *testing.T, ctx context.Context, db *sql.DB, scope, generationA, generationB, succeededA, succeededB string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)
`, scope, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	insertGeneration := func(generationID string, observedAt time.Time, status string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'test', $3, $3, $4, '{}'::jsonb)
`, generationID, scope, observedAt, status); err != nil {
			t.Fatalf("insert generation %s: %v", generationID, err)
		}
	}
	insertRow := func(workItemID, generationID string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, created_at, updated_at, payload
) VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'succeeded', 1, $4, $4, '{}'::jsonb)
`, workItemID, scope, generationID, past); err != nil {
			t.Fatalf("insert row %s: %v", workItemID, err)
		}
	}
	insertGeneration(generationA, past, "active")
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generationA); err != nil {
		t.Fatalf("pin generation A: %v", err)
	}
	insertRow(succeededA, generationA)
	// The rollover: A is superseded first (one active generation per
	// scope), then B becomes active and the pointer moves to it.
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`, generationA); err != nil {
		t.Fatalf("supersede generation A: %v", err)
	}
	insertGeneration(generationB, now, "active")
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generationB); err != nil {
		t.Fatalf("move pointer to B: %v", err)
	}
	insertRow(succeededB, generationB)
}

func rolloverRowStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read row %s status: %v", workItemID, err)
	}
	return status
}
