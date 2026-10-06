// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// This file keeps in the root package the one activation obligation proof
// that asserts the shipped active-repository read (activeRepositoryGenerationsQuery,
// unexported here): a repo_id collision loser is retired inapplicable. The
// other activation live proofs live in the activation package's external
// tests; the fixtures below are copies of theirs.

// TestActivationObligationInapplicableCollisionLoserLive (#7584):
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

// openActivationObligationProofDB opens an isolated schema with the full
// production bootstrap applied. Every activation obligation proof drives real
// queue Claims, so it must never share a schema (#7479).
func openActivationObligationProofDB(t *testing.T, prefix string) (context.Context, *sql.DB) {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	dsn := dsnForDeferredPartitionMemoProof(t)
	database := openIsolatedBootstrapSchema(t, dsn, prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	return ctx, database
}

func activationRepositoryFact(factID, scopeID, generationID, repoID, remote string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, ScopeID: scopeID, GenerationID: generationID,
		FactKind: "repository", StableFactKey: "repository:" + repoID,
		ObservedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		Payload:    map[string]any{"graph_id": repoID, "remote_url": remote},
		SourceRef:  facts.Ref{SourceSystem: "git", FactKey: factID},
	}
}

func commitActivationRepository(t *testing.T, ctx context.Context, store IngestionStore,
	envelope facts.Envelope, repoID string,
) {
	t.Helper()
	now := envelope.ObservedAt.Add(time.Minute)
	if err := store.CommitScopeGeneration(ctx,
		catalogTestScope(envelope.ScopeID, repoID),
		catalogTestGeneration(envelope.ScopeID, envelope.GenerationID, now),
		testFactChannel([]facts.Envelope{envelope})); err != nil {
		t.Fatalf("repository CommitScopeGeneration: %v", err)
	}
}

// claimActivationProjectorWork claims through the same queue (and therefore
// the same lease owner) the caller later Acks with; Ack fences on the owner.
func claimActivationProjectorWork(t *testing.T, ctx context.Context, queue ProjectorQueue,
	wantScope, wantGeneration string,
) projector.ScopeGenerationWork {
	t.Helper()
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if work.Scope.ScopeID != wantScope || work.Generation.GenerationID != wantGeneration {
		t.Fatalf("Claim = %s/%s, want %s/%s", work.Scope.ScopeID,
			work.Generation.GenerationID, wantScope, wantGeneration)
	}
	return work
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
