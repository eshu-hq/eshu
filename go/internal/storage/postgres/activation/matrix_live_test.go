// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestActivationObligationConsumerQueueIsolationLive: Finalize wakes only the
// exact (scope, generation, deployment_mapping, not-ready class) row. Every
// row that needs a reducer Claim exists before the target Ack, and no reducer
// Claim runs between Ack and Finalize, so the old-generation row stays
// observable. N3: a frozen row of another domain in the same scope and
// generation, carrying the same not-ready class, must not move either.
func TestActivationObligationConsumerQueueIsolationLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_isolation", false)
	old := f.notReady(t, f.scope, f.oldGen, "old-gen")
	wake := f.notReady(t, f.scope, f.gen, "wake")
	other := f.enqueueClaim(t, reducer.DomainDeploymentMapping, f.scope, f.gen, "other-class")
	if err := f.reducerQ.Fail(f.ctx, other, activationRetryable{}); err != nil {
		t.Fatal(err)
	}
	f.requireOtherClassRetrying(t, other.IntentID)
	otherDomain := f.notReadyDomain(t, reducer.DomainCodeImportRepoEdge, f.scope, f.gen, "other-domain")
	wrongScope := f.notReady(t, f.source, "gen-consumer-source", "wrong-scope")
	pq := postgres.NewProjectorQueue(postgres.SQLDB{DB: f.db}, "7584-consumer-projector", time.Minute)
	if err := pq.Ack(f.ctx, f.target, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	f.maintenance(t)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)
	ids := map[string]string{
		"old": old, "other": other.IntentID, "otherDomain": otherDomain, "wrongScope": wrongScope,
	}
	frozen := map[string]map[string]any{}
	for name, id := range ids {
		frozen[name] = f.row(t, id, false)
		if frozen[name]["status"] != "retrying" {
			t.Fatalf("%s fixture status=%v, want retrying", name, frozen[name]["status"])
		}
	}
	if frozen["otherDomain"]["failure_class"] != frozen["old"]["failure_class"] {
		t.Fatalf("other-domain fixture class=%v, want the wake class %v",
			frozen["otherDomain"]["failure_class"], frozen["old"]["failure_class"])
	}
	before := f.row(t, wake, true)
	obligation := f.claimObligation(t, "isolation-owner", time.Minute, f.gen)
	if done, err := f.finalize(obligation); err != nil || !done {
		t.Fatalf("finalize done=%v err=%v", done, err)
	}
	for name, id := range ids {
		f.mustEqual(t, name+" work", id, frozen[name])
	}
	if after := f.row(t, wake, true); !reflect.DeepEqual(before, after) {
		t.Fatalf("wake changed more than visibility:\n before=%v\n after=%v", before, after)
	}
	f.mustClaimable(t, wake)
}

// TestActivationObligationConsumerTerminalPreservedLive: terminal work created
// after maintenance (which would otherwise reopen it) keeps every column.
func TestActivationObligationConsumerTerminalPreservedLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_terminal", true)
	wake := f.notReady(t, f.scope, f.gen, "wake")
	f.maintenance(t)
	terminal := f.enqueueClaim(t, reducer.DomainDeploymentMapping, f.scope, f.gen, "terminal")
	if err := f.reducerQ.Ack(f.ctx, terminal, reducer.Result{IntentID: terminal.IntentID, Domain: terminal.Domain}); err != nil {
		t.Fatal(err)
	}
	frozen := f.row(t, terminal.IntentID, false)
	if frozen["status"] != "succeeded" {
		t.Fatalf("terminal fixture status=%v, want succeeded", frozen["status"])
	}
	obligation := f.claimObligation(t, "terminal-owner", time.Minute, f.gen)
	if done, err := f.finalize(obligation); err != nil || !done {
		t.Fatalf("finalize done=%v err=%v", done, err)
	}
	f.mustEqual(t, "terminal work", terminal.IntentID, frozen)
	f.mustClaimable(t, wake)
}

// TestActivationObligationConsumerRollbackAndIdentityLive: forged identities
// change nothing, and a completion that fails after the wake ran rolls the
// wake back with it.
func TestActivationObligationConsumerRollbackAndIdentityLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_rollback", true)
	wake := f.notReady(t, f.scope, f.gen, "wake")
	f.maintenance(t)
	obligation := f.claimObligation(t, "rollback-owner", time.Minute, f.gen)
	state := f.digest(t)
	for name, forged := range map[string]activation.Obligation{
		"unknown generation identity": {ScopeID: f.scope, GenerationID: "gen-consumer-other", LeaseOwner: obligation.LeaseOwner, LeaseToken: obligation.LeaseToken},
		"unknown scope identity":      {ScopeID: f.source, GenerationID: f.gen, LeaseOwner: obligation.LeaseOwner, LeaseToken: obligation.LeaseToken},
		"wrong token":                 {ScopeID: f.scope, GenerationID: f.gen, LeaseOwner: obligation.LeaseOwner, LeaseToken: obligation.LeaseToken + 1},
		"wrong owner":                 {ScopeID: f.scope, GenerationID: f.gen, LeaseOwner: "someone-else", LeaseToken: obligation.LeaseToken},
	} {
		forged := forged
		if done, err := f.finalize(&forged); err != nil || done {
			t.Fatalf("%s done=%v err=%v", name, done, err)
		}
		if got := f.digest(t); got != state {
			t.Fatalf("%s changed durable state", name)
		}
	}
	// Fault injection on the isolated schema: fail the completion write after
	// the wake already ran in the same transaction.
	for _, ddl := range []string{
		`CREATE FUNCTION fail_activation_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected completion failure'; END $$`,
		`CREATE TRIGGER fail_activation_completion BEFORE UPDATE ON activation_obligations FOR EACH ROW WHEN (NEW.state='completed') EXECUTE FUNCTION fail_activation_completion()`,
	} {
		if _, err := f.db.ExecContext(f.ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	done, err := f.finalize(obligation)
	var pgErr *pgconn.PgError
	if done || !errors.As(err, &pgErr) || pgErr.Code != "P0001" || pgErr.Message != "injected completion failure" {
		t.Fatalf("injected failure done=%v err=%v", done, err)
	}
	if got := f.digest(t); got != state {
		t.Fatal("wake survived a failed completion")
	}
	if _, err := f.db.ExecContext(f.ctx, "DROP TRIGGER fail_activation_completion ON activation_obligations"); err != nil {
		t.Fatal(err)
	}
	if done, err = f.finalize(obligation); err != nil || !done {
		t.Fatalf("retry after rollback done=%v err=%v", done, err)
	}
	f.mustClaimable(t, wake)
}

// TestActivationObligationConsumerNoPhaseSubstitutionLive: a backward phase
// of another generation of the same scope never satisfies the target.
// Maintenance runs while the old generation is still active.
func TestActivationObligationConsumerNoPhaseSubstitutionLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_substitution", false)
	f.maintenance(t)
	var otherPhases int
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM graph_projection_phase_state
WHERE scope_id=$1 AND generation_id=$2 AND keyspace='cross_repo_evidence' AND phase='backward_evidence_committed'`,
		f.scope, f.oldGen).Scan(&otherPhases); err != nil || otherPhases == 0 {
		t.Fatalf("fixture needs an old-generation backward phase: count=%d err=%v", otherPhases, err)
	}
	pq := postgres.NewProjectorQueue(postgres.SQLDB{DB: f.db}, "7584-consumer-projector", time.Minute)
	if err := pq.Ack(f.ctx, f.target, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	wake := f.notReady(t, f.scope, f.gen, "wake")
	obligation := f.claimObligation(t, "unready-owner", time.Minute, f.gen)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, false)
	state, row := f.digest(t), f.row(t, wake, false)
	result, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || result.Outcome != activation.OutcomePhaseNotReady {
		t.Fatalf("finalize with only another generation's phase = %+v err=%v", result, err)
	}
	if f.digest(t) != state {
		t.Fatal("another generation's phase changed durable state")
	}
	f.mustEqual(t, "waiting work", wake, row)
}

// TestActivationObligationConsumerSupersessionLive: once the scope moves to a
// newer generation, the claimed obligation retires as obsolete without waking
// the superseded generation's work, and the successor owns its own
// obligation.
func TestActivationObligationConsumerSupersessionLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_supersession", true)
	wake := f.notReady(t, f.scope, f.gen, "wake")
	obligation := f.claimObligation(t, "super-owner", time.Minute, f.gen)
	row := f.row(t, wake, false)
	newer := testfixtures.ActivationRepositoryFact("fact-consumer-target-new", f.scope, "gen-consumer-target-new",
		"repo-consumer-target", "https://github.com/acme/payments-deploy.git")
	newer.ObservedAt = newer.ObservedAt.Add(2 * time.Hour)
	commitActivationRepository(t, f.ctx, f.store, newer, "repo-consumer-target")
	pq := postgres.NewProjectorQueue(postgres.SQLDB{DB: f.db}, "7584-consumer-projector", time.Minute)
	if err := pq.Ack(f.ctx, claimActivationProjectorWork(t, f.ctx, pq, f.scope, newer.GenerationID),
		projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	f.maintenance(t)
	result, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || result.Outcome != activation.OutcomeObsolete {
		t.Fatalf("superseded finalize = %+v err=%v, want obsolete", result, err)
	}
	if state := f.obligationState(t, f.gen); state != "obsolete" {
		t.Fatalf("superseded obligation state=%q", state)
	}
	f.mustEqual(t, "superseded-generation work", wake, row)
	if got := f.obligationRow(t, newer.GenerationID); got == "" {
		t.Fatal("successor obligation absent")
	}
}

// TestActivationObligationWakeIsNotStarvedByOtherClassRowsLive (it
// kills the CTE-only class-filter mutant M12): more than one wake batch of
// retrying deployment_mapping rows of another failure class sort ahead of the
// one not-ready row. The wake must still reach the not-ready row and the
// obligation must complete; a wake whose row selection ignored the class
// would fill its batch with rows its UPDATE then drops, forever.
func TestActivationObligationWakeIsNotStarvedByOtherClassRowsLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_wake_starve", true)
	for i := 0; i <= activation.WakeBatchLimit; i++ {
		other := f.enqueueClaim(t, reducer.DomainDeploymentMapping, f.scope, f.gen, fmt.Sprintf("a-other-%02d", i))
		if err := f.reducerQ.Fail(f.ctx, other, activationRetryable{}); err != nil {
			t.Fatal(err)
		}
	}
	wake := f.notReady(t, f.scope, f.gen, "z-wake")
	var ahead int
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM fact_work_items
WHERE scope_id = $1 AND generation_id = $2 AND domain = 'deployment_mapping' AND status = 'retrying'
  AND failure_class <> 'cross_repo_backward_evidence_not_ready' AND work_item_id < $3`,
		f.scope, f.gen, wake).Scan(&ahead); err != nil || ahead <= activation.WakeBatchLimit {
		t.Fatalf("fixture needs more than %d other-class rows ahead of the wake row: %d err=%v",
			activation.WakeBatchLimit, ahead, err)
	}
	f.maintenance(t)
	obligation := f.claimObligation(t, "starve-owner", time.Minute, f.gen)
	result, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || result.Outcome != activation.OutcomeCompleted || result.Woken != 1 {
		t.Fatalf("finalize = %+v err=%v, want completed with the one not-ready row woken", result, err)
	}
	f.mustClaimable(t, wake)
}
