// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7597 a refinalize (real reset statements) retires the relationship generation and deletes the succeeded producer; the deferring consumer keeps the non-counting class through re-resolution, and the re-run producer's activation reopens an already dead-lettered consumer and lets the other succeed; isolated schema, about 2 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
)

// TestOwnGateWaitsThroughRefinalizeReResolutionLive is RED before the #7597
// re-resolution rule: a refinalize (the real reset statements) deletes the
// succeeded deployment_mapping row and retires the relationship generation,
// so a consumer that had been deferring for hours saw "no producer" past the
// 30-minute bound and dead-lettered before the projector re-enqueued its
// producer. Now a relationship generation row that exists and is not active
// keeps the consumer deferring without a bound; the re-run producer's
// activation lets it succeed, and reopens a consumer that had already
// dead-lettered on the dead producer.
func TestOwnGateWaitsThroughRefinalizeReResolutionLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 refinalize proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	clock := time.Now().UTC()
	const scope, gen = "git-repository-scope:acme/app", "gen-1"
	seedOwnGateScope(t, ctx, db, scope, gen, clock.Add(-3*time.Hour))
	enqueue := NewReducerQueue(SQLDB{DB: db}, "refinalize-7597-seed", time.Minute)
	intent := func(domain reducercontract.Domain, entity string) runtime.ReducerIntent {
		return runtime.ReducerIntent{
			ScopeID: scope, GenerationID: gen, Domain: domain, EntityKey: entity,
			Reason: "refinalize #7597 proof", FactID: "fact-" + entity, SourceSystem: "git",
		}
	}
	dm := intent(reducer.DomainDeploymentMapping, "dm")
	wm := intent(reducer.DomainWorkloadMaterialization, "repo:repo-edge-api")
	duc := intent(reducer.DomainDeployableUnitCorrelation, "repo:repo-edge-api")
	if _, err := enqueue.Enqueue(ctx, []runtime.ReducerIntent{dm, wm, duc}); err != nil {
		t.Fatalf("seed reducer work: %v", err)
	}
	dmID, wmID, ducID := reducerWorkItemID(dm), reducerWorkItemID(wm), reducerWorkItemID(duc)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET created_at = $2, updated_at = $2 WHERE scope_id = $1`,
		scope, clock.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	setWakeRow(t, ctx, db, dmID, "succeeded", "", 1)
	setWakeRow(t, ctx, db, wmID, "retrying", reducer.WorkloadMaterializationResolutionNotReadyFailureClass, 1)
	setWakeRow(t, ctx, db, ducID, "dead_letter", crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass, 3)
	store := NewRelationshipStore(SQLDB{DB: db})
	if err := store.ActivateResolutionGeneration(ctx, gen, scope); err != nil {
		t.Fatalf("seed the active relationship generation: %v", err)
	}
	// The seed activation woke the dead consumer; put it back for the proof.
	setWakeRow(t, ctx, db, ducID, "dead_letter", crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass, 3)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET reopened_at = NULL WHERE work_item_id = $1`, ducID); err != nil {
		t.Fatal(err)
	}

	if _, err := NewRecoveryStore(SQLDB{DB: db}).RefinalizeScopeProjections(
		ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scope}}, clock); err != nil {
		t.Fatalf("refinalize: %v", err)
	}
	var dmRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id = $1`, dmID).Scan(&dmRows); err != nil {
		t.Fatal(err)
	}
	if status := relationshipGenerationStatus(t, ctx, db, gen); dmRows != 0 || status != "superseded" {
		t.Fatalf("refinalize left dm rows=%d, relationship generation %q; want the producer row deleted and the generation retired",
			dmRows, status)
	}

	consumer := consumerFor(db, reducer.DomainWorkloadMaterialization)
	wmq := newOwnGateQueue(db, &clock, reducer.DomainWorkloadMaterialization)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET visible_at = $2 WHERE work_item_id = $1`, wmID, clock); err != nil {
		t.Fatal(err)
	}
	for cycle := 1; cycle <= 5; cycle++ {
		claimed, ok, err := wmq.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("cycle %d claim: ok=%v err=%v", cycle, ok, err)
		}
		if err := wmq.Fail(ctx, claimed, consumer.run(ctx, claimed)); err != nil {
			t.Fatalf("cycle %d fail: %v", cycle, err)
		}
		st, cls, msg, att, vis := readProducerTerminalRow(t, ctx, db, wmID)
		t.Logf("cycle %d: status=%s class=%s attempts=%d", cycle, st, cls, att)
		if st != "retrying" || cls != reducer.WorkloadMaterializationResolutionNotReadyFailureClass || att != 1 {
			t.Fatalf("cycle %d during re-resolution: status=%s class=%s attempts=%d msg=%q; want non-counting retrying",
				cycle, st, cls, att, msg)
		}
		if !strings.Contains(msg, "re-resolution in progress") {
			t.Fatalf("cycle %d message = %q, want it to say re-resolution is in progress", cycle, msg)
		}
		clock = vis.Add(time.Second)
	}

	// The projector re-enqueues the producer; it runs and activates.
	if _, err := enqueue.Enqueue(ctx, []runtime.ReducerIntent{dm}); err != nil {
		t.Fatalf("re-enqueue deployment_mapping: %v", err)
	}
	dmq := newOwnGateQueue(db, &clock, reducer.DomainDeploymentMapping)
	claimedDM, ok, err := dmq.Claim(ctx)
	if err != nil || !ok || claimedDM.IntentID != dmID {
		t.Fatalf("claim re-enqueued deployment_mapping: ok=%v id=%s err=%v", ok, claimedDM.IntentID, err)
	}
	if err := store.ActivateResolutionGenerationForClaim(ctx, gen, scope, claimedDM.IntentID, *claimedDM.ClaimedAt); err != nil {
		t.Fatalf("activate re-resolved generation: %v", err)
	}
	if err := dmq.Ack(ctx, claimedDM, reducercontract.Result{IntentID: dmID, Domain: claimedDM.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack deployment_mapping: %v", err)
	}
	if got := readWakeRow(t, ctx, db, ducID); got.status != "pending" || !got.reopened.Valid {
		t.Fatalf("already dead-lettered consumer = %+v, want woken by the re-resolution activation", got)
	}
	claimed, ok, err := wmq.Claim(ctx)
	if err != nil || !ok || claimed.IntentID != wmID {
		t.Fatalf("claim deferring consumer: ok=%v id=%s err=%v", ok, claimed.IntentID, err)
	}
	if err := consumer.run(ctx, claimed); err != nil {
		t.Fatalf("consumer after re-resolution: %v", err)
	}
}
