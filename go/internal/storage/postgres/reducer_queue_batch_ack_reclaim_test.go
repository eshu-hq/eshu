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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// #6162: the Ifa expire-lease fault injection cell forces
// claim_until = now() on every claimed reducer row without killing the
// handler, so the claimer legitimately re-claims a work item whose first
// handler is still in flight. Both handlers then finish and hand the acker
// two ack items for one work_item_id under two claim identities.
//
// splitReducerAckBatchIntents used to call that a conflict and return a hard
// error, which discarded the whole batch (including the winner's ack) and,
// because the error does not wrap reducer.ErrExecutionClaimRejected, drove
// the batch acker's fatal appendErr/cancel path instead of its claim-rejected
// path. The reducer exited, the row stayed 'claimed' with nothing alive to
// reclaim it, and the drain gate ran out its bound. Run 35617012182,
// shard 2/4.
//
// The batch must survive: keep the newest claim, drop the superseded one, and
// report the supersession as a claim rejection.
func TestSplitReducerAckBatchIntentsKeepsNewestClaimAfterLeaseReclaim(t *testing.T) {
	t.Parallel()

	const intentID = "reducer_gcp_project_supply-chain-demo-project_cassette-gcp-scd-gen1_gcp_resource_materialization"
	staleClaim := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	freshClaim := staleClaim.Add(900 * time.Millisecond)

	split, err := splitReducerAckBatchIntents([]reducer.Intent{
		{
			IntentID:     intentID,
			Domain:       reducer.DomainGCPResourceMaterialization,
			AttemptCount: 1,
			ClaimedAt:    &staleClaim,
		},
		{
			IntentID:     intentID,
			Domain:       reducer.DomainGCPResourceMaterialization,
			AttemptCount: 2,
			ClaimedAt:    &freshClaim,
		},
	})
	if err != nil {
		t.Fatalf("splitReducerAckBatchIntents() error = %v, want nil: a lease reclaim is expected, not a conflict", err)
	}
	if got, want := len(split.unrelated), 1; got != want {
		t.Fatalf("unrelated intent count = %d, want %d", got, want)
	}
	if got := split.unrelated[0].ClaimedAt; got == nil || !got.Equal(freshClaim) {
		t.Fatalf("kept claim = %v, want the newest claim %v", got, freshClaim)
	}
	if got, want := split.unrelated[0].AttemptCount, 2; got != want {
		t.Fatalf("kept attempt count = %d, want %d", got, want)
	}
	if got, want := split.supersededClaims, 1; got != want {
		t.Fatalf("superseded claim count = %d, want %d", got, want)
	}
}

// An identical duplicate is not a supersession: nothing was dropped that had
// a result of its own, so it must not be reported as a claim rejection.
func TestSplitReducerAckBatchIntentsDedupesIdenticalClaimWithoutSupersession(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	intent := reducer.Intent{
		IntentID:  "intent-6162-identical",
		Domain:    reducer.DomainOwnership,
		ClaimedAt: &claimedAt,
	}

	split, err := splitReducerAckBatchIntents([]reducer.Intent{intent, intent})
	if err != nil {
		t.Fatalf("splitReducerAckBatchIntents() error = %v, want nil", err)
	}
	if got, want := len(split.unrelated), 1; got != want {
		t.Fatalf("unrelated intent count = %d, want %d", got, want)
	}
	if got, want := split.supersededClaims, 0; got != want {
		t.Fatalf("superseded claim count = %d, want %d", got, want)
	}
}

// Negative control. A work item id encodes its domain, so the same id under
// two domains is an invariant violation rather than a lease race, and must
// stay an error. Without this the change would widen into "accept anything
// duplicated".
func TestSplitReducerAckBatchIntentsRejectsDomainConflict(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)

	_, err := splitReducerAckBatchIntents([]reducer.Intent{
		{IntentID: "intent-6162-domain-conflict", Domain: reducer.DomainOwnership, ClaimedAt: &claimedAt},
		{IntentID: "intent-6162-domain-conflict", Domain: reducer.DomainGovernance, ClaimedAt: &claimedAt},
	})
	if err == nil {
		t.Fatal("splitReducerAckBatchIntents() error = nil, want a domain-conflict error")
	}
	if !strings.Contains(err.Error(), "conflicting domains") {
		t.Fatalf("splitReducerAckBatchIntents() error = %v, want it to name the domain conflict", err)
	}
	if errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatal("a domain conflict must not be reported as a claim rejection")
	}
}

// End of the same path through the exported surface: AckBatch must ack the
// surviving claim and report the dropped one as ErrReducerClaimRejected, which
// wraps reducer.ErrExecutionClaimRejected. That wrapping is the discriminator
// the batch acker uses to log and continue instead of cancelling the run.
func TestReducerQueueAckBatchReportsSupersededClaimAsClaimRejected(t *testing.T) {
	t.Parallel()

	const intentID = "intent-6162-ackbatch-reclaim"
	staleClaim := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	freshClaim := staleClaim.Add(900 * time.Millisecond)

	database := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}}}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-6162",
		LeaseDuration: time.Minute,
	}

	err := queue.AckBatch(
		context.Background(),
		[]reducer.Intent{
			{
				IntentID:     intentID,
				Domain:       reducer.DomainGCPResourceMaterialization,
				AttemptCount: 1,
				ClaimedAt:    &staleClaim,
			},
			{
				IntentID:     intentID,
				Domain:       reducer.DomainGCPResourceMaterialization,
				AttemptCount: 2,
				ClaimedAt:    &freshClaim,
			},
		},
		nil,
	)
	if !errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatalf("AckBatch() error = %v, want an error wrapping reducer.ErrExecutionClaimRejected", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("AckBatch() exec count = %d, want %d: the surviving claim must still be acked", got, want)
	}
	ids, ok := database.execs[0].args[2].([]string)
	if !ok {
		t.Fatalf("AckBatch() id argument type = %T, want []string", database.execs[0].args[2])
	}
	if got, want := len(ids), 1; got != want {
		t.Fatalf("AckBatch() acked id count = %d, want %d", got, want)
	}
	claimedAts, ok := database.execs[0].args[3].([]time.Time)
	if !ok {
		t.Fatalf("AckBatch() claimed_at argument type = %T, want []time.Time", database.execs[0].args[3])
	}
	if got, want := len(claimedAts), 1; got != want {
		t.Fatalf("AckBatch() acked claim count = %d, want %d", got, want)
	}
	if !claimedAts[0].Equal(freshClaim) {
		t.Fatalf("AckBatch() acked claim = %v, want the newest claim %v", claimedAts[0], freshClaim)
	}
}

// A reclaim duplicates the ack AND its result. The surviving claim is the
// newest one, so the result that decides the value-flow refresh emit gate
// (#6785) must be the newest claim's result too. Pairing the newest intent
// with the superseded handler's result silently drops a completion event
// whenever the superseded run wrote nothing and the winning run did -- the
// exact outcome value_flow_refresh_ack.go's fail-open comment calls the worse
// one.
func TestReducerQueueAckBatchPairsTheSurvivingClaimsResult(t *testing.T) {
	t.Parallel()

	const intentID = "intent-6162-refresh-producer-reclaim"
	staleClaim := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	freshClaim := staleClaim.Add(900 * time.Millisecond)

	database := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}}}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-6162",
		LeaseDuration: time.Minute,
	}

	err := queue.AckBatch(
		context.Background(),
		[]reducer.Intent{
			{
				IntentID:     intentID,
				Domain:       reducer.DomainWorkloadMaterialization,
				AttemptCount: 1,
				ClaimedAt:    &staleClaim,
			},
			{
				IntentID:     intentID,
				Domain:       reducer.DomainWorkloadMaterialization,
				AttemptCount: 2,
				ClaimedAt:    &freshClaim,
			},
		},
		// The superseded run wrote nothing; the surviving run wrote rows.
		[]reducer.Result{
			{IntentID: intentID, CanonicalWrites: 0},
			{IntentID: intentID, CanonicalWrites: 7},
		},
	)
	if !errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatalf("AckBatch() error = %v, want a claim rejection", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("AckBatch() exec count = %d, want %d", got, want)
	}

	// ackValueFlowRefreshProducerReducerWorkBatchQuery binds the emit ids last.
	args := database.execs[0].args
	emitIDs, ok := args[len(args)-1].([]string)
	if !ok {
		t.Fatalf("emit id argument type = %T, want []string", args[len(args)-1])
	}
	if len(emitIDs) != 1 || emitIDs[0] != intentID {
		t.Fatalf(
			"emit ids = %v, want [%s]: the surviving claim wrote 7 canonical rows, so its refresh must emit",
			emitIDs, intentID,
		)
	}
}

// A mixed-domain batch is the normal shape for this acker, and it is where the
// supersession signal used to disappear. The container-image block assigned
// claimRejected outright instead of accumulating into it, so a batch holding a
// superseded claim in one domain AND a container-image sub-batch that acked
// cleanly reported success: AckBatch returned nil, the batch acker never
// reached logReducerAckClaimRejected, and the lease race #6162 exists to make
// visible went unrecorded again.
//
// Found by independent review after #6926 merged. The overwrite predates that
// PR, but seeding claimRejected from supersededClaims is what put the fix's own
// signal in its path.
func TestReducerQueueAckBatchKeepsSupersededSignalBesideCleanTargetAck(t *testing.T) {
	t.Parallel()

	const supersededID = "intent-6162-mixed-superseded"
	staleClaim := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	freshClaim := staleClaim.Add(900 * time.Millisecond)

	// Both sub-batches ack every row they are given, so nothing but the
	// supersession can set claimRejected.
	database := &fakeExecQueryer{execResults: []sql.Result{
		rowsAffectedResult{rowsAffected: 1},
		rowsAffectedResult{rowsAffected: 1},
	}}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-6162",
		LeaseDuration: time.Minute,
	}

	err := queue.AckBatch(
		context.Background(),
		[]reducer.Intent{
			{
				IntentID:     "intent-6162-mixed-target",
				Domain:       reducer.DomainContainerImageIdentity,
				AttemptCount: 1,
				ClaimEpoch:   7,
				ClaimedAt:    &freshClaim,
			},
			{
				IntentID:     supersededID,
				Domain:       reducer.DomainGCPResourceMaterialization,
				AttemptCount: 1,
				ClaimedAt:    &staleClaim,
			},
			{
				IntentID:     supersededID,
				Domain:       reducer.DomainGCPResourceMaterialization,
				AttemptCount: 2,
				ClaimedAt:    &freshClaim,
			},
		},
		nil,
	)
	if !errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatalf(
			"AckBatch() error = %v, want a claim rejection: a clean container-image ack must not erase the superseded claim",
			err,
		)
	}
	if got, want := len(database.execs), 2; got != want {
		t.Fatalf("AckBatch() exec count = %d, want %d", got, want)
	}
}
