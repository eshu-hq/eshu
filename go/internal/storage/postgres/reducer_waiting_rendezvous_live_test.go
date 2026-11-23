// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 rendezvous cost contract: a park waiting on one scope holds no lock another scope's activation needs, and a many-scope blocking set takes bounded locks
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// TestParkWaitingOnOneScopeDoesNotStallAnotherScopesActivationLive pins the
// conflict domain of the #11256 rendezvous: one scope's activation. A narrowed
// wait that names two blocking scopes waits behind the in-flight activation of
// the second. While it waits it must hold no advisory lock, or the activation
// of the first scope (which needs that lock exclusively) would queue behind a
// parker that is itself queued behind an unrelated activation, up to the park
// bound, and every later park of the first scope would queue behind that
// activation: a convoy across scopes.
//
// The test stands in for the second scope's in-flight activation with a held
// exclusive lock on its key, parks through the production
// setAsideWaitingForCause, reads pg_locks for the parker while it waits, and
// then runs the production activation of the first scope, which must finish
// while the parker is still waiting.
func TestParkWaitingOnOneScopeDoesNotStallAnotherScopesActivationLive(t *testing.T) {
	ctx := context.Background()
	database, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: database})

	seedNarrowWaitScope(t, ctx, database, "scope-waiter-cv", "gen-waiter-cv", "active", at)
	seedNarrowWaitScope(t, ctx, database, "scope-holder-cv-1", "gen-holder-cv-1", "pending", at)
	seedNarrowWaitScope(t, ctx, database, "scope-holder-cv-2", "gen-holder-cv-2", "pending", at)
	seedNarrowRetryRow(t, ctx, database, "wait-cv", "scope-waiter-cv", "gen-waiter-cv",
		"workload_materialization", "workload_materialization_resolution_not_ready", at)

	// The second scope's activation is in flight: its exclusive lock is held.
	hold, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin in-flight activation stand-in: %v", err)
	}
	defer func() { _ = hold.Rollback() }()
	if _, err := hold.ExecContext(ctx, activationRendezvousLockSQL,
		activationRendezvousKey("scope-holder-cv-2")); err != nil {
		t.Fatalf("hold the second scope's activation lock: %v", err)
	}

	cause := narrowWaitTestCause{
		repos:    []string{"repo-cv"},
		blocking: []string{"scope-holder-cv-1", "scope-holder-cv-2"},
	}
	parked := make(chan error, 1)
	go func() {
		_, err := setAsideWaitingForCause(ctx, SQLDB{DB: database}, nil,
			narrowWaitIntent("wait-cv", "scope-waiter-cv", "gen-waiter-cv"), narrowWaitEntry(),
			cause, nil, time.Now().UTC())
		parked <- err
	}()
	awaitBlockedBackends(t, ctx, database, 1, nil)

	var heldByWaiter int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks AS held
WHERE held.locktype = 'advisory' AND held.granted
  AND held.database = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND held.pid IN (
      SELECT waiting.pid FROM pg_locks AS waiting
      WHERE waiting.locktype = 'advisory' AND NOT waiting.granted
        AND waiting.database = (SELECT oid FROM pg_database WHERE datname = current_database()))`).Scan(&heldByWaiter); err != nil {
		t.Fatalf("read advisory locks held by the waiting park: %v", err)
	}
	if heldByWaiter != 0 {
		t.Fatalf("the waiting park holds %d advisory locks, want 0: a park that waits on one scope while holding another's lock stalls that scope's activation", heldByWaiter)
	}

	activated := make(chan error, 1)
	go func() {
		activated <- store.ActivateResolutionGeneration(ctx, "gen-holder-cv-1", "scope-holder-cv-1")
	}()
	select {
	case err := <-activated:
		if err != nil {
			t.Fatalf("activate the first scope: %v", err)
		}
	case err := <-parked:
		t.Fatalf("the park returned (err %v) before the first scope's activation did: the activation waited behind the parker", err)
	case <-time.After(narrowedParkRendezvousTimeout / 2):
		t.Fatalf("the first scope's activation had not finished after %s while the parker waited on the second scope",
			narrowedParkRendezvousTimeout/2)
	}

	if err := hold.Rollback(); err != nil {
		t.Fatalf("release the second scope's activation lock: %v", err)
	}
	if err := <-parked; err != nil {
		t.Fatalf("park wait: %v", err)
	}
}

// TestParkWithManyBlockingScopesTakesBoundedLocksLive pins that a park's
// advisory locks are bounded however large the blocking set is. A session that
// takes one advisory lock per key in one statement fails with "out of shared
// memory" once the keys outgrow the cluster lock table (stock postgres 18.6:
// 64 locks per transaction slot times max_connections), and that error stops
// the whole reducer run. The blocking set here spans several try-lock chunks,
// and the in-flight activation sits in a middle chunk: the park must find it,
// wait on that one key while holding no advisory lock, and finish once the
// activation is done.
func TestParkWithManyBlockingScopesTakesBoundedLocksLive(t *testing.T) {
	ctx := context.Background()
	database, _ := openNarrowFenceDB(t, ctx)

	// Large enough to span four chunks.
	blocking := make([]string, 0, 400)
	for i := range 400 {
		blocking = append(blocking, fmt.Sprintf("scope-incomplete-big-%06d", i))
	}
	hold, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin in-flight activation stand-in: %v", err)
	}
	defer func() { _ = hold.Rollback() }()
	if _, err := hold.ExecContext(ctx, activationRendezvousLockSQL,
		activationRendezvousKey(blocking[200])); err != nil {
		t.Fatalf("hold a middle scope's activation lock: %v", err)
	}

	// A key that names the whole set, as rows parked before the #11256 key
	// bound do (new keys name at most 16 scopes).
	wide, err := json.Marshal(waitingKey{ScopeID: "scope-waiter-big", GenerationID: "gen-waiter-big", BlockingScopeIDs: blocking})
	if err != nil {
		t.Fatalf("marshal wide key: %v", err)
	}
	parked := make(chan error, 1)
	go func() {
		parked <- awaitInFlightActivations(ctx, SQLDB{DB: database}, nil,
			WaitingKindRelationshipGenerationActive, string(wide))
	}()
	if awaitBlockedBackends(t, ctx, database, 1, parked) {
		t.Fatalf("the park returned without waiting on the in-flight activation of %s", blocking[200])
	}

	var heldByWaiter int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks AS held
WHERE held.locktype = 'advisory' AND held.granted
  AND held.database = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND held.pid IN (
      SELECT waiting.pid FROM pg_locks AS waiting
      WHERE waiting.locktype = 'advisory' AND NOT waiting.granted
        AND waiting.database = (SELECT oid FROM pg_database WHERE datname = current_database()))`).Scan(&heldByWaiter); err != nil {
		t.Fatalf("read advisory locks held by the waiting park: %v", err)
	}
	if heldByWaiter != 0 {
		t.Fatalf("the waiting park holds %d advisory locks while it waits on one of %d scopes, want 0", heldByWaiter, len(blocking))
	}
	if err := hold.Rollback(); err != nil {
		t.Fatalf("release the in-flight activation stand-in: %v", err)
	}
	select {
	case err := <-parked:
		if err != nil {
			t.Fatalf("park with %d blocking scopes: %v", len(blocking), err)
		}
	case <-time.After(narrowedParkRendezvousTimeout + 5*time.Second):
		t.Fatalf("the park did not return after the in-flight activation finished")
	}
}
