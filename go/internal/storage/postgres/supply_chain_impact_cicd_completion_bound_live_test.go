// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	completionstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

// cicdBoundFindings is how many settled supply_chain_impact findings items
// exist when the ci_cd pass acknowledges: a small fleet.
const cicdBoundFindings = 30

// TestUnchangedCICDSuccessRePendsNoSupplyChainImpactItemLive is the bound on
// completion re-pends (#10972 reality audit, risk 2; the arbiter's PR-4,
// change-gated ci_cd emission). Thirty supply_chain_impact findings items in
// thirty scopes are settled. A ci_cd_run_correlation pass that wrote nothing
// (CanonicalWrites 0, changed verdict 0) acknowledges through the real ReducerQueue.Ack, whose
// statement emits the completion event (reducer_queue_ack.go
// ackCICDRunCorrelationReducerWorkQuery), and the real completion store claims
// and fans that event out. A ci_cd success that changed nothing gives the
// findings passes no new input, so no findings item may re-pend.
//
// RED before PR-4: the ack emitted an event on every success and the fan-out
// re-pended every settled findings item (30 of 30 on the coordinator's run),
// the 1,318-rows-in-8h shape the QA requeue report measured.
func TestUnchangedCICDSuccessRePendsNoSupplyChainImpactItemLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	stale := now.Add(-time.Hour)
	const owner = "cicd-bound-owner"

	for i := 0; i < cicdBoundFindings; i++ {
		scope := fmt.Sprintf("repository:10972-cicd-bound-%02d", i)
		gen := "generation:" + scope
		seedContainerImageIdentityAckScope(t, ctx, db, scope)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scope, gen)
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'succeeded', 'intent', $1, '{}'::jsonb, $4, $4)`,
			fmt.Sprintf("reducer_10972_cicd_bound_findings_%02d", i), scope, gen, stale); err != nil {
			t.Fatal(err)
		}
	}

	const cicdScope, cicdGen, cicdItem = "repository:10972-cicd-bound-ci", "generation:10972-cicd-bound-ci", "reducer_10972_cicd_bound_ci"
	seedContainerImageIdentityAckScope(t, ctx, db, cicdScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, cicdScope, cicdGen)
	claimedAt := now.Add(-time.Second)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, lease_owner, claim_until, last_attempt_at, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'ci_cd_run_correlation', 'claimed', 'intent', $1, $4, $5, $6, 1, '{}'::jsonb, $7, $7)`,
		cicdItem, cicdScope, cicdGen, owner, now.Add(time.Minute), claimedAt, stale); err != nil {
		t.Fatal(err)
	}
	queue := NewReducerQueue(SQLDB{DB: db}, owner, time.Minute)
	intent := reducercontract.Intent{
		IntentID: cicdItem, ScopeID: cicdScope, GenerationID: cicdGen,
		Domain: reducer.DomainCICDRunCorrelation, AttemptCount: 1, ClaimedAt: &claimedAt,
	}
	if err := queue.Ack(ctx, intent, reducercontract.Result{
		IntentID: cicdItem, Domain: reducer.DomainCICDRunCorrelation,
		Status: reducercontract.ResultStatusSucceeded, CanonicalWrites: 0,
		SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 0},
	}); err != nil {
		t.Fatalf("ack the unchanged ci_cd pass: %v", err)
	}

	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(5 * time.Second) }
	enqueued := 0
	if lease, ok, err := store.Claim(ctx, owner, time.Minute); err != nil {
		t.Fatalf("claim completion event: %v", err)
	} else if ok {
		result, err := store.Fanout(ctx, lease, 500)
		if err != nil {
			t.Fatalf("fan out completion event: %v", err)
		}
		enqueued = result.IntentsEnqueued
	}
	var repended int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
WHERE domain = 'supply_chain_impact' AND work_item_id LIKE 'reducer_10972_cicd_bound_findings_%' AND status <> 'succeeded'`).Scan(&repended); err != nil {
		t.Fatal(err)
	}
	if repended != 0 || enqueued != 0 {
		t.Fatalf("an unchanged ci_cd success re-pended %d of %d settled findings items (fan-out enqueued %d), want 0", repended, cicdBoundFindings, enqueued)
	}
}
