// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11104 backward-evidence wake proofs: a deployment_mapping row deferred by the real cross-repo gate reopens when the real deferred-backfill fan-in publishes its generation's exact backward_evidence_committed phase, in both orders, across the publisher/Fail overlap, and through the audit after a missed wake; other phases, generations, and scopes never wake it; disposable database per test, about 20 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestBackwardEvidenceParkThenPublishWakesLive is proof 1: park first (with a
// sibling that succeeded before the park, the CI order), then the real fan-in
// publishes the exact phase; the waiter reopens in the publish. RED on main:
// the class parks as target_committed/audit, the publish wakes only the
// coarse phase_committed key, and the audit proxy never fires because the
// sibling succeeded before the park, so the row sits out the backstop.
func TestBackwardEvidenceParkThenPublishWakesLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC()
	const scopeID, genID = "git-repository-scope:acme/be-1", "be-gen-1"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-1", "be-sib-1", clock)

	parkBackwardEvidenceWaiter(t, ctx, base, &clock)
	// The post-commit re-wake is off here so the reopen must come from the
	// in-tx exact wake inside the publish; proof 4 covers the re-wake.
	fanIn := backwardEvidenceFanIn(base)
	fanIn.WakePhaseWaitsAfterCommit = nil
	publishBackwardEvidence(t, ctx, fanIn, scopeID, genID)

	assertBackwardEvidenceReopened(t, ctx, base, "be-dm-1", 1)
}

// TestBackwardEvidencePublishThenParkReopensLive is proof 2: the handler
// gated before the publish, the publish commits, then Fail parks; the
// park-time check sees the exact phase and reopens at once. RED on main: the
// target_committed park-time check reads sibling progress, not the phase.
func TestBackwardEvidencePublishThenParkReopensLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC()
	const scopeID, genID = "git-repository-scope:acme/be-2", "be-gen-2"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-2", "be-sib-2", clock)

	queue, intent, gateErr := claimGatedBackwardEvidence(t, ctx, base, &clock)
	publishBackwardEvidence(t, ctx, backwardEvidenceFanIn(base), scopeID, genID)
	if err := queue.Fail(ctx, intent, gateErr); err != nil {
		t.Fatalf("fail after publish: %v", err)
	}

	assertBackwardEvidenceReopened(t, ctx, base, "be-dm-2", 1)
}

// TestBackwardEvidenceUnrelatedPublishesStayParkedLive is proof 3: a phase
// for another keyspace/phase, the same phase for another generation of the
// scope, and the exact phase for another scope never wake the waiter, and
// the audit does not reopen it either. Green on main (nothing wakes it
// there); after #11104 it pins the exactness of the key and the predicate.
func TestBackwardEvidenceUnrelatedPublishesStayParkedLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC()
	const scopeID, genID = "git-repository-scope:acme/be-3", "be-gen-3"
	const otherScope, otherScopeGen = "git-repository-scope:acme/be-3-other", "be-gen-3-other"
	const otherGen = "be-gen-3-older"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-3", "be-sib-3", clock)
	addOwnGateGeneration(t, ctx, base, scopeID, otherGen, clock.Add(-2*time.Hour), "superseded")
	seedOwnGateScope(t, ctx, base, otherScope, otherScopeGen, clock.Add(-time.Hour))

	parkBackwardEvidenceWaiter(t, ctx, base, &clock)

	phases := NewGraphProjectionPhaseStateStore(SQLDB{DB: base})
	now := time.Now().UTC()
	rows := []gpphase.PhaseState{
		// Same scope generation, another keyspace and phase.
		{
			Key: gpphase.PhaseKey{
				ScopeID: scopeID, AcceptanceUnitID: scopeID, SourceRunID: genID,
				GenerationID: genID, Keyspace: gpphase.KeyspaceCodeEntitiesUID,
			},
			Phase: gpphase.PhaseCanonicalNodesCommitted, CommittedAt: now, UpdatedAt: now,
		},
		// Same keyspace, another phase.
		{
			Key: gpphase.PhaseKey{
				ScopeID: scopeID, AcceptanceUnitID: scopeID, SourceRunID: genID,
				GenerationID: genID, Keyspace: gpphase.KeyspaceCrossRepoEvidence,
			},
			Phase: gpphase.PhaseDeploymentMapping, CommittedAt: now, UpdatedAt: now,
		},
		// Exact phase, another generation of the same scope.
		{
			Key: gpphase.PhaseKey{
				ScopeID: scopeID, AcceptanceUnitID: scopeID, SourceRunID: otherGen,
				GenerationID: otherGen, Keyspace: gpphase.KeyspaceCrossRepoEvidence,
			},
			Phase: gpphase.PhaseBackwardEvidenceCommitted, CommittedAt: now, UpdatedAt: now,
		},
	}
	if err := phases.Upsert(ctx, rows); err != nil {
		t.Fatalf("publish unrelated phases: %v", err)
	}
	// Exact phase for another scope, through the real fan-in.
	publishBackwardEvidence(t, ctx, backwardEvidenceFanIn(base), otherScope, otherScopeGen)

	assertBackwardEvidenceParked(t, ctx, base, "be-dm-3")
	if _, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, time.Now().UTC(),
		DefaultWaitingAuditInterval, DefaultWaitingAuditBatchSize); err != nil {
		t.Fatalf("audit: %v", err)
	}
	assertBackwardEvidenceParked(t, ctx, base, "be-dm-3")
}

// holdOpenPhaseStore publishes through the real root phase store, then holds
// the publisher's transaction open until released, so a proof can run Fail
// inside the window between the in-tx wake and the commit.
type holdOpenPhaseStore struct {
	inner   backfill.PhaseStore
	inTx    chan<- struct{}
	release <-chan struct{}
}

func (h holdOpenPhaseStore) PublishGraphProjectionPhases(ctx context.Context, rows []gpphase.PhaseState) error {
	if err := h.inner.PublishGraphProjectionPhases(ctx, rows); err != nil {
		return err
	}
	h.inTx <- struct{}{}
	<-h.release
	return nil
}

// holdOpenFanIn wraps the root fan-in store's phase constructor with
// holdOpenPhaseStore.
func holdOpenFanIn(base *sql.DB, inTx chan<- struct{}, release <-chan struct{}) backfill.Store {
	store := backwardEvidenceFanIn(base)
	newPhase := store.NewPhaseStore
	store.NewPhaseStore = func(tx db.Transaction) backfill.PhaseStore {
		return holdOpenPhaseStore{inner: newPhase(tx), inTx: inTx, release: release}
	}
	return store
}

// runOverlap gates the waiter, starts the publisher, runs Fail while the
// publisher's transaction is open after its in-tx wake, then lets it commit.
func runOverlap(t *testing.T, ctx context.Context, base *sql.DB, fanIn backfill.Store, inTx <-chan struct{}, release chan struct{}, scopeID, genID string) {
	t.Helper()
	clock := time.Now().UTC()
	queue, intent, gateErr := claimGatedBackwardEvidence(t, ctx, base, &clock)
	done := make(chan error, 1)
	go func() {
		_, err := fanIn.PublishDeferredBackfillPartitions(ctx,
			map[backfill.ScopeGenerationPartition][]string{
				{ScopeID: scopeID, GenerationID: genID}: {"repository:" + scopeID},
			}, nil, "", 1, nil)
		done <- err
	}()
	select {
	case <-inTx:
	case err := <-done:
		t.Fatalf("publisher finished before its in-tx wake: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("publisher never reached its in-tx wake")
	}
	if err := queue.Fail(ctx, intent, gateErr); err != nil {
		close(release)
		t.Fatalf("fail inside the publisher window: %v", err)
	}
	// The publisher has not committed, so Fail's park-time check cannot
	// see the phase: the waiter is parked when the commit lands.
	state := readWaitingRow(t, ctx, base, intent.IntentID)
	if state.waitRows != 1 {
		close(release)
		t.Fatalf("waiter wait rows inside the window = %d, want 1", state.waitRows)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("publisher: %v", err)
	}
}

// TestBackwardEvidenceOverlapPostCommitRewakeLive is proof 4: the publisher
// runs its in-tx wake (nothing parked yet), Fail parks inside the open
// window, then the publisher commits; the post-commit re-wake reopens the
// waiter. RED on main (no exact wake exists at all).
func TestBackwardEvidenceOverlapPostCommitRewakeLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	const scopeID, genID = "git-repository-scope:acme/be-4", "be-gen-4"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-4", "be-sib-4", time.Now().UTC())

	inTx := make(chan struct{}, 1)
	release := make(chan struct{})
	runOverlap(t, ctx, base, holdOpenFanIn(base, inTx, release), inTx, release, scopeID, genID)

	assertBackwardEvidenceReopened(t, ctx, base, "be-dm-4", 1)
}

// TestBackwardEvidenceMissedWakeAuditReopensLive is proof 5: the durable
// crash residual (phase committed, waiter parked, no wake landed) is
// reopened by the audit's exact-phase predicate. The phase row is written
// directly so no wake of any kind runs, which is exactly the state a
// publisher that crashed between its commit and its re-wake leaves behind.
// RED on main: the audit evaluates the target_committed sibling-progress
// proxy, which the earlier sibling success never satisfies.
func TestBackwardEvidenceMissedWakeAuditReopensLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC()
	const scopeID, genID = "git-repository-scope:acme/be-5", "be-gen-5"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-5", "be-sib-5", clock)

	parkBackwardEvidenceWaiter(t, ctx, base, &clock)
	insertBackwardEvidencePhaseRowNoWake(t, ctx, base, scopeID, genID)
	assertBackwardEvidenceParked(t, ctx, base, "be-dm-5")

	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, time.Now().UTC(),
		DefaultWaitingAuditInterval, DefaultWaitingAuditBatchSize)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if reopened != 1 {
		t.Fatalf("audit reopened = %d, want 1", reopened)
	}
	assertBackwardEvidenceReopened(t, ctx, base, "be-dm-5", 1)
}

// insertBackwardEvidencePhaseRowNoWake commits the exact phase row with no
// wake statement, the durable state a crash after the publish commit leaves.
func insertBackwardEvidencePhaseRowNoWake(t *testing.T, ctx context.Context, base *sql.DB, scopeID, genID string) {
	t.Helper()
	if _, err := base.ExecContext(ctx, `
INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at)
VALUES ($1, $1, $2, $2, 'cross_repo_evidence', 'backward_evidence_committed', now(), now())`,
		scopeID, genID); err != nil {
		t.Fatalf("insert phase row: %v", err)
	}
}

// TestBackwardEvidenceWokenRowConvergesLive is proof 6: the woken row is
// claimable now, passes the real handler gate, and succeeds on that attempt.
// RED on main: the row is still parked, so the claim finds nothing.
func TestBackwardEvidenceWokenRowConvergesLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC()
	const scopeID, genID = "git-repository-scope:acme/be-6", "be-gen-6"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-6", "be-sib-6", clock)

	parkBackwardEvidenceWaiter(t, ctx, base, &clock)
	publishBackwardEvidence(t, ctx, backwardEvidenceFanIn(base), scopeID, genID)

	clock = time.Now().UTC().Add(time.Second)
	queue := newOwnGateQueue(base, &clock, reducer.DomainDeploymentMapping)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim woken waiter: ok=%v err=%v (still parked on the backstop?)", ok, err)
	}
	if intent.IntentID != "be-dm-6" {
		t.Fatalf("claimed %s, want be-dm-6", intent.IntentID)
	}
	if _, err := backwardEvidenceHandler(base).Resolve(ctx, intent.ScopeID, intent.GenerationID); err != nil {
		t.Fatalf("woken handler run: %v (the gate must pass once the phase committed)", err)
	}
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack woken waiter: %v", err)
	}
	state := readWaitingRow(t, ctx, base, "be-dm-6")
	if state.status != "succeeded" {
		t.Fatalf("converged status = %q, want succeeded", state.status)
	}
	if state.waitRows != 0 {
		t.Fatalf("converged wait rows = %d, want 0", state.waitRows)
	}
}

// TestBackwardEvidenceOverlapWithoutRewakeStrandsUntilAuditLive is the
// mutation half of proof 4 and the faithful crash residual of proof 5: with
// the post-commit re-wake removed (a publisher that died after its commit),
// the overlap leaves the waiter parked on the backstop, and only the audit's
// exact-phase predicate reopens it.
func TestBackwardEvidenceOverlapWithoutRewakeStrandsUntilAuditLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	const scopeID, genID = "git-repository-scope:acme/be-7", "be-gen-7"
	seedBackwardEvidenceScope(t, ctx, base, scopeID, genID, "be-dm-7", "be-sib-7", time.Now().UTC())

	inTx := make(chan struct{}, 1)
	release := make(chan struct{})
	fanIn := holdOpenFanIn(base, inTx, release)
	fanIn.WakePhaseWaitsAfterCommit = nil
	runOverlap(t, ctx, base, fanIn, inTx, release, scopeID, genID)

	assertBackwardEvidenceParked(t, ctx, base, "be-dm-7")
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, time.Now().UTC(),
		DefaultWaitingAuditInterval, DefaultWaitingAuditBatchSize)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if reopened != 1 {
		t.Fatalf("audit reopened = %d, want 1", reopened)
	}
	assertBackwardEvidenceReopened(t, ctx, base, "be-dm-7", 1)
}
