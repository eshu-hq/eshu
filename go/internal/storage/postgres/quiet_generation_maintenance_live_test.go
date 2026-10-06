// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// This file uses only symbols that exist on origin/main, so the same flow
// re-runs RED there with a stub for the consumer hooks it calls
// (startQuietActivationConsumer, assertSettledQuietGeneration,
// awaitQuietObligationCompleted); on this branch those hooks live in
// quiet_generation_consumer_helpers_test.go.

// TestQuietGenerationActivatesAfterMaintenanceSnapshotLive drives the collector
// loop across one update commit, its normal maintenance callback, a later real
// projector Ack, and quiet polls. The update facts exist before maintenance
// reads the old active generation. No ingestion commit occurs after Ack. The
// activation obligation consumer runs with the production partition-scoped
// maintainer (#7584). Every commit, Claim and Ack is the production store
// path on the full bootstrap schema.
func TestQuietGenerationActivatesAfterMaintenanceSnapshotLive(t *testing.T) {
	runQuietGenerationActivation(t, false)
}

// TestQuietGenerationActivatesWithControlArmMaintenanceLive is the LABELLED
// CONTROL ARM: the same flow with the whole native deferred maintenance as
// the consumer's callback. It exists only as a comparison; it is never the
// shipped callback.
func TestQuietGenerationActivatesWithControlArmMaintenanceLive(t *testing.T) {
	runQuietGenerationActivation(t, true)
}

const (
	quietSourceScope = "git:quiet-source"
	quietTargetScope = "git:quiet-target"
	quietOldID       = "quiet-source-old"
	quietNewID       = "quiet-source-new"
	quietTargetID    = "quiet-target-current"
)

// openQuietGenerationProofDB opens an isolated schema with the full
// production bootstrap applied (ApplyBootstrap), on the deferred-partition
// proof DSN, never a hand-extended partial schema.
func openQuietGenerationProofDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	return openIsolatedBootstrapSchema(t, dsnForDeferredPartitionMemoProof(t), "quiet_generation")
}

// quietRepositoryFacts is one generation's repository fact plus, when alias
// is set, one Terraform content fact referencing alias.
func quietRepositoryFacts(scopeID, generationID, repoID, name, alias string, at time.Time) []facts.Envelope {
	envelope := func(id, kind string, payload map[string]any) facts.Envelope {
		return facts.Envelope{
			FactID: id, ScopeID: scopeID, GenerationID: generationID, FactKind: kind,
			StableFactKey: kind + ":" + id, ObservedAt: at, Payload: payload,
			SourceRef: facts.Ref{SourceSystem: "git", FactKey: id},
		}
	}
	out := []facts.Envelope{envelope(generationID+"-repository", "repository",
		map[string]any{"repo_id": repoID, "name": name})}
	if alias != "" {
		out = append(out, envelope(generationID+"-content", "content", map[string]any{
			"repo_id": repoID, "artifact_type": "terraform",
			"relative_path": "main.tf", "content": fmt.Sprintf("app_repo = %q", alias),
		}))
	}
	return out
}

// claimAckQuiet claims the next projector work through queue, requires it to
// be the wanted generation, and acknowledges it (the real projector Ack).
func claimAckQuiet(t *testing.T, ctx context.Context, queue ProjectorQueue, scopeID, generationID string) {
	t.Helper()
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("projector Claim for %s: ok=%v err=%v", generationID, ok, err)
	}
	if work.Scope.ScopeID != scopeID || work.Generation.GenerationID != generationID {
		t.Fatalf("projector Claim = %s/%s, want %s/%s", work.Scope.ScopeID,
			work.Generation.GenerationID, scopeID, generationID)
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("projector Ack %s: %v", generationID, err)
	}
}

// countingQuietCommitter is the production ingestion commit with a counter.
type countingQuietCommitter struct {
	store IngestionStore
	calls atomic.Int32
}

func (c *countingQuietCommitter) CommitScopeGeneration(ctx context.Context, scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration, stream <-chan facts.Envelope,
) error {
	if err := c.store.CommitScopeGeneration(ctx, scopeValue, generation, stream); err != nil {
		return err
	}
	c.calls.Add(1)
	return nil
}

func runQuietGenerationActivation(t *testing.T, controlArm bool) {
	t.Helper()
	database := openQuietGenerationProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	store := NewIngestionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return base.Add(2 * time.Minute) }
	queue := NewProjectorQueue(SQLDB{DB: database}, "quiet-projector", time.Minute)
	queue.Now = func() time.Time { return base.Add(3 * time.Minute) }

	// The initial generations of both repositories are committed and
	// activated through the production commit, Claim and Ack.
	for _, initial := range []struct {
		scopeID, generationID, repoID, name, alias string
	}{
		{quietSourceScope, quietOldID, "repo-source", "source-service", "target-service"},
		{quietTargetScope, quietTargetID, "repo-target", "target-service", ""},
	} {
		if err := store.CommitScopeGeneration(ctx, catalogTestScope(initial.scopeID, initial.repoID),
			catalogTestGeneration(initial.scopeID, initial.generationID, base),
			testFactChannel(quietRepositoryFacts(initial.scopeID, initial.generationID,
				initial.repoID, initial.name, initial.alias, base))); err != nil {
			t.Fatalf("commit initial generation %q: %v", initial.generationID, err)
		}
		claimAckQuiet(t, ctx, queue, initial.scopeID, initial.generationID)
	}

	// The source yields one update and then remains quiet; the production
	// commit persists the pending generation, its facts and its projector
	// work in one transaction before Service.Run invokes the maintenance hook.
	update := collector.FactsFromSlice(
		catalogTestScope(quietSourceScope, "repo-source"),
		catalogTestGeneration(quietSourceScope, quietNewID, base.Add(time.Minute)),
		quietRepositoryFacts(quietSourceScope, quietNewID, "repo-source", "source-service", "target-service", base.Add(time.Minute)),
	)
	source := &quietGenerationSource{
		update: update, afterAck: make(chan struct{}),
		maintenanceReturned: make(chan struct{}), idleObserved: make(chan struct{}),
	}
	committer := &countingQuietCommitter{store: store}
	var drainCalls atomic.Int32
	serviceCtx, stopService := context.WithCancel(ctx)
	serviceDone := make(chan struct{})
	var serviceErr error
	service := collector.Service{
		Source: source, Committer: committer, PollInterval: time.Millisecond,
		AfterEmptyBatchDrained: false, // the real single-shard ingester setting
		AfterBatchDrained: func(ctx context.Context, hasCommitted bool) error {
			err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
				ctx,
				DeferredMaintenanceBarrierConfig{
					ShardCount: 1, ShardIndex: 0, HasCommitted: hasCommitted,
				},
				nil, nil,
			)
			if err == nil {
				drainCalls.Add(1)
			}
			return err
		},
	}
	go func() {
		serviceErr = service.Run(serviceCtx)
		close(serviceDone)
	}()
	t.Cleanup(func() {
		stopService()
		select {
		case <-serviceDone:
			if serviceErr != nil {
				t.Errorf("collector.Service.Run() error = %v", serviceErr)
			}
		case <-time.After(2 * time.Second):
			t.Error("collector.Service.Run did not stop after cancellation")
		}
	})
	select {
	case <-source.maintenanceReturned:
	case <-serviceDone:
		t.Fatalf("collector exited before first maintenance: %v", serviceErr)
	case <-ctx.Done():
		t.Fatalf("first maintenance did not finish: %v", ctx.Err())
	}
	if got := committer.calls.Load(); got != 1 {
		t.Fatalf("collector committed %d generations before Ack, want 1", got)
	}
	if got := drainCalls.Load(); got != 1 {
		t.Fatalf("maintenance callbacks before Ack = %d, want 1", got)
	}

	phaseStore := NewGraphProjectionPhaseStateStore(SQLDB{DB: database})
	assertQuietBackwardPhase(t, ctx, phaseStore, quietSourceScope, quietOldID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, quietTargetScope, quietTargetID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, quietSourceScope, quietNewID, false)
	// A deployment_mapping handler for the new generation already found the
	// phase missing and is waiting on the not-ready retry schedule.
	seedQuietWaitingDeploymentMapping(t, ctx, database, quietSourceScope, quietNewID, base.Add(2*time.Minute))

	claimAckQuiet(t, ctx, queue, quietSourceScope, quietNewID)
	var activeID, generationStatus, projectorStatus string
	if err := database.QueryRowContext(ctx, `
SELECT ingestion_scopes.active_generation_id, scope_generations.status, fact_work_items.status
FROM ingestion_scopes
JOIN scope_generations ON scope_generations.scope_id = ingestion_scopes.scope_id
JOIN fact_work_items ON fact_work_items.generation_id = scope_generations.generation_id
WHERE ingestion_scopes.scope_id = $1 AND scope_generations.generation_id = $2
  AND fact_work_items.work_item_id = $3`, quietSourceScope, quietNewID,
		projectorWorkItemID(quietSourceScope, quietNewID),
	).Scan(&activeID, &generationStatus, &projectorStatus); err != nil {
		t.Fatalf("read activation state: %v", err)
	}
	if activeID != quietNewID || generationStatus != "active" || projectorStatus != "succeeded" {
		t.Fatalf("Ack state = (%q, %q, %q), want (%q, active, succeeded)",
			activeID, generationStatus, projectorStatus, quietNewID)
	}
	assertQuietBackwardPhase(t, ctx, phaseStore, quietSourceScope, quietNewID, false)
	// #7584: the Ack wrote the exact-generation obligation; the resolution
	// engine's consumer settles it.
	consumer := startQuietActivationConsumer(t, ctx, database, store, controlArm)

	// The same collector process now polls empty. It has committed once, so
	// the existing Service.Run contract sends no further drain callback.
	// A correct runtime must nevertheless publish the exact phase.
	close(source.afterAck)
	select {
	case <-source.idleObserved:
	case <-serviceDone:
		t.Fatalf("collector exited before three quiet polls: %v", serviceErr)
	case <-ctx.Done():
		t.Fatalf("collector did not make three quiet polls: %v", ctx.Err())
	}
	if got := committer.calls.Load(); got != 1 {
		t.Fatalf("collector committed %d generations after quiet polls, want 1", got)
	}
	if got := drainCalls.Load(); got != 1 {
		t.Fatalf("maintenance callbacks after quiet polls = %d, want 1", got)
	}
	awaitQuietExactPhase(t, ctx, phaseStore, serviceDone, func() error { return serviceErr }, func() string {
		return fmt.Sprintf("maintenance callbacks=%d, commits=%d", drainCalls.Load(), committer.calls.Load())
	})

	assertQuietBackwardPhase(t, ctx, phaseStore, quietSourceScope, quietOldID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, quietTargetScope, quietTargetID, true)
	var newEvidence int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM relationship_evidence_facts
WHERE generation_id = $1 AND source_repo_id = 'repo-source'
  AND target_repo_id = 'repo-target'`, quietNewID).Scan(&newEvidence); err != nil {
		t.Fatalf("count update-generation relationship evidence: %v", err)
	}
	if newEvidence == 0 {
		t.Fatal("quiet update published a phase without its same-generation relationship evidence")
	}
	awaitQuietObligationCompleted(t, ctx, database, quietSourceScope, quietNewID)
	consumer.assertSettledQuietGeneration(t, ctx, database, quietSourceScope, quietNewID)
	if got := drainCalls.Load(); got != 1 {
		t.Fatalf("ingester maintenance callbacks after consumer = %d, want 1", got)
	}
}

// awaitQuietExactPhase waits up to 2 s for the update generation's
// same-generation backward phase; without a consumer it never appears.
func awaitQuietExactPhase(t *testing.T, ctx context.Context, phaseStore *GraphProjectionPhaseStateStore,
	serviceDone <-chan struct{}, serviceErr func() error, counts func() string,
) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, found, err := phaseStore.Lookup(ctx, quietBackwardPhaseKey(quietSourceScope, quietNewID),
			reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
		if err != nil {
			t.Fatalf("lookup update-generation backward phase: %v", err)
		}
		if ready && found {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("new active generation %q has no same-generation backward phase after "+
				"three quiet collector polls; %s", quietNewID, counts())
		case <-serviceDone:
			t.Fatalf("collector exited while waiting for exact backward phase: %v", serviceErr())
		case <-ctx.Done():
			t.Fatalf("waiting for exact backward phase: %v", ctx.Err())
		}
	}
}

// seedQuietWaitingDeploymentMapping inserts the row shape the reducer's
// real Fail leaves for a deployment_mapping handler that found the
// backward-evidence phase missing (proven on the full schema by
// TestActivationObligationConsumerOrderingLive): retrying, the not-ready
// class, no lease, visible an hour from now.
func seedQuietWaitingDeploymentMapping(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID string, now time.Time,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items
    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
     visible_at, next_attempt_at, failure_class, failure_message, payload, created_at, updated_at)
VALUES ('quiet-new-deployment-mapping', $1, $2, 'reducer', 'deployment_mapping', 'retrying', 1,
     clock_timestamp() + interval '1 hour', clock_timestamp() + interval '1 hour',
     'cross_repo_backward_evidence_not_ready', 'backward evidence not ready', '{}'::jsonb, $3, $3)`,
		scopeID, generationID, now); err != nil {
		t.Fatalf("seed waiting deployment_mapping row: %v", err)
	}
}

func assertQuietBackwardPhase(
	t *testing.T,
	ctx context.Context,
	store *GraphProjectionPhaseStateStore,
	scopeID, generationID string,
	want bool,
) {
	t.Helper()
	ready, found, err := store.Lookup(ctx, quietBackwardPhaseKey(scopeID, generationID),
		reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
	if err != nil {
		t.Fatalf("lookup backward evidence phase for (%q, %q): %v", scopeID, generationID, err)
	}
	if ready != want || found != want {
		t.Fatalf("backward evidence phase for (%q, %q) = ready:%t found:%t, want %t",
			scopeID, generationID, ready, found, want)
	}
}

// quietGenerationSource yields one update, then one empty batch that triggers
// the ingester's maintenance callback. Later idle polls wait until the test
// acknowledges the update, so Ack cannot race ahead of that callback.
type quietGenerationSource struct {
	update              collector.CollectedGeneration
	afterAck            chan struct{}
	maintenanceReturned chan struct{}
	idleObserved        chan struct{}
	stage               int
	quietPolls          int
}

func (s *quietGenerationSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	switch s.stage {
	case 0:
		s.stage = 1
		return s.update, true, nil
	case 1:
		s.stage = 2
		return collector.CollectedGeneration{}, false, nil
	default:
		if s.quietPolls == 0 {
			close(s.maintenanceReturned)
		}
		select {
		case <-s.afterAck:
		case <-ctx.Done():
			return collector.CollectedGeneration{}, false, ctx.Err()
		}
		s.quietPolls++
		if s.quietPolls == 3 {
			close(s.idleObserved)
		}
		return collector.CollectedGeneration{}, false, nil
	}
}

func quietBackwardPhaseKey(scopeID, generationID string) reducer.GraphProjectionPhaseKey {
	return reducer.GraphProjectionPhaseKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: scopeID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
	}
}
