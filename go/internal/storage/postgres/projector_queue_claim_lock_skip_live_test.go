// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// TestProjectorClaimDoesNotWaitOnLockedMaintenanceRows proves the claim never
// blocks behind another transaction's row lock on a row one of its
// maintenance branches would update (#7108). The shipped statement waited
// here, which is the wait edge that closed the deadlock cycle. Each locked row
// must be left alone, not claimed, and handled by the first claim after the
// lock is released:
//   - scope-l gen-old: stale generation the supersede branch would retire
//   - scope-d gen-d1: expired duplicate beside a live lease
//   - scope-f gen-f2: expired sibling of the scope this claim takes
//   - scope-g gen-g-old: stale generation whose generation row is locked
func TestProjectorClaimDoesNotWaitOnLockedMaintenanceRows(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedClaimMaintenanceScopes(t, database, "scope-l", "scope-d", "scope-f", "scope-g")
	seedClaimMaintenanceWork(t, database, "scope-g", "gen-g-old", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-g", "gen-g-new", "pending", "pending", 2*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-l", "gen-old", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-l", "gen-new", "pending", "pending", 2*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-d", "gen-d1", "pending", "claimed", 3*time.Hour, durationPtr(-time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-d", "gen-d2", "active", "running", 4*time.Hour, durationPtr(time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-f", "gen-f1", "pending", "claimed", 5*time.Hour, durationPtr(-2*time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-f", "gen-f2", "pending", "claimed", time.Hour, durationPtr(-time.Minute))

	holder, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(`
SELECT work_item_id FROM fact_work_items WHERE work_item_id = ANY($1) FOR NO KEY UPDATE
`, []string{
		projectorWorkItemID("scope-l", "gen-old"),
		projectorWorkItemID("scope-d", "gen-d1"),
		projectorWorkItemID("scope-f", "gen-f2"),
	}); err != nil {
		t.Fatalf("hold maintenance row locks: %v", err)
	}
	if _, err := holder.Exec(`
SELECT generation_id FROM scope_generations WHERE generation_id = 'gen-g-old' FOR NO KEY UPDATE
`); err != nil {
		t.Fatalf("hold stale generation lock: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	work, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v after %s, want no wait on locked maintenance rows", err, time.Since(started))
	}
	t.Logf("claim with maintenance rows locked took %s", time.Since(started))
	if !ok || work.Generation.GenerationID != "gen-f1" {
		t.Fatalf("Claim() = (%q, %v), want the expired lease gen-f1", work.Generation.GenerationID, ok)
	}
	for _, row := range [][3]string{
		{"scope-l", "gen-old", "pending"},
		{"scope-l", "gen-new", "pending"},
		{"scope-g", "gen-g-old", "pending"},
		{"scope-g", "gen-g-new", "pending"},
		{"scope-d", "gen-d1", "claimed"},
		{"scope-f", "gen-f2", "claimed"},
	} {
		if status, _, _ := workState(t, database, row[0], row[1]); status != row[2] {
			t.Fatalf("%s status = %q while locked, want %s", row[1], status, row[2])
		}
	}

	// A second claim while the locks are held must not take gen-g-old, whose
	// work row is free but stale, nor jump ahead to a newer generation.
	if work, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("second Claim() while locked = (%q, %v, %v), want no claim", work.Generation.GenerationID, ok, err)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	released := time.Now()
	claimed := map[string]bool{}
	for range 4 {
		work, ok, err := queue.Claim(context.Background())
		if err != nil {
			t.Fatalf("Claim() after release error = %v", err)
		}
		if !ok {
			break
		}
		claimed[work.Generation.GenerationID] = true
	}
	t.Logf("claims after release took %s", time.Since(released))
	if !claimed["gen-new"] || !claimed["gen-g-new"] || len(claimed) != 2 {
		t.Fatalf("claims after release = %v, want exactly gen-new and gen-g-new", claimed)
	}
	for _, row := range [][3]string{
		{"scope-l", "gen-old", "superseded"},
		{"scope-g", "gen-g-old", "superseded"},
		{"scope-d", "gen-d1", "retrying"},
		{"scope-f", "gen-f2", "retrying"},
	} {
		if status, _, _ := workState(t, database, row[0], row[1]); status != row[2] {
			t.Fatalf("%s status after release = %q, want %s", row[1], status, row[2])
		}
	}
}

// TestProjectorClaimLockRechecksRowsChangedAfterSnapshot is the EvalPlanQual
// proof for the lock-first rewrite. A test-only trigger pauses the claim
// inside its first UPDATE, after the statement snapshot. Meanwhile another
// session claims the row the candidate would pick and renews the lease the
// duplicate reclaim would take. Both locks must re-check the row-self
// predicates on the committed row and drop it.
func TestProjectorClaimLockRechecksRowsChangedAfterSnapshot(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedClaimMaintenanceScopes(t, database, "scope-a", "scope-b", "scope-c")
	seedClaimMaintenanceWork(t, database, "scope-a", "gen-a1", "pending", "pending", 3*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-a", "gen-a2", "pending", "pending", 2*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-b", "gen-b", "pending", "pending", 5*time.Hour, nil)
	seedClaimMaintenanceWork(t, database, "scope-c", "gen-c1", "pending", "claimed", 4*time.Hour, durationPtr(-time.Minute))
	seedClaimMaintenanceWork(t, database, "scope-c", "gen-c2", "active", "running", 4*time.Hour, durationPtr(time.Minute))
	if _, err := database.Exec(`
CREATE FUNCTION proof_pause_on_supersede() RETURNS trigger AS $$
BEGIN
    IF NEW.status = 'superseded' AND OLD.status <> 'superseded'
       AND current_setting('eshu_proof.pause', true) = 'on' THEN
        PERFORM pg_sleep(1.5);
    END IF;
    RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER proof_pause_on_supersede BEFORE UPDATE ON fact_work_items
FOR EACH ROW EXECUTE FUNCTION proof_pause_on_supersede();
`); err != nil {
		t.Fatalf("install pause trigger: %v", err)
	}
	var searchPath string
	if err := database.QueryRow("SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read search_path: %v", err)
	}
	pausedDSN := withDSNParam(withDSNParam(withDSNParam(dsn,
		"search_path="+strings.TrimSpace(searchPath)),
		"eshu_proof.pause=on"),
		"application_name="+pausedClaimApplicationName)
	paused, err := sql.Open("pgx", pausedDSN)
	if err != nil {
		t.Fatalf("open paused pool: %v", err)
	}
	t.Cleanup(func() { _ = paused.Close() })

	type claimResult struct {
		generation string
		ok         bool
		err        error
	}
	done := make(chan claimResult, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "paused-claimer", time.Minute)
		work, ok, err := queue.Claim(context.Background())
		done <- claimResult{generation: work.Generation.GenerationID, ok: ok, err: err}
	}()
	// Race the claimer only once it is provably inside the trigger's pg_sleep,
	// after its statement snapshot. A fixed sleep could let the UPDATEs commit
	// before the snapshot, so the assertions would pass without EvalPlanQual.
	waitForPausedClaimer(t, dsn)
	if _, err := database.Exec(`
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'racing-worker', claim_until = now() + interval '1 minute'
WHERE work_item_id = $1
`, projectorWorkItemID("scope-b", "gen-b")); err != nil {
		t.Fatalf("racing claim: %v", err)
	}
	if _, err := database.Exec(`
UPDATE fact_work_items SET claim_until = now() + interval '1 minute' WHERE work_item_id = $1
`, projectorWorkItemID("scope-c", "gen-c1")); err != nil {
		t.Fatalf("racing heartbeat: %v", err)
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("paused Claim() error = %v", result.err)
	}
	if !result.ok || result.generation != "gen-a2" {
		t.Fatalf("paused Claim() = (%q, %v), want gen-a2 after dropping the raced gen-b", result.generation, result.ok)
	}
	if status, _, owner := workState(t, database, "scope-b", "gen-b"); status != "claimed" || owner != "racing-worker" {
		t.Fatalf("raced gen-b = (%s, %q), want kept by racing-worker", status, owner)
	}
	if status, class, owner := workState(t, database, "scope-c", "gen-c1"); status != "claimed" || owner != "other-worker" {
		t.Fatalf("renewed gen-c1 = (%s, %s, %q), want its renewed lease kept", status, class, owner)
	}
}

// pausedClaimApplicationName tags the session whose claim the proof trigger
// pauses, so the test can find it in pg_stat_activity.
const pausedClaimApplicationName = "eshu_7108_paused_claimer"

// waitForPausedClaimer polls pg_stat_activity until the tagged session is
// waiting in pg_sleep inside an active statement, with a deadline. It fails the
// test if the paused state is never reached, because the racing UPDATEs would
// then not exercise the lock recheck.
func waitForPausedClaimer(t *testing.T, dsn string) {
	t.Helper()
	observer, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open observer: %v", err)
	}
	defer func() { _ = observer.Close() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var paused bool
		if err := observer.QueryRow(`
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE application_name = $1
      AND state = 'active'
      AND wait_event_type = 'Timeout'
      AND wait_event = 'PgSleep'
)`, pausedClaimApplicationName).Scan(&paused); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if paused {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("claimer never reached pg_sleep within 10s; the racing UPDATEs would not test the recheck")
}
