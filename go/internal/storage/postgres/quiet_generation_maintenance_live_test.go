// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestQuietGenerationActivatesAfterMaintenanceSnapshotLive drives the collector
// loop across one update commit, its normal maintenance callback, a later real
// projector Ack, and quiet polls. The update facts exist before maintenance
// reads the old active generation. No ingestion commit occurs after Ack. The
// activation obligation consumer runs with the production partition-scoped
// maintainer (#7584).
func TestQuietGenerationActivatesAfterMaintenanceSnapshotLive(t *testing.T) {
	runQuietGenerationActivation(t, false)
}

// TestQuietGenerationActivatesWithControlArmMaintenanceLive is the LABELLED
// CONTROL ARM: the same flow with the whole native deferred maintenance as
// the consumer's callback. It exists only as a comparison; ruling D2 forbids
// it as the shipped callback.
func TestQuietGenerationActivatesWithControlArmMaintenanceLive(t *testing.T) {
	runQuietGenerationActivation(t, true)
}

func runQuietGenerationActivation(t *testing.T, controlArm bool) {
	t.Helper()
	if os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE") != "1" {
		t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 for disposable PostgreSQL proof")
	}
	dsn := dsnForDeferredPartitionMemoProof(t)
	database := openDeferredPartitionMemoProofDB(t, dsn)
	provisionReopenPartitionMemoSchema(t, database)
	provisionQuietGenerationAckColumns(t, database)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	base := time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second)
	const (
		sourceScope = "git:quiet-source"
		targetScope = "git:quiet-target"
		oldID       = "quiet-source-old"
		newID       = "quiet-source-new"
		targetID    = "quiet-target-current"
	)
	seedMemoProofScopesAndFacts(t, ctx, database, []memoProofFixture{
		{scopeID: sourceScope, genID: oldID, repoID: "repo-source", repoName: "source-service"},
		{scopeID: targetScope, genID: targetID, repoID: "repo-target", repoName: "target-service"},
	}, map[string]string{"repo-source": "target-service"}, base)
	for _, row := range []struct{ scopeID, generationID string }{
		{sourceScope, oldID}, {targetScope, targetID},
	} {
		if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status = 'active', activated_at = $3
WHERE scope_id = $1 AND generation_id = $2`, row.scopeID, row.generationID, base); err != nil {
			t.Fatalf("activate initial generation %q: %v", row.generationID, err)
		}
		if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
			row.scopeID, row.generationID); err != nil {
			t.Fatalf("point scope %q at initial generation: %v", row.scopeID, err)
		}
	}

	// The source yields one update and then remains quiet. Its fixture
	// committer persists the pending generation, two facts, and projector work
	// in one transaction before Service.Run invokes the maintenance hook.
	update := collector.FactsFromSlice(
		scope.IngestionScope{ScopeID: sourceScope, SourceSystem: "git"},
		scope.ScopeGeneration{GenerationID: newID},
		[]facts.Envelope{
			{
				FactID: "quiet-new-repository", ScopeID: sourceScope,
				GenerationID: newID, FactKind: "repository",
				Payload: map[string]any{"repo_id": "repo-source", "name": "source-service"},
			},
			{
				FactID: "quiet-new-content", ScopeID: sourceScope,
				GenerationID: newID, FactKind: "content",
				Payload: map[string]any{
					"repo_id": "repo-source", "artifact_type": "terraform",
					"relative_path": "main.tf", "content": "app_repo = \"target-service\"",
				},
			},
		},
	)
	source := &quietGenerationSource{
		update: update, afterAck: make(chan struct{}),
		maintenanceReturned: make(chan struct{}), idleObserved: make(chan struct{}),
	}
	committer := &quietGenerationFixtureCommitter{
		database: database, scopeID: sourceScope, generationID: newID,
		ingestedAt: base.Add(time.Minute),
	}

	store := NewIngestionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return base.Add(2 * time.Minute) }
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
	assertQuietBackwardPhase(t, ctx, phaseStore, sourceScope, oldID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, targetScope, targetID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, sourceScope, newID, false)
	// A deployment_mapping handler for the new generation already found the
	// phase missing and is waiting on the not-ready retry schedule.
	seedQuietWaitingDeploymentMapping(t, ctx, database, sourceScope, newID, base.Add(2*time.Minute))

	queue := NewProjectorQueue(SQLDB{DB: database}, "quiet-projector", time.Minute)
	queue.Now = func() time.Time { return base.Add(3 * time.Minute) }
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: sourceScope},
		Generation:   scope.ScopeGeneration{GenerationID: newID},
		AttemptCount: 1,
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("ProjectorQueue.Ack(update): %v", err)
	}
	var activeID, generationStatus, projectorStatus string
	if err := database.QueryRowContext(ctx, `
SELECT ingestion_scopes.active_generation_id, scope_generations.status, fact_work_items.status
FROM ingestion_scopes
JOIN scope_generations ON scope_generations.scope_id = ingestion_scopes.scope_id
JOIN fact_work_items ON fact_work_items.generation_id = scope_generations.generation_id
WHERE ingestion_scopes.scope_id = $1 AND scope_generations.generation_id = $2
  AND fact_work_items.work_item_id = 'quiet-new-projector'`, sourceScope, newID,
	).Scan(&activeID, &generationStatus, &projectorStatus); err != nil {
		t.Fatalf("read activation state: %v", err)
	}
	if activeID != newID || generationStatus != "active" || projectorStatus != "succeeded" {
		t.Fatalf("Ack state = (%q, %q, %q), want (%q, active, succeeded)",
			activeID, generationStatus, projectorStatus, newID)
	}
	assertQuietBackwardPhase(t, ctx, phaseStore, sourceScope, newID, false)
	// #7584: the Ack wrote the exact-generation obligation; the resolution
	// engine's consumer settles it. The callback here is the labelled
	// whole-maintenance control arm, never the shipped callback.
	consumer := startQuietActivationConsumer(t, ctx, database, store, controlArm)

	// The same collector process now polls empty. It has committed once, so
	// the existing Service.Run contract sends no further drain callback.
	// A correct runtime must nevertheless complete the activation obligation.
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
	phaseDeadline := time.NewTimer(2 * time.Second)
	defer phaseDeadline.Stop()
	phaseTicker := time.NewTicker(10 * time.Millisecond)
	defer phaseTicker.Stop()
	for {
		ready, found, err := phaseStore.Lookup(ctx, quietBackwardPhaseKey(sourceScope, newID),
			reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
		if err != nil {
			t.Fatalf("lookup update-generation backward phase: %v", err)
		}
		if ready && found {
			break
		}
		select {
		case <-phaseTicker.C:
		case <-phaseDeadline.C:
			t.Fatalf("new active generation %q has no same-generation backward phase after "+
				"three quiet collector polls; maintenance callbacks=%d, commits=%d",
				newID, drainCalls.Load(), committer.calls.Load())
		case <-serviceDone:
			t.Fatalf("collector exited while waiting for exact backward phase: %v", serviceErr)
		case <-ctx.Done():
			t.Fatalf("waiting for exact backward phase: %v", ctx.Err())
		}
	}

	assertQuietBackwardPhase(t, ctx, phaseStore, sourceScope, oldID, true)
	assertQuietBackwardPhase(t, ctx, phaseStore, targetScope, targetID, true)

	var newEvidence int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM relationship_evidence_facts
WHERE generation_id = $1 AND source_repo_id = 'repo-source'
  AND target_repo_id = 'repo-target'`, newID).Scan(&newEvidence); err != nil {
		t.Fatalf("count update-generation relationship evidence: %v", err)
	}
	if newEvidence == 0 {
		t.Fatal("quiet update published a phase without its same-generation relationship evidence")
	}
	awaitQuietObligationCompleted(t, ctx, database, sourceScope, newID)
	consumer.assertSettledQuietGeneration(t, ctx, database, sourceScope, newID)
	if got := drainCalls.Load(); got != 1 {
		t.Fatalf("ingester maintenance callbacks after consumer = %d, want 1", got)
	}
}

func provisionQuietGenerationAckColumns(t *testing.T, database *sql.DB) {
	t.Helper()
	// Extend the existing backfill/reopen fixture with only the production
	// columns ProjectorQueue.Ack reads or updates. The queue and phase methods
	// themselves execute their production SQL against this isolated schema.
	if _, err := database.ExecContext(t.Context(), `
ALTER TABLE ingestion_scopes
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN ingested_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE scope_generations
    ADD COLUMN activated_at TIMESTAMPTZ NULL,
    ADD COLUMN superseded_at TIMESTAMPTZ NULL,
    ADD COLUMN is_delta BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN delta_baseline_commit_sha TEXT NULL,
    ADD COLUMN source_commit_sha TEXT NULL;
`); err != nil {
		t.Fatalf("extend deferred maintenance proof schema for real Ack: %v", err)
	}
	// ProjectorQueue.Ack writes its activation obligation (#7584) in the Ack
	// transaction, so the fixture carries the production table, applied from
	// the shipped migration rather than a copy.
	if _, err := database.ExecContext(t.Context(), MigrationSQL("activation_obligations")); err != nil {
		t.Fatalf("apply activation obligation migration to proof schema: %v", err)
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

// quietGenerationFixtureCommitter limits the test's stand-in to the collector
// commit boundary: it writes the supplied fact stream and pending projector
// work atomically to the isolated schema. Maintenance, Ack, and phase lookup
// all execute the production store methods.
type quietGenerationFixtureCommitter struct {
	database     *sql.DB
	scopeID      string
	generationID string
	ingestedAt   time.Time
	calls        atomic.Int32
}

func (c *quietGenerationFixtureCommitter) CommitScopeGeneration(
	ctx context.Context,
	collectedScope scope.IngestionScope,
	generation scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	if collectedScope.ScopeID != c.scopeID || generation.GenerationID != c.generationID {
		return fmt.Errorf("unexpected update partition (%q, %q)", collectedScope.ScopeID, generation.GenerationID)
	}
	transaction, err := c.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update fixture commit: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at, status) VALUES ($1, $2, $3, 'pending')",
		c.generationID, c.scopeID, c.ingestedAt,
	); err != nil {
		return fmt.Errorf("insert pending update generation: %w", err)
	}
	factCount := 0
	for envelope := range stream {
		if envelope.ScopeID != c.scopeID || envelope.GenerationID != c.generationID {
			return fmt.Errorf("fact %q has wrong partition (%q, %q)",
				envelope.FactID, envelope.ScopeID, envelope.GenerationID)
		}
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			return fmt.Errorf("marshal update fact %q: %w", envelope.FactID, err)
		}
		if _, err := transaction.ExecContext(ctx,
			"INSERT INTO fact_records "+
				"(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, "+
				"source_system, source_fact_key, observed_at, ingested_at, payload) "+
				"VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)",
			envelope.FactID, c.scopeID, c.generationID, envelope.FactKind,
			c.ingestedAt, payload,
		); err != nil {
			return fmt.Errorf("insert update fact %q: %w", envelope.FactID, err)
		}
		factCount++
	}
	if factCount != 2 {
		return fmt.Errorf("update fact count = %d, want 2", factCount)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO fact_work_items "+
			"(work_item_id, scope_id, generation_id, stage, domain, status, "+
			"attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at) "+
			"VALUES ('quiet-new-projector', $1, $2, 'projector', 'source_local', 'running', "+
			"1, 'quiet-projector', $3, $4, '{}'::jsonb, $4, $4)",
		c.scopeID, c.generationID, c.ingestedAt.Add(10*time.Minute), c.ingestedAt,
	); err != nil {
		return fmt.Errorf("insert claimed update projector work: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit update generation fixture: %w", err)
	}
	c.calls.Add(1)
	return nil
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
