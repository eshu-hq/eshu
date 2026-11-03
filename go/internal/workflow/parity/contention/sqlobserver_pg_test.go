// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openObserverPG opens a disposable database with the workflow schema on
// the server PARITY_INTREE_DSN names (opt-in PARITY_INTREE_DISPOSABLE=1),
// the same gate as the parity suite. These tests are the seeded real-Postgres
// RED cases for every SQLObserver detector and for the prefix-only delete.
func openObserverPG(t *testing.T) (context.Context, *sql.DB, *SQLObserver) {
	t.Helper()
	admin := os.Getenv(envInTreeDSN)
	if admin == "" {
		t.Skip(envInTreeDSN + " is not set; skipping the real-Postgres observer tests")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(t, admin, os.Getenv(envInTreeOptIn), 2*time.Minute)
	if err := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db}).EnsureSchema(ctx); err != nil {
		t.Fatalf("workflow schema: %v", err)
	}
	obs, err := NewSQLObserver(ctx, db)
	if err != nil {
		t.Fatalf("observer: %v", err)
	}
	return ctx, db, obs
}

func pgExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func pgRun(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	pgExec(t, ctx, db, `INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ($1, 'schedule', 'collection_active', now(), now())`, id)
}

func pgItem(t *testing.T, ctx context.Context, db *sql.DB, id, run, scopeID, status string, token int64) {
	pgExec(t, ctx, db, `INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id,
    scope_id, generation_id, status, current_fencing_token, created_at, updated_at)
VALUES ($1, $2, 'terraform_state', $3, $4, 'gen', $5, $6, now(), now())`, id, run, InstanceID, scopeID, status, token)
}

func pgClaim(t *testing.T, ctx context.Context, db *sql.DB, id, item, status string, token int64) {
	pgExec(t, ctx, db, `INSERT INTO workflow_claims (claim_id, work_item_id, fencing_token, owner_id, status,
    claimed_at, heartbeat_at, lease_expires_at, created_at, updated_at)
VALUES ($1, $2, $3, 'owner', $4, now(), now(), now() + interval '1 minute', now(), now())`, id, item, token, status)
}

// TestSQLObserverDetectorsOnPostgres seeds one violation per detector next
// to a clean item, and requires each detector to name exactly its item.
func TestSQLObserverDetectorsOnPostgres(t *testing.T) {
	ctx, db, obs := openObserverPG(t)
	run := Prefix + "run-7-0"
	pgRun(t, ctx, db, run)
	clean, dual, regressed, twice := Prefix+"wi-7-clean", Prefix+"wi-7-dual", Prefix+"wi-7-regressed", Prefix+"wi-7-twice"
	pgItem(t, ctx, db, clean, run, Prefix+"scope-0", "claimed", 1)
	pgClaim(t, ctx, db, Prefix+"claim-clean", clean, "active", 1)
	assertDetectors(t, ctx, obs, nil, nil, nil)

	pgItem(t, ctx, db, dual, run, Prefix+"scope-0", "claimed", 2)
	pgClaim(t, ctx, db, Prefix+"claim-dual-1", dual, "active", 1)
	pgClaim(t, ctx, db, Prefix+"claim-dual-2", dual, "active", 2)
	pgItem(t, ctx, db, regressed, run, Prefix+"scope-0", "claimed", 1)
	pgClaim(t, ctx, db, Prefix+"claim-regressed", regressed, "expired", 3)
	pgItem(t, ctx, db, twice, run, Prefix+"scope-0", "completed", 2)
	pgClaim(t, ctx, db, Prefix+"claim-twice-1", twice, "completed", 1)
	pgClaim(t, ctx, db, Prefix+"claim-twice-2", twice, "completed", 2)
	assertDetectors(t, ctx, obs, []string{dual}, []string{regressed}, []string{twice})
}

func assertDetectors(t *testing.T, ctx context.Context, obs *SQLObserver, dual, fencing, twice []string) {
	t.Helper()
	snap, err := obs.Snapshot(ctx)
	if err != nil || !slices.Equal(snap.DualLive, dual) {
		t.Fatalf("dual live = %v (err %v), want %v", snap.DualLive, err, dual)
	}
	bad, err := obs.FencingViolations(ctx)
	if err != nil || !slices.Equal(bad, fencing) {
		t.Fatalf("fencing = %v (err %v), want %v", bad, err, fencing)
	}
	got, err := obs.DoubleTerminal(ctx)
	if err != nil || !slices.Equal(got, twice) {
		t.Fatalf("double terminal = %v (err %v), want %v", got, err, twice)
	}
}

// TestSQLObserverDeletesOnlyThisRunsRows: cleanup removes this seed's runs,
// items, and claims, and the harness quota scopes, and nothing else. The
// deployed synthetic instance's rows (scope synth-8875-rehearsal-tfstate,
// which carries the bare prefix), another seed's rows, and real rows stay.
func TestSQLObserverDeletesOnlyThisRunsRows(t *testing.T) {
	ctx, db, obs := openObserverPG(t)
	mine, other := Prefix+"run-7-0", Prefix+"run-8-0"
	deployedScope := Prefix + "tfstate"
	for _, run := range []string{mine, other, "run-real", "synth-8875-tfstate-run-1"} {
		pgRun(t, ctx, db, run)
	}
	for n, id := range []string{Prefix + "wi-7-0", Prefix + "wi-7-1", Prefix + "wi-7-2"} {
		pgItem(t, ctx, db, id, mine, Prefix+"scope-0", "claimed", 1)
		pgClaim(t, ctx, db, Prefix+"claim-7-"+string(rune('a'+n)), id, "active", 1)
	}
	pgItem(t, ctx, db, Prefix+"wi-8-0", other, Prefix+"scope-0", "pending", 0)
	pgItem(t, ctx, db, "wi-real", "run-real", "scope-real", "pending", 0)
	pgItem(t, ctx, db, "synth-8875-tfstate-wi-1", "synth-8875-tfstate-run-1", deployedScope, "pending", 0)
	for _, scopeID := range []string{Prefix + "scope-quota-at", deployedScope} {
		pgExec(t, ctx, db, `INSERT INTO component_quota_windows (component, scope_id, window_start, grants, updated_at)
VALUES ('package_registry', $1, date_trunc('hour', now()), 3, now())`, scopeID)
	}
	before, err := obs.Census(ctx)
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if got := before.Other["component_quota_windows|package_registry"]; got != 1 {
		t.Fatalf("deployed-instance quota row counted as harness: other=%d, want 1", got)
	}
	deleted, err := obs.DeletePrefixed(ctx, 7, 2)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted["workflow_work_items"] != 3 || deleted["workflow_claims"] != 3 || deleted["workflow_runs"] != 1 ||
		deleted["component_quota_windows"] != 1 {
		t.Fatalf("deleted %v, want 3 items, 3 claims, 1 run, 1 quota row", deleted)
	}
	rows, err := db.QueryContext(ctx, `SELECT work_item_id FROM workflow_work_items
UNION ALL SELECT run_id FROM workflow_runs UNION ALL SELECT scope_id FROM component_quota_windows ORDER BY 1`)
	if err != nil {
		t.Fatalf("left rows: %v", err)
	}
	left, err := scanStrings(rows)
	if err != nil {
		t.Fatalf("left rows: %v", err)
	}
	for _, keep := range []string{
		Prefix + "wi-8-0", other, "wi-real", "run-real", "synth-8875-tfstate-wi-1",
		"synth-8875-tfstate-run-1", deployedScope,
	} {
		if !slices.Contains(left, keep) {
			t.Fatalf("cleanup deleted %q it did not create; left %v", keep, left)
		}
	}
	if len(left) != 7 {
		t.Fatalf("left %d rows %v, want the 7 rows this run did not create", len(left), left)
	}
}

// TestInTreeServerSafety: the in-tree leg refuses a server that holds the
// core or satellite database, or that is the satellite's own server.
func TestInTreeServerSafety(t *testing.T) {
	admin := os.Getenv(envInTreeDSN)
	if admin == "" {
		t.Skip(envInTreeDSN + " is not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := inTreeServerSafe(ctx, db, admin, ""); err != nil {
		t.Fatalf("disposable server refused: %v", err)
	}
	sameServer, err := postgresproof.WithDatabase(admin, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if err := inTreeServerSafe(ctx, db, admin, sameServer); err == nil {
		t.Fatalf("accepted an in-tree server that is the satellite's own server")
	}
	pgExec(t, ctx, db, "CREATE DATABASE workflow_claims")
	defer pgExec(t, ctx, db, "DROP DATABASE workflow_claims")
	if err := inTreeServerSafe(ctx, db, admin, ""); err == nil {
		t.Fatalf("accepted a server that holds a workflow_claims database")
	}
}
