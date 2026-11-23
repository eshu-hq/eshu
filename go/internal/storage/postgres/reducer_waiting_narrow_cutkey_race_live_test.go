// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 cut-key park versus activation race on the production schema: a bounded-prefix narrowed wait parks while every scope in its prefix activates, and the activation wake or the park's rendezvous must reopen it (the proof #11297 left to #11256's lost-wake fix); disposable database per run
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

// TestNarrowedWaitCutKeyRacesActivationLive races a cut-key park against the
// activation of every scope in its prefix. Once all of them committed, the wait
// must be gone and the queue row retrying on its original attempt: the
// activation wake or the park-time rendezvous, never neither. #11297 shipped
// this test and removed it because it lost a wake about 3 runs in 8 through the
// open park-versus-activation window (#11256); the rendezvous closes that
// window, so the proof stands here.
func TestNarrowedWaitCutKeyRacesActivationLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := sequentialHolderIDs(80)
	boundFleet(t, ctx, db, at, holders)
	prefix, _, _ := boundWaitingScopeIDs(holders, narrowedWaitingBlockingBudgetBytes)
	if len(prefix) == 0 || len(prefix) >= len(holders) {
		t.Fatalf("prefix = %d of %d, want a cut", len(prefix), len(holders))
	}
	seedNarrowRetryRow(t, ctx, db, "wait-bw-race", boundWaiterScope, boundWaiterGen,
		"workload_materialization", "workload_materialization_resolution_not_ready", at)
	intent := narrowWaitIntent("wait-bw-race", boundWaiterScope, boundWaiterGen)

	var wg sync.WaitGroup
	errs := make(chan error, len(prefix)+1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := setAsideWaitingForCause(ctx, SQLDB{DB: db}, nil, intent, narrowWaitEntry(),
			narrowCapCause{repos: []string{"repo-bw"}, blocking: holders}, nil, time.Now().UTC()); err != nil {
			errs <- fmt.Errorf("park: %w", err)
		}
	}()
	for _, id := range prefix {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, "gen-"+id, id); err != nil {
				errs <- fmt.Errorf("activate %s: %w", id, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race error: %v", err)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-bw-race"); got != 0 {
		t.Fatalf("wait rows after every prefix scope activated = %d, want 0: a lost wake", got)
	}
	status, attempts, _ := narrowQueueState(t, ctx, db, "wait-bw-race")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("queue row = %s attempts %d, want retrying/1", status, attempts)
	}
}
