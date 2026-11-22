// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 deterministic interleaving of a narrowed wait park against the in-flight activation wake on the production schema
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// TestWaitParkDuringInFlightActivationIsWokenLive pins the ordering
// that leaves a narrowed wait parked after its only blocker activated (#11256):
// the activation's narrowed wake statement has taken its snapshot, which cannot
// contain a wait row that does not exist yet, and the activation has not
// committed; the Fail path then parks the wait, and its satisfaction check
// cannot see the uncommitted activation; the activation commits last. Without
// a rendezvous, both sides read a snapshot that predates the other's commit and
// neither reopens the wait, so it sleeps to the audit (5 minutes) or the
// backstop (10 minutes). On main the original concurrent test failed 132 of 200
// runs with exactly that end state.
//
// The test runs the production ActivateResolutionGeneration and parks through
// the production setAsideWaitingForCause, and stalls the activation after its
// wake snapshot without any product seam or sleep: a decoy wait row that also
// names the holder is locked by the test, so the wake's FOR UPDATE blocks on it
// after the statement snapshot is fixed. The test then waits until the parker
// has either returned or is blocked on a lock, and only then lets the
// activation finish. A wait row must not survive.
func TestWaitParkDuringInFlightActivationIsWokenLive(t *testing.T) {
	cases := []struct {
		name string
		// holderScope and holderGen are the generation the test activates;
		// the waiter scope and generation are the parked intent's own.
		holderScope, holderGen string
		waiterScope, waiterGen string
		// waiterGenActive seeds the waiter's own generation active (the
		// narrowed wait, whose blocker is a different scope).
		waiterGenActive bool
		cause           error
	}{
		{
			name:        "narrowed corpus wait parks behind its blocker's activation",
			holderScope: "scope-holder-il", holderGen: "gen-holder-il",
			waiterScope: "scope-waiter-il", waiterGen: "gen-waiter-il", waiterGenActive: true,
			cause: narrowWaitTestCause{repos: []string{"repo-il"}, blocking: []string{"scope-holder-il"}},
		},
		{
			// The provider is the activation of the waiter's own generation
			// (the coarse relationship_generation_active wait, no narrowing).
			name:        "coarse generation-active wait parks behind its own activation",
			holderScope: "scope-waiter-co", holderGen: "gen-waiter-co",
			waiterScope: "scope-waiter-co", waiterGen: "gen-waiter-co",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runParkDuringActivation(t, tc.holderScope, tc.holderGen, tc.waiterScope, tc.waiterGen, tc.waiterGenActive, tc.cause)
		})
	}
}

func runParkDuringActivation(t *testing.T, holderScope, holderGen, waiterScope, waiterGen string, waiterGenActive bool, cause error) {
	t.Helper()
	ctx := context.Background()
	database, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: database})
	// The wake statement blocks on the decoy for as long as the test needs on a
	// loaded host; the default lock timeout would abort the activation.
	store.activationLockTimeout = 60 * time.Second

	if waiterScope != holderScope {
		status := "pending"
		if waiterGenActive {
			status = "active"
		}
		seedNarrowWaitScope(t, ctx, database, waiterScope, waiterGen, status, at)
	}
	seedNarrowWaitScope(t, ctx, database, holderScope, holderGen, "pending", at)
	for _, id := range []string{"wait-il", "wait-decoy-il"} {
		seedNarrowRetryRow(t, ctx, database, id, waiterScope, waiterGen,
			"workload_materialization", "workload_materialization_resolution_not_ready", at)
	}
	park := func(id string) error {
		_, err := setAsideWaitingForCause(ctx, SQLDB{DB: database}, nil,
			narrowWaitIntent(id, waiterScope, waiterGen), narrowWaitEntry(),
			cause, nil, time.Now().UTC())
		return err
	}

	// The decoy is parked through the production path (no activation is in
	// flight, so it parks at once) and then locked by this test.
	if err := park("wait-decoy-il"); err != nil {
		t.Fatalf("park decoy: %v", err)
	}
	hold, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin decoy lock: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = hold.Rollback()
		}
	}
	defer release()
	if _, err := hold.ExecContext(ctx,
		`SELECT 1 FROM reducer_waiting_items WHERE work_item_id = 'wait-decoy-il' FOR UPDATE`); err != nil {
		t.Fatalf("lock decoy wait row: %v", err)
	}

	// 1. The production activation runs until its wake statement blocks on the
	// decoy, with its snapshot already taken and the real wait not yet parked.
	activated := make(chan error, 1)
	go func() { activated <- store.ActivateResolutionGeneration(ctx, holderGen, holderScope) }()
	awaitBlockedBackends(t, ctx, database, 1, nil)

	// 2. The Fail path parks the real wait while the activation is in flight.
	parked := make(chan error, 1)
	go func() { parked <- park("wait-il") }()
	parkReturned := awaitBlockedBackends(t, ctx, database, 2, parked)

	// 3. The activation finishes last.
	release()
	if err := <-activated; err != nil {
		t.Fatalf("activate holder: %v", err)
	}
	if !parkReturned {
		select {
		case err := <-parked:
			if err != nil {
				t.Fatalf("park wait: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("parker did not return after the activation committed")
		}
	}

	if got := countNarrowWaits(t, ctx, database, "wait-il"); got != 0 {
		status, attempts, visibleAt := narrowQueueState(t, ctx, database, "wait-il")
		t.Fatalf("wait rows after the activation committed = %d, want 0: the holder generation is active, "+
			"queue row %s attempts %d visible_at %s; a wake lost here sleeps to the audit or the backstop",
			got, status, attempts, visibleAt.Format(time.RFC3339))
	}
	status, attempts, _ := narrowQueueState(t, ctx, database, "wait-il")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("wait-il queue row = %s attempts %d, want retrying/1: the wake must not burn the attempt budget", status, attempts)
	}
	if got := countNarrowWaits(t, ctx, database, "wait-decoy-il"); got != 0 {
		t.Fatalf("decoy wait rows after the activation = %d, want 0: the wake must still reopen what it saw", got)
	}
}

// awaitBlockedBackends waits, with no guess-sized sleep, until at least want
// backends in the proof database are waiting on a lock. With a non-nil parked
// channel it also returns early, reporting true, once the parker has returned
// (its error already checked): a parker that never blocks is the unfixed
// behaviour the caller then asserts on. It fails when neither happens within
// the bound.
func awaitBlockedBackends(t *testing.T, ctx context.Context, database *sql.DB, want int, parked <-chan error) bool {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if parked != nil {
			select {
			case err := <-parked:
				if err != nil {
					t.Fatalf("park wait: %v", err)
				}
				return true
			default:
			}
		}
		var blocked int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND wait_event_type = 'Lock'`).Scan(&blocked); err != nil {
			t.Fatalf("read blocked backends: %v", err)
		}
		if blocked >= want {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("fewer than %d backends blocked on a lock within 30s", want)
	return false
}
