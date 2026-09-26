// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fenceProofWork is one projector work row seeded at an explicit instant, so
// two claimers with different injected clocks can see different ready sets.
type fenceProofWork struct {
	scopeID          string
	generationID     string
	generationStatus string
	workStatus       string
	attempts         int
	ingestedAt       time.Time
	visibleAt        time.Time
	updatedAt        time.Time
}

// seedFenceProofWork inserts one generation and its projector work row.
func seedFenceProofWork(t *testing.T, database *sql.DB, work fenceProofWork) {
	t.Helper()
	if _, err := database.Exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'push', $3, $3, $4)
`, work.generationID, work.scopeID, work.ingestedAt, work.generationStatus); err != nil {
		t.Fatalf("insert generation %s: %v", work.generationID, err)
	}
	if _, err := database.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, visible_at, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'projector', 'source_local', $4, $5, $6, '{}'::jsonb, $7, $7)
`, projectorWorkItemID(work.scopeID, work.generationID), work.scopeID, work.generationID,
		work.workStatus, work.attempts, work.visibleAt, work.updatedAt); err != nil {
		t.Fatalf("insert work %s: %v", work.generationID, err)
	}
}

// claimedLeaseCount counts claimed or running projector rows in one scope.
func claimedLeaseCount(t *testing.T, database *sql.DB, scopeID string) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`
SELECT count(*) FROM fact_work_items
WHERE stage = 'projector' AND scope_id = $1 AND status IN ('claimed', 'running')
`, scopeID).Scan(&count); err != nil {
		t.Fatalf("count leases in %s: %v", scopeID, err)
	}
	return count
}

// TestProjectorClaimScopeFenceExcludesCrossSnapshotClaim is the #7115 race in
// one deterministic interleaving. Claimer B takes its statement snapshot at T
// and pauses inside its supersede UPDATE. Claimer A, with clock T+10s, then
// claims gen-s1, a retry that became visible between the two clocks, and
// commits. B's snapshot cannot see that lease, so its in-flight guard passes
// for scope-s and it would pick gen-s2, a second live lease in the same scope.
// The scope claim fence must make B drop scope-s and claim gen-z2 instead.
func TestProjectorClaimScopeFenceExcludesCrossSnapshotClaim(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-z")
	at := time.Now().UTC().Truncate(time.Second)
	for _, work := range []fenceProofWork{
		// Retried generation: invisible at B's clock, visible at A's.
		{"scope-s", "gen-s1", "active", "retrying", 2, at.Add(-2 * time.Hour), at.Add(5 * time.Second), at.Add(-2 * time.Hour)},
		{"scope-s", "gen-s2", "pending", "pending", 0, at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour)},
		// gen-z1 is supersedable, so B's supersede UPDATE fires the pause.
		{"scope-z", "gen-z1", "pending", "pending", 0, at.Add(-3 * time.Hour), at.Add(-3 * time.Hour), at.Add(-3 * time.Hour)},
		{"scope-z", "gen-z2", "pending", "pending", 0, at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-30 * time.Minute)},
	} {
		seedFenceProofWork(t, database, work)
	}
	paused := openPausedClaimPool(t, database, dsn)

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "worker-b", time.Minute)
		queue.Now = func() time.Time { return at }
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{generation: work.Generation.GenerationID, ok: ok, err: err}
	}()
	waitForPausedClaimer(t, dsn)

	racer := NewProjectorQueue(SQLDB{DB: database}, "worker-a", time.Minute)
	racer.Now = func() time.Time { return at.Add(10 * time.Second) }
	work, ok, err := racer.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s1" {
		t.Fatalf("racing Claim() = (%q, %v, %v), want gen-s1", work.Generation.GenerationID, ok, err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if got := claimedLeaseCount(t, database, "scope-s"); got != 1 {
		t.Fatalf("scope-s holds %d leases after both claims, want 1 (paused claim took %q)", got, result.generation)
	}
	if !result.ok || result.generation != "gen-z2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-z2 in the other scope", result.generation, result.ok)
	}
	if status, _, owner := workState(t, database, "scope-s", "gen-s1"); status != "claimed" || owner != "worker-a" {
		t.Fatalf("gen-s1 = (%s, %q), want claimed by worker-a", status, owner)
	}
	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	if got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background()); err != nil || got != 0 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = (%d, %v), want 0", got, err)
	}
}

// TestQueueObserverCountsProjectorScopesWithMultipleLiveLeases proves the
// #7115 invariant gauge's query against real rows: it counts a scope with two
// live leases once, and ignores an expired lease beside a live one and a scope
// with a single live lease.
func TestQueueObserverCountsProjectorScopesWithMultipleLiveLeases(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	seedClaimMaintenanceScopes(t, database, "scope-x", "scope-y", "scope-z")
	seedClaimMaintenanceWork(t, database, "scope-x", "gen-x1", "pending", "claimed", 2*time.Hour, durationPtr(time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-x", "gen-x2", "pending", "running", time.Hour, durationPtr(time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-y", "gen-y1", "pending", "claimed", 2*time.Hour, durationPtr(-time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-y", "gen-y2", "pending", "running", time.Hour, durationPtr(time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-z", "gen-z1", "pending", "claimed", time.Hour, durationPtr(time.Minute))

	observer := NewQueueObserverStore(SQLQueryer{DB: database})
	got, err := observer.ProjectorScopesWithMultipleLiveLeases(context.Background())
	if err != nil {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() error = %v", err)
	}
	if got != 1 {
		t.Fatalf("ProjectorScopesWithMultipleLiveLeases() = %d, want 1 (scope-x only)", got)
	}
}

// TestProjectorClaimSkipsBusyScopeWithoutWaiting holds scope-s's fence row the
// way another claimer in flight does. The claim must not wait for it, must not
// claim in scope-s, and must claim the ready row in another scope. Once the
// holder releases, the next claim takes scope-s.
func TestProjectorClaimSkipsBusyScopeWithoutWaiting(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedBusyScopeProof(t, database)

	holder, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(
		`SELECT scope_id FROM projector_scope_claim_fences WHERE scope_id = 'scope-s' FOR NO KEY UPDATE`,
	); err != nil {
		t.Fatalf("hold fence row: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: openBoundedLockPool(t, database, dsn)}, "claimer", time.Minute)
	work, ok := claimWithinBound(t, queue, "scope-s fence row held")
	if !ok || work.Generation.GenerationID != "gen-o" {
		t.Fatalf("Claim() = (%q, %v), want gen-o while scope-s's fence row is held", work.Generation.GenerationID, ok)
	}
	if status, _, _ := workState(t, database, "scope-s", "gen-s"); status != "pending" {
		t.Fatalf("gen-s status = %q while its fence row is held, want pending", status)
	}

	if err := holder.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("release holder: %v", err)
	}
	work, ok, err = queue.Claim(context.Background())
	if err != nil || !ok || work.Generation.GenerationID != "gen-s" {
		t.Fatalf("Claim() after release = (%q, %v, %v), want gen-s", work.Generation.GenerationID, ok, err)
	}
}

// TestProjectorClaimIgnoresIngestionHoldingScopeRow pins the behaviour the
// fence table restores (#7115): an ingestion transaction holding the
// ingestion_scopes row does not defer the scope's claim, because the claim
// never locks ingestion_scopes. The claim returns gen-s promptly while the
// scope row is held.
func TestProjectorClaimIgnoresIngestionHoldingScopeRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedBusyScopeProof(t, database)

	holder, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(
		`UPDATE ingestion_scopes SET ingested_at = ingested_at WHERE scope_id = 'scope-s'`,
	); err != nil {
		t.Fatalf("hold scope row: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: openBoundedLockPool(t, database, dsn)}, "claimer", time.Minute)
	work, ok := claimWithinBound(t, queue, "scope-s ingestion row held")
	if !ok || work.Generation.GenerationID != "gen-s" {
		t.Fatalf("Claim() = (%q, %v), want gen-s while ingestion holds the scope row", work.Generation.GenerationID, ok)
	}
}

// seedBusyScopeProof seeds pending gen-s (older) in scope-s and gen-o in
// scope-o.
func seedBusyScopeProof(t *testing.T, database *sql.DB) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, "scope-s", "scope-o")
	at := time.Now().UTC().Truncate(time.Second)
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-s", "gen-s", "pending", "pending", 0, at.Add(-2 * time.Hour), at.Add(-2 * time.Hour), at.Add(-2 * time.Hour),
	})
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-o", "gen-o", "pending", "pending", 0, at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
}

// openBoundedLockPool opens a pool on the proof schema whose sessions give up
// on a row lock after 5 s, so a claim that waits fails instead of hanging.
func openBoundedLockPool(t *testing.T, database *sql.DB, dsn string) *sql.DB {
	t.Helper()
	var searchPath string
	if err := database.QueryRow("SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read search_path: %v", err)
	}
	bounded, err := sql.Open("pgx", withDSNParam(withDSNParam(dsn, "search_path="+strings.TrimSpace(searchPath)), "lock_timeout=5000"))
	if err != nil {
		t.Fatalf("open bounded pool: %v", err)
	}
	t.Cleanup(func() { _ = bounded.Close() })
	return bounded
}

// claimWithinBound claims once and fails the test on an error or a claim that
// took longer than 2 s, which means it waited on a lock.
func claimWithinBound(t *testing.T, queue ProjectorQueue, held string) (projector.ScopeGenerationWork, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	work, ok, err := queue.Claim(ctx)
	elapsed := time.Since(started)
	t.Logf("claim with %s took %s", held, elapsed)
	if err != nil {
		t.Fatalf("Claim() error = %v after %s with %s, want no wait", err, elapsed, held)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Claim() took %s with %s, want no wait", elapsed, held)
	}
	return work, ok
}

// TestProjectorClaimLeavesFenceUnlockedWhenWorkRowBusy proves the lock step's
// order: work row first, then fence row. When another transaction holds the
// candidate's work row, the claim must skip it without taking that
// candidate's fence row, so another claimer is not turned away from the scope
// for the rest of this transaction. The inverse also holds: the scope the
// claim did take has its fence row locked and its ingestion_scopes row free.
// The claim runs inside an open transaction and a probe session tries each
// row with NOWAIT.
func TestProjectorClaimLeavesFenceUnlockedWhenWorkRowBusy(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedBusyScopeProof(t, database)
	at := time.Now().UTC().Truncate(time.Second)
	ctx := context.Background()

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(`SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`,
		projectorWorkItemID("scope-s", "gen-s")); err != nil {
		t.Fatalf("hold gen-s work row: %v", err)
	}

	claimer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin claimer: %v", err)
	}
	defer func() { _ = claimer.Rollback() }()
	var claimedGeneration string
	if err := claimer.QueryRow(claimProjectorWorkQuery, at, "claimer", at.Add(time.Minute), "").Scan(
		new(string), new(string), new(string), new(string), new(string), new(bool), new(string), new(string),
		&claimedGeneration, new(int), new(time.Time), new(time.Time), new(string), new(string), new(string), new([]byte),
	); err != nil {
		t.Fatalf("claim in open transaction: %v", err)
	}
	if claimedGeneration != "gen-o" {
		t.Fatalf("claimed %q, want gen-o while gen-s's work row is held", claimedGeneration)
	}

	for _, probe := range []struct {
		query      string
		wantLocked bool
		what       string
	}{
		{`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-s' FOR NO KEY UPDATE NOWAIT`, false, "skipped scope-s fence row"},
		{`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-o' FOR NO KEY UPDATE NOWAIT`, true, "claimed scope-o fence row"},
		{`SELECT 1 FROM ingestion_scopes WHERE scope_id = 'scope-o' FOR NO KEY UPDATE NOWAIT`, false, "claimed scope-o ingestion_scopes row"},
	} {
		locked := probeRowLocked(t, database, probe.query)
		if locked != probe.wantLocked {
			t.Fatalf("%s locked = %v, want %v", probe.what, locked, probe.wantLocked)
		}
	}
}

// probeRowLocked runs a NOWAIT lock query in its own rolled-back transaction
// and reports whether it hit SQLSTATE 55P03 (lock_not_available).
func probeRowLocked(t *testing.T, database *sql.DB, query string) bool {
	t.Helper()
	probe, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin probe: %v", err)
	}
	defer func() { _ = probe.Rollback() }()
	_, err = probe.Exec(query)
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return false
	case errors.As(err, &pgErr) && pgErr.Code == "55P03":
		return true
	default:
		t.Fatalf("probe %q: %v", query, err)
		return false
	}
}
