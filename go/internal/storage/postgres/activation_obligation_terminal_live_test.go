// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationInapplicableWithoutRepositoryFactLive (#7584 ruling
// D3(d)): a cloud scope's accepted Ack owes an obligation, but its generation
// has no repository fact and can never carry a backward-evidence phase. One
// consumer cycle retires it as inapplicable with no maintenance callback; a
// later cycle after the short lease does not reclaim it; Prune never deletes
// it and CatchUp never re-owes it.
func TestActivationObligationInapplicableWithoutRepositoryFactLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_inapplicable")
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	cloud := scope.IngestionScope{
		ScopeID: "gcp:project:demo:relationship:global", SourceSystem: "gcp",
		ScopeKind: scope.KindAccount, CollectorKind: scope.CollectorGCP, PartitionKey: "gcp:project:demo",
	}
	observed := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if err := store.CommitScopeGeneration(ctx, cloud,
		catalogTestGeneration(cloud.ScopeID, "gen-cloud", observed.Add(time.Minute)),
		testFactChannel([]facts.Envelope{{
			FactID: "fact-gcp-edge", ScopeID: cloud.ScopeID, GenerationID: "gen-cloud",
			FactKind: "gcp_cloud_relationship", StableFactKey: "gcp_cloud_relationship:edge",
			ObservedAt: observed,
			Payload:    map[string]any{"source": "projects/demo", "target": "repo-deploy"},
			SourceRef:  facts.Ref{SourceSystem: "gcp", FactKey: "fact-gcp-edge"},
		}})); err != nil {
		t.Fatalf("cloud CommitScopeGeneration: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "7584-cloud-projector", time.Minute)
	work := claimActivationProjectorWork(t, ctx, queue, cloud.ScopeID, "gen-cloud")
	if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	maintainer := &countingActivationMaintainer{}
	runner := newLiveActivationRunner(database, maintainer, 300*time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, cloud.ScopeID, "gen-cloud", "inapplicable", 1)
	if got := maintainer.callsFor("gen-cloud"); got != 0 {
		t.Fatalf("maintenance callbacks for an inapplicable generation = %d, want 0", got)
	}
	time.Sleep(450 * time.Millisecond) // past the 300 ms lease on any clock
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, cloud.ScopeID, "gen-cloud", "inapplicable", 1)
	if got := maintainer.callsFor("gen-cloud"); got != 0 {
		t.Fatalf("maintenance callbacks after the lease = %d, want 0", got)
	}
	obligations := activation.NewStore(SQLDB{DB: database})
	if deleted, err := obligations.Prune(ctx, 0, 100); err != nil || deleted != 0 {
		t.Fatalf("Prune deleted %d err=%v, want an inapplicable row kept", deleted, err)
	}
	if page, err := obligations.CatchUp(ctx, "", 100); err != nil || page.Inserted != 0 {
		t.Fatalf("CatchUp = %+v err=%v, want nothing re-owed", page, err)
	}
	stats, err := obligations.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ByState[activation.StateInapplicable] != 1 {
		t.Fatalf("census = %+v, want one inapplicable row", stats.ByState)
	}
	assertObligationStateToken(t, ctx, database, cloud.ScopeID, "gen-cloud", "inapplicable", 1)
}

// TestActivationObligationInapplicableCollisionLoserLive (#7584 ruling D3(c)):
// two repository scopes share one repo_id; the shipped active-repository read
// (DISTINCT ON repo_id) maps it to the newer scope only. The maintainer
// decides from that read: the loser gets ErrActivationInapplicable and is
// retired after exactly one callback; the winner runs the control-arm pass
// and completes.
func TestActivationObligationInapplicableCollisionLoserLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_collision")
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := NewProjectorQueue(SQLDB{DB: database}, "7584-collision-projector", time.Minute)
	for _, r := range []struct {
		scope, generation string
		later             time.Duration
	}{{"git:collision-loser", "gen-loser", 0}, {"git:collision-winner", "gen-winner", time.Hour}} {
		fact := activationRepositoryFact("fact-"+r.generation, r.scope, r.generation,
			"repo-shared", "https://github.com/acme/shared.git")
		fact.ObservedAt = fact.ObservedAt.Add(r.later)
		commitActivationRepository(t, ctx, store, fact, "repo-shared")
		work := claimActivationProjectorWork(t, ctx, queue, r.scope, r.generation)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	maintainer := &countingActivationMaintainer{decide: func(ctx context.Context, work maintenance.ActivationObligation) error {
		mapped, err := shippedReadMapsGeneration(ctx, database, work.ScopeID, work.GenerationID)
		if err != nil {
			return err
		}
		if !mapped {
			return fmt.Errorf("owed partition %s/%s: %w", work.ScopeID, work.GenerationID, maintenance.ErrActivationInapplicable)
		}
		return NewIngestionStore(SQLDB{DB: database}).RunDeferredRelationshipMaintenance(ctx, nil, nil)
	}}
	runner := newLiveActivationRunner(database, maintainer, 300*time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, "git:collision-loser", "gen-loser", "inapplicable", 1)
	assertObligationStateToken(t, ctx, database, "git:collision-winner", "gen-winner", "completed", 1)
	time.Sleep(450 * time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := maintainer.callsFor("gen-loser"); got != 1 {
		t.Fatalf("maintenance callbacks for the collision loser = %d, want exactly 1", got)
	}
	assertObligationStateToken(t, ctx, database, "git:collision-loser", "gen-loser", "inapplicable", 1)
}

// TestActivationObligationCatalogChangedIsHeldLive (#7584 ruling D2): the
// maintainer refuses with ErrActivationCatalogChanged; the consumer keeps the
// lease, does not retry inside it, and never runs a pass itself. The epoch
// whole pass (run here by the test, as the ingester would after the commit
// that changed the catalog) publishes the phase, and the next cycle after the
// lease completes the obligation without another callback.
func TestActivationObligationCatalogChangedIsHeldLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_catalog")
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	fact := activationRepositoryFact("fact-owed", "git:catalog-owed", "gen-owed", "repo-owed", "https://github.com/acme/owed.git")
	commitActivationRepository(t, ctx, store, fact, "repo-owed")
	queue := NewProjectorQueue(SQLDB{DB: database}, "7584-catalog-projector", time.Minute)
	work := claimActivationProjectorWork(t, ctx, queue, "git:catalog-owed", "gen-owed")
	if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	maintainer := &countingActivationMaintainer{decide: func(context.Context, maintenance.ActivationObligation) error {
		return fmt.Errorf("targeted pass refused: %w", maintenance.ErrActivationCatalogChanged)
	}}
	runner := newLiveActivationRunner(database, maintainer, 400*time.Millisecond)
	for i := 0; i < 2; i++ { // the second cycle is inside the lease
		if _, err := runner.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertObligationStateToken(t, ctx, database, "git:catalog-owed", "gen-owed", "leased", 1)
	if got := maintainer.callsFor("gen-owed"); got != 1 {
		t.Fatalf("callbacks inside one lease = %d, want 1", got)
	}
	wholePasses := 0
	if err := NewIngestionStore(SQLDB{DB: database}).RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	wholePasses++
	time.Sleep(550 * time.Millisecond)
	if _, err := runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertObligationStateToken(t, ctx, database, "git:catalog-owed", "gen-owed", "completed", 2)
	if got := maintainer.callsFor("gen-owed"); got != 1 || wholePasses != 1 {
		t.Fatalf("callbacks=%d wholePasses=%d, want 1 and 1 (the consumer ran no pass)", got, wholePasses)
	}
}

// TestActivationObligationNullActivePointerIsObsoleteLive (#7584 ruling D4):
// the active generation G fails through the real projector Fail path after a
// refinalize re-drive, which nulls the scope's pointer. G already has its
// phase and a waiting row. One consumer cycle retires G's obligation as
// obsolete, wakes nothing and completes nothing.
func TestActivationObligationNullActivePointerIsObsoleteLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_nullptr", true)
	if _, err := NewRecoveryStore(SQLDB{DB: f.db}).RefinalizeScopeProjections(f.ctx,
		recovery.RefinalizeFilter{ScopeIDs: []string{f.scope}}, time.Now().UTC()); err != nil {
		t.Fatalf("refinalize: %v", err)
	}
	pq := NewProjectorQueue(SQLDB{DB: f.db}, "7584-nullptr-projector", time.Minute)
	redrive := claimActivationProjectorWork(t, f.ctx, pq, f.scope, f.gen)
	wake := f.notReady(t, f.scope, f.gen, "wake")
	f.maintenance(t)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)
	if err := pq.Fail(f.ctx, redrive, errors.New("permanent projection failure")); err != nil {
		t.Fatalf("projector Fail: %v", err)
	}
	assertActivationActivePointer(t, f.ctx, f.db, f.scope, "")
	before := f.row(t, wake, false)
	maintainer := &countingActivationMaintainer{}
	runner := newLiveActivationRunner(f.db, maintainer, time.Minute)
	if _, err := runner.RunOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	if state := f.obligationState(t, f.gen); state != "obsolete" {
		t.Fatalf("obligation for the failed generation = %q, want obsolete", state)
	}
	f.mustEqual(t, "waiting work of the failed generation", wake, before)
	if got := maintainer.callsFor(f.gen); got != 0 {
		t.Fatalf("maintenance callbacks for the failed generation = %d, want 0", got)
	}
}

type countingActivationMaintainer struct {
	decide func(context.Context, maintenance.ActivationObligation) error
	calls  map[string]int
}

func (m *countingActivationMaintainer) MaintainActivation(ctx context.Context, work maintenance.ActivationObligation) error {
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	m.calls[work.GenerationID]++
	if m.decide == nil {
		return nil
	}
	return m.decide(ctx, work)
}

func (m *countingActivationMaintainer) callsFor(generationID string) int {
	return m.calls[generationID]
}

func newLiveActivationRunner(database *sql.DB, maintainer maintenance.ActivationMaintainer, lease time.Duration) *maintenance.ActivationObligationRunner {
	return &maintenance.ActivationObligationRunner{
		Store:      activation.RunnerStore{Store: activation.NewStore(SQLDB{DB: database})},
		Maintainer: maintainer,
		Config:     maintenance.ActivationObligationRunnerConfig{Owner: "7584-terminal-consumer", Lease: lease},
	}
}

// shippedReadMapsGeneration reports whether the shipped active-repository read
// maps any repository to the exact scope generation.
func shippedReadMapsGeneration(ctx context.Context, database *sql.DB, scopeID, generationID string) (bool, error) {
	rows, err := database.QueryContext(ctx, activeRepositoryGenerationsQuery)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var repoID, s, g string
		if err := rows.Scan(&repoID, &s, &g); err != nil {
			return false, err
		}
		if s == scopeID && g == generationID {
			return true, nil
		}
	}
	return false, rows.Err()
}

func assertObligationStateToken(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID, wantState string, wantToken int64,
) {
	t.Helper()
	var state string
	var token int64
	var finished bool
	if err := database.QueryRowContext(ctx, `SELECT state, claim_token, finished_at IS NOT NULL
FROM activation_obligations WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(&state, &token, &finished); err != nil {
		t.Fatalf("read obligation %s/%s: %v", scopeID, generationID, err)
	}
	terminal := wantState == "completed" || wantState == "obsolete" || wantState == "inapplicable"
	if state != wantState || token != wantToken || finished != terminal {
		t.Fatalf("obligation %s/%s = state %q token %d finished %t, want %q token %d finished %t",
			scopeID, generationID, state, token, finished, wantState, wantToken, terminal)
	}
}

// TestActivationObligationRetireInapplicableIsFencedLive: the maintainer-side
// retire takes the same lease fence as Finalize (a stale token writes
// nothing) and still retires as obsolete when the scope moved on.
func TestActivationObligationRetireInapplicableIsFencedLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_retire_fence", true)
	stale := f.claimObligation(t, "retire-owner", 300*time.Millisecond, f.gen)
	f.awaitLeaseExpiry(t, f.gen)
	fresh := f.claimObligation(t, "retire-owner", time.Minute, f.gen)
	state := f.digest(t)
	if result, err := f.oblig.RetireInapplicable(f.ctx, *stale); err != nil || result.Outcome != activation.OutcomeNotOwner {
		t.Fatalf("stale-token retire = %+v err=%v, want not_owner", result, err)
	}
	if f.digest(t) != state {
		t.Fatal("stale-token retire changed durable state")
	}
	newer := activationRepositoryFact("fact-retire-newer", f.scope, "gen-retire-newer",
		"repo-consumer-target", "https://github.com/acme/payments-deploy.git")
	newer.ObservedAt = newer.ObservedAt.Add(3 * time.Hour)
	commitActivationRepository(t, f.ctx, f.store, newer, "repo-consumer-target")
	pq := NewProjectorQueue(SQLDB{DB: f.db}, "7584-consumer-projector", time.Minute)
	if err := pq.Ack(f.ctx, claimActivationProjectorWork(t, f.ctx, pq, f.scope, newer.GenerationID),
		projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.oblig.RetireInapplicable(f.ctx, *fresh)
	if err != nil || result.Outcome != activation.OutcomeObsolete {
		t.Fatalf("retire after the pointer moved = %+v err=%v, want obsolete", result, err)
	}
	if got := f.obligationState(t, f.gen); got != "obsolete" {
		t.Fatalf("obligation state = %q, want obsolete", got)
	}
}
