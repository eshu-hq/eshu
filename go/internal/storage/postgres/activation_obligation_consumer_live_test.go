// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sync"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationConsumerOrderingLive drives the real ingestion,
// projector and reducer queue APIs in both orders of activation and the
// control-arm maintenance, and stops at native claimability of the woken row.
func TestActivationObligationConsumerOrderingLive(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		maintenanceBeforeActivation bool
	}{
		{"maintenance_before_activation", true},
		{"activation_before_maintenance", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database, store, sourceWork, targetWork := setupActivationConsumer(t, "activation_order")
			queue := NewProjectorQueue(SQLDB{DB: database}, "7584-consumer-projector", time.Minute)
			if err := queue.Ack(ctx, sourceWork, projectorruntime.Result{}); err != nil {
				t.Fatalf("source Ack: %v", err)
			}
			if tc.maintenanceBeforeActivation {
				if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
					t.Fatalf("maintenance before target Ack: %v", err)
				}
				assertActivationBackwardPhase(t, ctx, database, targetWork, false)
			}
			if err := queue.Ack(ctx, targetWork, projectorruntime.Result{}); err != nil {
				t.Fatalf("target Ack: %v", err)
			}
			assertExactActivationObligation(t, ctx, database, targetWork,
				"accepted target Ack did not create obligation")
			reducerQueue := NewReducerQueue(SQLDB{DB: database}, "7584-consumer-reducer", time.Minute)
			reducerQueue.RetryDelay = time.Minute
			reducerQueue.Now = activationDatabaseClock(t, ctx, database)
			reducerQueue.ClaimDomains = []reducer.Domain{reducer.DomainDeploymentMapping}
			if _, err := reducerQueue.Enqueue(ctx, []projectorruntime.ReducerIntent{{
				ScopeID: targetWork.Scope.ScopeID, GenerationID: targetWork.Generation.GenerationID,
				Domain: reducer.DomainDeploymentMapping, SourceSystem: "git",
				Reason: "consumer readiness probe",
			}}); err != nil {
				t.Fatalf("native reducer Enqueue: %v", err)
			}
			targetIntent := claimTargetReducerIntent(t, ctx, reducerQueue,
				targetWork.Scope.ScopeID, targetWork.Generation.GenerationID)
			if err := reducerQueue.Fail(ctx, targetIntent, crossrepo.BackwardEvidenceNotReadyError{
				ScopeID: targetWork.Scope.ScopeID, GenerationID: targetWork.Generation.GenerationID,
			}); err != nil {
				t.Fatalf("native reducer Fail: %v", err)
			}
			assertActivationTargetRetry(t, ctx, database, targetWork.Scope.ScopeID,
				targetWork.Generation.GenerationID, "retrying",
				crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass)
			if !tc.maintenanceBeforeActivation {
				if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
					t.Fatalf("maintenance after target Ack: %v", err)
				}
				assertActivationBackwardPhase(t, ctx, database, targetWork, true)
			}
			obligations := activation.NewStore(SQLDB{DB: database})
			target := claimActivationObligation(t, ctx, obligations, "7584-consumer-owner", time.Minute,
				targetWork.Scope.ScopeID, targetWork.Generation.GenerationID)
			if tc.maintenanceBeforeActivation {
				// Without maintenance after activation the exact phase is
				// absent, so Finalize must refuse and write nothing.
				result, err := obligations.Finalize(ctx, *target)
				if err != nil || result.Outcome != activation.OutcomePhaseNotReady {
					t.Fatalf("finalize before maintenance = %+v err=%v, want phase_not_ready", result, err)
				}
				if err := store.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
					t.Fatalf("consumer control-arm maintenance: %v", err)
				}
				assertActivationBackwardPhase(t, ctx, database, targetWork, true)
			}
			result, err := obligations.Finalize(ctx, *target)
			if err != nil || result.Outcome != activation.OutcomeCompleted || result.Woken != 1 {
				t.Fatalf("finalize = %+v err=%v, want completed with one wake", result, err)
			}
			assertActivationTargetRetry(t, ctx, database, targetWork.Scope.ScopeID,
				targetWork.Generation.GenerationID, "retrying",
				crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass)
			reclaimed := claimTargetReducerIntent(t, ctx, reducerQueue,
				targetWork.Scope.ScopeID, targetWork.Generation.GenerationID)
			if reclaimed.IntentID != targetIntent.IntentID {
				t.Fatalf("woke %s, want %s", reclaimed.IntentID, targetIntent.IntentID)
			}
		})
	}
}

// TestActivationObligationConsumerLateFailureLive: a handler that read
// readiness before the phase committed is still claimed when the phase lands.
// Finalize must keep the obligation open until that handler's late Fail, then
// wake exactly that row.
func TestActivationObligationConsumerLateFailureLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_late", true)
	intent := f.enqueueClaim(t, reducer.DomainDeploymentMapping, f.scope, f.gen, "late-failure")
	// The handler's cached readiness read: the phase is absent.
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, false)
	obligation := f.claimObligation(t, "7584-late-consumer", time.Minute, f.gen)
	f.maintenance(t)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)
	result, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || result.Outcome != activation.OutcomeWorkPending {
		t.Fatalf("finalize while handler claimed = %+v err=%v, want work_pending", result, err)
	}
	if err := f.reducerQ.Fail(f.ctx, intent, crossrepo.BackwardEvidenceNotReadyError{
		ScopeID: intent.ScopeID, GenerationID: intent.GenerationID,
	}); err != nil {
		t.Fatal(err)
	}
	assertActivationTargetRetry(t, f.ctx, f.db, intent.ScopeID, intent.GenerationID, "retrying",
		crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass)
	if done, err := f.finalize(obligation); err != nil || !done {
		t.Fatalf("late failure wake complete=%v err=%v", done, err)
	}
	f.mustClaimable(t, intent.IntentID)
}

// TestActivationObligationConsumerClaimRecoveryLive: two owners claim two
// distinct obligations in parallel; after the real lease expires a restarted
// owner reclaims with a higher token and the expired owner cannot finish.
func TestActivationObligationConsumerClaimRecoveryLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_recovery", true)
	f.maintenance(t)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)
	type outcome struct {
		work *activation.Obligation
		err  error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, owner := range []string{"parallel-a", "parallel-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			<-start
			work, err := f.oblig.Claim(f.ctx, owner, 150*time.Millisecond)
			results <- outcome{work, err}
		}(owner)
	}
	close(start)
	wg.Wait()
	close(results)
	var claims []*activation.Obligation
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.work == nil {
			t.Fatal("independent eligible obligation not claimed")
		}
		claims = append(claims, result.work)
	}
	if claims[0].ScopeID == claims[1].ScopeID && claims[0].GenerationID == claims[1].GenerationID {
		t.Fatal("parallel owners received one obligation")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var expired bool
		if err := f.db.QueryRowContext(f.ctx,
			"SELECT bool_and(lease_until<=clock_timestamp()) FROM activation_obligations WHERE state='leased'").Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real leases did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	reclaim, err := f.oblig.Claim(f.ctx, "parallel-a", time.Minute)
	if err != nil || reclaim == nil {
		t.Fatalf("restart Claim work=%v err=%v", reclaim, err)
	}
	var stale *activation.Obligation
	for _, work := range claims {
		if work.ScopeID == reclaim.ScopeID && work.GenerationID == reclaim.GenerationID {
			stale = work
		}
	}
	if stale == nil || reclaim.LeaseToken <= stale.LeaseToken {
		t.Fatal("reclaim did not advance original token")
	}
	if done, err := finalizeActivation(f.ctx, f.oblig, stale); err != nil || done {
		t.Fatalf("expired owner completion=%v err=%v", done, err)
	}
	var state string
	var token int64
	if err := f.db.QueryRowContext(f.ctx,
		"SELECT state,claim_token FROM activation_obligations WHERE scope_id=$1 AND generation_id=$2",
		reclaim.ScopeID, reclaim.GenerationID).Scan(&state, &token); err != nil {
		t.Fatal(err)
	}
	if state != "leased" || token != reclaim.LeaseToken {
		t.Fatal("stale owner changed reclaimed state")
	}
}
