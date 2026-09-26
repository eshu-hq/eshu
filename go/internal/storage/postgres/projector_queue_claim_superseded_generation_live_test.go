// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// supersededClaimRow is one extra projector work row on a scope's retired
// generation. claimUntil is relative to now; nil leaves the row unleased.
type supersededClaimRow struct {
	workItemID string
	status     string
	claimUntil *time.Duration
}

// seedSupersededClaimScope seeds the #7130 claim hazard: gen-old is
// superseded, gen-new is active and published, and gen-new's projector row
// succeeded. The scope's projector_scope_claim_fences row comes from the
// ingestion_scopes insert trigger, as in every #7115 claim fixture.
func seedSupersededClaimScope(t *testing.T, database *sql.DB, scopeID string, rows ...supersededClaimRow) time.Time {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, scopeID)
	oldSupersededAt := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Microsecond)
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at
) VALUES ($1 || '-gen-old', $1, 'push', now() - interval '2 hours', now() - interval '2 hours',
          'superseded', now() - interval '2 hours', $2),
         ($1 || '-gen-new', $1, 'push', now() - interval '1 hour', now() - interval '1 hour',
          'active', now() - interval '30 minutes', NULL)`, args: []any{scopeID, oldSupersededAt}},
		{
			query: `UPDATE ingestion_scopes SET active_generation_id = $1 || '-gen-new' WHERE scope_id = $1`,
			args:  []any{scopeID},
		},
		{query: `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, visible_at, payload, created_at, updated_at
) VALUES ('projector_' || $1 || '_' || $1 || '-gen-new', $1, $1 || '-gen-new', 'projector',
          'source_local', 'succeeded', 1, NULL, '{}'::jsonb,
          now() - interval '1 hour', now() - interval '30 minutes')`, args: []any{scopeID}},
	} {
		if _, err := database.Exec(seed.query, seed.args...); err != nil {
			t.Fatalf("seed superseded claim scope %s: %v", scopeID, err)
		}
	}
	for _, row := range rows {
		var until, owner any
		if row.claimUntil != nil {
			until = time.Now().UTC().Add(*row.claimUntil)
			owner = "zombie-worker"
		}
		if _, err := database.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ($1, $2, $2 || '-gen-old', 'projector', 'source_local', $3, 1, $4, $5,
          now() - interval '10 minutes', '{}'::jsonb,
          now() - interval '10 minutes', now() - interval '10 minutes')
`, row.workItemID, scopeID, row.status, owner, until); err != nil {
			t.Fatalf("seed work %s: %v", row.workItemID, err)
		}
	}
	return oldSupersededAt
}

// supersededClaimSweepState is the row state the claim fence must leave.
type supersededClaimSweepState struct {
	status, failureClass, generationStatus string
	leaseOwner, claimUntil                 sql.NullString
	attempts                               int
}

func readSupersededClaimSweepState(t *testing.T, database *sql.DB, workItemID string) supersededClaimSweepState {
	t.Helper()
	var state supersededClaimSweepState
	var failureClass, generationStatus sql.NullString
	if err := database.QueryRow(`
SELECT status, failure_class, failure_details::jsonb ->> 'generation_status',
       lease_owner, claim_until::text, attempt_count
FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(
		&state.status, &failureClass, &generationStatus,
		&state.leaseOwner, &state.claimUntil, &state.attempts,
	); err != nil {
		t.Fatalf("read work %s: %v", workItemID, err)
	}
	state.failureClass = failureClass.String
	state.generationStatus = generationStatus.String
	return state
}

// assertPublishedGenerationUntouched checks the claim fence never moves the
// published generation, the scope pointer, or the retired generation.
func assertPublishedGenerationUntouched(t *testing.T, database *sql.DB, scopeID string, oldSupersededAt time.Time) {
	t.Helper()
	var newStatus, oldStatus, pointer string
	var newSupersededAt sql.NullTime
	var gotOldSupersededAt time.Time
	if err := database.QueryRow(`
SELECT
    (SELECT status FROM scope_generations WHERE generation_id = $1 || '-gen-new'),
    (SELECT superseded_at FROM scope_generations WHERE generation_id = $1 || '-gen-new'),
    (SELECT status FROM scope_generations WHERE generation_id = $1 || '-gen-old'),
    (SELECT superseded_at FROM scope_generations WHERE generation_id = $1 || '-gen-old'),
    (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)`, scopeID).Scan(
		&newStatus, &newSupersededAt, &oldStatus, &gotOldSupersededAt, &pointer,
	); err != nil {
		t.Fatalf("read generations of %s: %v", scopeID, err)
	}
	if newStatus != "active" || newSupersededAt.Valid || pointer != scopeID+"-gen-new" {
		t.Fatalf("published generation = (%s, superseded_at valid=%v), pointer %s; want active, NULL, %s-gen-new",
			newStatus, newSupersededAt.Valid, pointer, scopeID)
	}
	if oldStatus != "superseded" || !gotOldSupersededAt.Equal(oldSupersededAt) {
		t.Fatalf("retired generation = (%s, %v), want (superseded, %v)", oldStatus, gotOldSupersededAt, oldSupersededAt)
	}
}

// TestProjectorClaimSweepsSupersededGenerationRow is the #7130 claim fence.
// A refinalize or liveness-recovery row left on a generation that a newer Ack
// retired must never be claimed: projecting it retracts the published
// generation's canonical graph before Ack refuses. Claim must mark the row
// superseded instead, including an expired-lease zombie row that the reclaim
// rank would otherwise re-claim, and leave a live lease to its heartbeat.
func TestProjectorClaimSweepsSupersededGenerationRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name       string
		status     string
		claimUntil *time.Duration
	}{
		{name: "pending", status: "pending"},
		{name: "retrying", status: "retrying"},
		{name: "expired_claimed_zombie", status: "claimed", claimUntil: durationPtr(-time.Minute)},
		{name: "expired_running_zombie", status: "running", claimUntil: durationPtr(-time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			workItemID := "refinalize_scope-t1_scope-t1-gen-old"
			oldSupersededAt := seedSupersededClaimScope(t, database, "scope-t1",
				supersededClaimRow{workItemID: workItemID, status: tc.status, claimUntil: tc.claimUntil})
			queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

			work, ok, err := queue.Claim(ctx)
			if err != nil || ok {
				t.Fatalf("Claim() = (%s, %v, %v), want no claim of the retired generation",
					work.Generation.GenerationID, ok, err)
			}
			state := readSupersededClaimSweepState(t, database, workItemID)
			if state.status != "superseded" ||
				state.failureClass != "projector_superseded_by_newer_generation" ||
				state.generationStatus != "superseded" ||
				state.leaseOwner.Valid || state.claimUntil.Valid || state.attempts != 1 {
				t.Fatalf("retired-generation row = %+v, want superseded, lease cleared, attempts 1, generation_status superseded", state)
			}
			assertPublishedGenerationUntouched(t, database, "scope-t1", oldSupersededAt)
		})
	}

	t.Run("sweep_frees_scope_for_newer_pending_generation", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		oldSupersededAt := seedSupersededClaimScope(t, database, "scope-t1n",
			supersededClaimRow{workItemID: "refinalize_scope-t1n_scope-t1n-gen-old", status: "pending"})
		seedClaimMaintenanceWork(t, database, "scope-t1n", "scope-t1n-gen-next", "pending", "pending", 0, nil)
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok || work.Generation.GenerationID != "scope-t1n-gen-next" {
			t.Fatalf("Claim() = (%s, %v, %v), want scope-t1n-gen-next", work.Generation.GenerationID, ok, err)
		}
		if state := readSupersededClaimSweepState(t, database, "refinalize_scope-t1n_scope-t1n-gen-old"); state.status != "superseded" {
			t.Fatalf("retired-generation row status = %s, want superseded in the same claim", state.status)
		}
		assertPublishedGenerationUntouched(t, database, "scope-t1n", oldSupersededAt)
	})

	t.Run("live_lease_left_to_heartbeat", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		workItemID := "refinalize_scope-t1l_scope-t1l-gen-old"
		seedSupersededClaimScope(t, database, "scope-t1l",
			supersededClaimRow{workItemID: workItemID, status: "running", claimUntil: durationPtr(time.Minute)})
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

		if work, ok, err := queue.Claim(ctx); err != nil || ok {
			t.Fatalf("Claim() = (%s, %v, %v), want no claim", work.Generation.GenerationID, ok, err)
		}
		state := readSupersededClaimSweepState(t, database, workItemID)
		if state.status != "running" || state.leaseOwner.String != "zombie-worker" {
			t.Fatalf("live lease row = %+v, want running and still owned; the claim must not steal a live lease", state)
		}
	})

	// failed and dead_letter rows are never claim candidates and replay keeps
	// them terminal, so the fence leaves them, and their triage class, alone.
	// Sweeping them would also put a legacy dead-letter backlog into one claim.
	for _, status := range []string{"failed", "dead_letter"} {
		t.Run(status+"_left_alone", func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			workItemID := "refinalize_scope-t1t_scope-t1t-gen-old"
			seedSupersededClaimScope(t, database, "scope-t1t",
				supersededClaimRow{workItemID: workItemID, status: status})
			if _, err := database.Exec(
				`UPDATE fact_work_items SET failure_class = 'triage_x' WHERE work_item_id = $1`, workItemID,
			); err != nil {
				t.Fatalf("set triage class: %v", err)
			}
			queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

			if work, ok, err := queue.Claim(ctx); err != nil || ok {
				t.Fatalf("Claim() = (%s, %v, %v), want no claim", work.Generation.GenerationID, ok, err)
			}
			state := readSupersededClaimSweepState(t, database, workItemID)
			if state.status != status || state.failureClass != "triage_x" || state.attempts != 1 {
				t.Fatalf("%s row = %+v, want status %s and failure_class triage_x unchanged", status, state, status)
			}
		})
	}
}

// TestProjectorClaimStillClaimsLiveGenerations holds the controls the fence
// must not break: a replay after Fail (failed generation, no newer sibling)
// and ordinary work on the active generation are still claimed.
func TestProjectorClaimStillClaimsLiveGenerations(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	for _, generationStatus := range []string{"failed", "active", "pending"} {
		t.Run(generationStatus, func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			seedClaimMaintenanceScopes(t, database, "scope-ctl")
			seedClaimMaintenanceWork(t, database, "scope-ctl", "gen-ctl", generationStatus, "pending", time.Hour, nil)
			queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

			work, ok, err := queue.Claim(ctx)
			if err != nil || !ok || work.Generation.GenerationID != "gen-ctl" {
				t.Fatalf("Claim() = (%s, %v, %v), want gen-ctl claimed", work.Generation.GenerationID, ok, err)
			}
		})
	}
}

// TestProjectorClaimSupersededSweepDropsLeaseRenewedAfterSnapshot proves the
// widened lock step keeps its EvalPlanQual recheck. The claim's snapshot sees
// b-row as an expired zombie on a superseded generation. While the claim is
// paused superseding a-row, before it locks b-row, a heartbeat renews b-row's
// lease and commits. The lock step must re-read claim_until on the locked
// version and drop b-row, so a live lease is never swept.
func TestProjectorClaimSupersededSweepDropsLeaseRenewedAfterSnapshot(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	seedSupersededClaimScope(t, database, "scope-ea", supersededClaimRow{workItemID: "a-row", status: "pending"})
	seedSupersededClaimScope(t, database, "scope-eb",
		supersededClaimRow{workItemID: "b-row", status: "running", claimUntil: durationPtr(-time.Minute)})
	paused := openPausedClaimPool(t, database, dsn)

	done := make(chan error, 1)
	go func() {
		queue := NewProjectorQueue(SQLDB{DB: paused}, "claimer", time.Minute)
		_, ok, err := queue.Claim(context.Background())
		if err == nil && ok {
			err = errors.New("claim returned work, want none")
		}
		done <- err
	}()
	waitForPausedClaimer(t, dsn)

	renew, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin renewal: %v", err)
	}
	defer func() { _ = renew.Rollback() }()
	// A claim that already locked b-row would block this renewal; fail fast
	// instead, because the recheck would then not be exercised.
	if _, err := renew.Exec(`SET LOCAL lock_timeout = '500ms'`); err != nil {
		t.Fatalf("set renewal lock_timeout: %v", err)
	}
	if _, err := renew.Exec(`
UPDATE fact_work_items
SET claim_until = now() + interval '5 minutes', updated_at = now()
WHERE work_item_id = 'b-row'`); err != nil {
		t.Fatalf("renew b-row while the claim is paused (claim locked it early?): %v", err)
	}
	if err := renew.Commit(); err != nil {
		t.Fatalf("commit renewal: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("paused Claim() = %v", err)
	}
	if state := readSupersededClaimSweepState(t, database, "a-row"); state.status != "superseded" {
		t.Fatalf("a-row status = %s, want superseded", state.status)
	}
	state := readSupersededClaimSweepState(t, database, "b-row")
	if state.status != "running" || state.leaseOwner.String != "zombie-worker" {
		t.Fatalf("b-row = %+v, want running and owned: a lease renewed after the snapshot must survive", state)
	}
}
