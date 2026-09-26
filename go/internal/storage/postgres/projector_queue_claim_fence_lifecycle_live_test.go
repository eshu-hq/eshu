// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// fenceRow reads one scope's fence value; ok is false when the row is missing.
func fenceRow(t *testing.T, database *sql.DB, scopeID string) (int64, bool) {
	t.Helper()
	var fence int64
	err := database.QueryRow(`SELECT fence FROM projector_scope_claim_fences WHERE scope_id = $1`, scopeID).Scan(&fence)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read fence row %s: %v", scopeID, err)
	}
	return fence, true
}

// upsertProofScope runs the ingestion upsert for one git scope.
func upsertProofScope(t *testing.T, database *sql.DB, scopeID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := database.Exec(upsertIngestionScopeQuery,
		scopeID, "repository", "git", scopeID, nil, "git", scopeID, now, now, "active", nil, "{}",
	); err != nil {
		t.Fatalf("upsert scope %s: %v", scopeID, err)
	}
}

// TestProjectorClaimFenceRowLifecycle proves the trigger keeps one fence row
// per scope across the paths that create and remove scopes (#7115): the
// ingestion upsert creates it, re-upserting an existing scope leaves the fence
// untouched, a direct INSERT (the test and migration path) creates it and the
// scope's enqueued work is claimable, and deleting the scope removes it.
func TestProjectorClaimFenceRowLifecycle(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)

	upsertProofScope(t, database, "scope-u")
	if fence, ok := fenceRow(t, database, "scope-u"); !ok || fence != 0 {
		t.Fatalf("fence row after upsert insert = (%d, %v), want (0, true)", fence, ok)
	}
	if _, err := database.Exec(`UPDATE projector_scope_claim_fences SET fence = 7 WHERE scope_id = 'scope-u'`); err != nil {
		t.Fatalf("set fence: %v", err)
	}
	upsertProofScope(t, database, "scope-u")
	if fence, ok := fenceRow(t, database, "scope-u"); !ok || fence != 7 {
		t.Fatalf("fence row after re-upsert = (%d, %v), want (7, true) untouched", fence, ok)
	}

	seedClaimMaintenanceScopes(t, database, "scope-d")
	if _, ok := fenceRow(t, database, "scope-d"); !ok {
		t.Fatal("direct INSERT INTO ingestion_scopes left no fence row")
	}
	if _, err := database.Exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-d', 'scope-d', 'push', now(), now(), 'pending')`); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if err := queue.Enqueue(context.Background(), "scope-d", "gen-d"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	work, ok, err := queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-d" {
		t.Fatalf("Claim() = (%q, %v, %v), want the enqueued gen-d", work.Generation.GenerationID, ok, err)
	}
	if fence, _ := fenceRow(t, database, "scope-d"); fence != 1 {
		t.Fatalf("scope-d fence after one claim = %d, want 1", fence)
	}

	if _, err := database.Exec(`DELETE FROM ingestion_scopes WHERE scope_id = 'scope-u'`); err != nil {
		t.Fatalf("delete scope: %v", err)
	}
	if _, ok := fenceRow(t, database, "scope-u"); ok {
		t.Fatal("deleting the scope left its fence row")
	}
}

// TestProjectorClaimSkipsScopeWithoutFenceRow pins what a missing fence row
// does: the scope is unclaimable, never claimed without a fence. The claim
// takes the other scope's work and then returns nothing while scope-m's
// pending row waits, and the missing-fence gauge query counts the scope.
func TestProjectorClaimSkipsScopeWithoutFenceRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	seedClaimMaintenanceScopes(t, database, "scope-m", "scope-o")
	seedClaimMaintenanceWork(t, database, "scope-m", "gen-m", "pending", "pending", 2*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-o", "gen-o", "pending", "pending", time.Hour, nil)
	if _, err := database.Exec(`DELETE FROM projector_scope_claim_fences WHERE scope_id = 'scope-m'`); err != nil {
		t.Fatalf("delete fence row: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	var claimed []string
	for range 3 {
		work, ok, err := queue.Claim(context.Background())
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if ok {
			claimed = append(claimed, work.Generation.GenerationID)
		}
	}
	if len(claimed) != 1 || claimed[0] != "gen-o" {
		t.Fatalf("claims = %v, want only gen-o while scope-m has no fence row", claimed)
	}
	if status, _, _ := workState(t, database, "scope-m", "gen-m"); status != "pending" {
		t.Fatalf("gen-m status = %q, want pending (unclaimable, not claimed)", status)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	missing, err := observer.ProjectorScopesMissingClaimFence(context.Background())
	if err != nil || missing != 1 {
		t.Fatalf("ProjectorScopesMissingClaimFence() = (%d, %v), want 1", missing, err)
	}
}

// TestProjectorScopeClaimFenceMigrationBackfills applies the bootstrap
// definitions before migration 130, seeds scopes the way a pre-#7115 install
// has them, then applies 130: every scope gets a fence row at 0, and applying
// it again changes nothing, including a fence a claim already bumped.
func TestProjectorScopeClaimFenceMigrationBackfills(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	schemaName := fmt.Sprintf("claim_fence_backfill_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = admin.Close()
	})
	database, err := sql.Open("pgx", withDSNParam(dsn, "search_path="+schemaName))
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	database.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = database.Close() })

	defs := BootstrapDefinitionsWithoutContentSearchIndexes()
	fenceIndex := -1
	for i, def := range defs {
		if def.Name == "projector_scope_claim_fences" {
			fenceIndex = i
		}
	}
	if fenceIndex < 0 {
		t.Fatal("bootstrap definitions have no projector_scope_claim_fences migration")
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: database}, defs[:fenceIndex]); err != nil {
		t.Fatalf("apply definitions before 130: %v", err)
	}
	seedClaimMaintenanceScopes(t, database, "scope-a", "scope-b", "scope-c")

	fence := defs[fenceIndex : fenceIndex+1]
	if err := ApplyDefinitions(ctx, SQLDB{DB: database}, fence); err != nil {
		t.Fatalf("apply 130: %v", err)
	}
	for _, scopeID := range []string{"scope-a", "scope-b", "scope-c"} {
		if value, ok := fenceRow(t, database, scopeID); !ok || value != 0 {
			t.Fatalf("%s fence after backfill = (%d, %v), want (0, true)", scopeID, value, ok)
		}
	}
	if _, err := database.Exec(`UPDATE projector_scope_claim_fences SET fence = 3 WHERE scope_id = 'scope-b'`); err != nil {
		t.Fatalf("bump fence: %v", err)
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: database}, fence); err != nil {
		t.Fatalf("reapply 130: %v", err)
	}
	// Earlier migrations seed scopes of their own (115's global value-flow
	// scope), so compare against every scope, not only the three seeded here.
	var fences, scopes int
	if err := database.QueryRow(`
SELECT (SELECT count(*) FROM projector_scope_claim_fences), (SELECT count(*) FROM ingestion_scopes)
`).Scan(&fences, &scopes); err != nil {
		t.Fatalf("count fence rows: %v", err)
	}
	if value, _ := fenceRow(t, database, "scope-b"); fences != scopes || value != 3 {
		t.Fatalf("after rerun: %d fence rows for %d scopes, scope-b fence %d; want one row per scope and the bumped fence 3 kept",
			fences, scopes, value)
	}
	seedClaimMaintenanceScopes(t, database, "scope-d")
	if _, ok := fenceRow(t, database, "scope-d"); !ok {
		t.Fatal("scope inserted after the migration has no fence row; the trigger is missing")
	}
}
