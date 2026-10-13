// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file is the root side of the #7794 deferred-backfill seam: every
// IngestionStore entry point that reaches the backfill engine goes through
// backfillStore, and the commit-path orchestration the engine was extracted
// from lives here. It holds no engine logic itself.

// backfillStore builds the deferred-backfill engine's Store from this store's
// shared handles, supplying the two seams the engine must not import from the
// parent package: the catalog loader and the evidence-store constructor. Every
// IngestionStore entry point that reaches the engine goes through here, so the
// engine sees the same database, beginner, batch sizing, clock, logger, and
// instruments the root pass always used (issue #7794).
func (s IngestionStore) backfillStore() backfill.Store {
	return backfill.Store{
		DB:          s.database,
		Beginner:    s.beginner,
		BatchSize:   s.maintenanceBatchSize,
		Workers:     s.maintenanceWorkers,
		Now:         s.now,
		Logger:      s.Logger,
		Instruments: s.Instruments,
		LoadCatalog: backfill.LoadRepositoryCatalog,
		NewEvidenceStore: func(tx db.Transaction) backfill.EvidenceStore {
			return NewRelationshipStore(tx)
		},
		NewPhaseStore: func(tx db.Transaction) backfill.PhaseStore {
			return NewGraphProjectionPhaseStateStore(tx)
		},
	}
}

// BackfillAllRelationshipEvidence runs a corpus-wide backward evidence discovery
// pass and publishes readiness for the active repository generations. Evidence
// discovery reads the whole committed fact corpus (cross-repo relationships need
// every repository's facts), but the writes are split into independent,
// per-repository-batch transactions so the pass never holds a fleet-wide lock.
// Each batch transaction acquires only its own repositories' exclusive
// maintenance locks (sorted, deadlock-free), re-reads those repositories' active
// generations under the lock so evidence attaches to the current generation, and
// commits to release the locks before the next batch. A stall on one batch
// therefore blocks only that batch's repositories, never unrelated commits.
func (s IngestionStore) BackfillAllRelationshipEvidence(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	_, err := s.backfillStore().RunAllRelationshipEvidence(ctx, tracer, instruments)
	return err
}

// skipUnchangedGeneration decides, before any write, whether a snapshot
// duplicates the scope's current generation (#7978). It returns true when the
// snapshot was handled without storing a generation: the fact stream is then
// drained and the skip recorded. A default-branch change (#7678) is never a
// duplicate, and neither is a reconciliation whose re-projection request went
// stale (scope.ReconcileReasonRequestStale). A reconciliation snapshot that
// matches the active generation re-opens that generation's projector work, so
// the sweep's drift retraction (#2340) and graph_dirty repair (#7389) still
// happen without a new generation, fact, document, or term row. A match on a
// pending generation (ruling a9-120) stores nothing: that generation's own
// projection is still queued.
//
// The duplicate decision itself (backfill.DecideUnchangedGeneration) is pure
// engine logic; this method is the root commit-path orchestration around it:
// it loads the candidate through the root freshness read, drains the stream,
// executes the re-projection transaction, and records the skip.
func (s IngestionStore) skipUnchangedGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) (bool, error) {
	if !backfill.GenerationNeedsDuplicateCheck(generation) {
		return false, nil
	}
	candidate, found, err := s.unchangedGeneration(ctx, scopeValue.ScopeID)
	if err != nil {
		return true, errors.Join(
			fmt.Errorf("check active generation freshness: %w", err),
			drainFactsAndCheckStream(factStream, factStreamErr),
		)
	}
	action, reason := backfill.DecideUnchangedGeneration(generation, candidate, found)
	if action == backfill.UnchangedActionStore {
		if reason != "" {
			s.logCommitStage(ctx, scopeValue, generation, "store_over_"+reason, time.Now(),
				slog.String("candidate_generation_id", candidate.GenerationID))
		}
		return false, nil
	}
	if err := drainFactsAndCheckStream(factStream, factStreamErr); err != nil {
		return true, fmt.Errorf("read fact stream: %w", err)
	}
	if streamDropped(generation) {
		generation.PreparseIdentity = "" // review F-7: a dropped file stamps no identity
	}
	if action == backfill.UnchangedActionReprojectActive {
		reason, err = s.reprojectActiveGeneration(ctx, mutation, requireClaimFence, scopeValue, generation, candidate)
		if err != nil {
			return true, fmt.Errorf("re-project active generation: %w", err)
		}
	}
	s.backfillStore().RecordSkippedRefresh(ctx, scopeValue, generation, candidate.GenerationID, reason)
	return true, nil
}

// runPostCommitRelationshipBackfill runs the per-commit new-repository
// relationship backfill in its own short transaction, AFTER the caller's main
// ingestion commit has already released the deferred-maintenance shared
// advisory barrier (issue #4451, § T8). Before this split,
// backfill.BackfillRelationshipEvidenceForNewRepositories ran inside the same
// locked transaction as the atomic scope/generation/fact commit, so the
// barrier — and therefore any same-repo deferred-maintenance exclusive-lock
// batch waiting on it — was held for the backfill's corpus-anchor read and
// DiscoverEvidence pass too, not just the atomic write.
//
// This method re-acquires the barrier for only its own bounded backfill write,
// so a same-repo deferred-maintenance batch now waits at most for whichever of
// the two short critical sections (the atomic commit or this backfill) it
// overlaps, never both combined. Lock ordering is unchanged (same
// lockstore.DeferredMaintenanceRepoLockKey), so this introduces no new deadlock class:
// see TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks.
//
// Errors are logged, never returned: the generation this call enriches is
// already durably committed by the time this runs, so surfacing an error here
// would make collector.Service.Run treat an already-successful commit as a
// commit_failure and dead-letter/retry an already-landed generation. The
// periodic corpus-wide BackfillAllRelationshipEvidence pass (cmd/ingester)
// already re-discovers evidence for every active repository on its own
// schedule, so a failure here only delays — never drops — the evidence.
func (s IngestionStore) runPostCommitRelationshipBackfill(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	knownRepoIDs map[string]struct{},
	currentGenerationRepos map[string]relationships.CatalogEntry,
) {
	// Gate: skip the barrier transaction entirely unless a genuinely-new
	// repository was onboarded this generation. currentGenerationRepos
	// routinely includes the already-known repo on every commit, so gating on
	// the raw non-empty set would open the extra transaction on every commit
	// (issue #4451, per PR review). The backfill itself still receives the FULL
	// current set below: it uses the known repositories as evidence targets and
	// does its own new-vs-known filtering, so pre-filtering here would drop
	// those targets and discover zero evidence.
	hasNewRepo := false
	for repoID := range currentGenerationRepos {
		if _, known := knownRepoIDs[repoID]; !known {
			hasNewRepo = true
			break
		}
	}
	if !hasNewRepo {
		return
	}
	if s.beginner == nil {
		return
	}

	currentGenerationRepoIDs := catalogEntryIDSet(currentGenerationRepos)
	start := time.Now()
	if err := s.commitPostCommitRelationshipBackfillTx(ctx, scopeValue, generation, knownRepoIDs, currentGenerationRepoIDs); err != nil {
		if s.Logger != nil {
			s.Logger.ErrorContext(
				ctx, "post-commit relationship backfill failed",
				slog.String("scope_id", scopeValue.ScopeID),
				slog.String("generation_id", generation.GenerationID),
				slog.Float64("duration_seconds", time.Since(start).Seconds()),
				slog.String("error", err.Error()),
			)
		}
		return
	}
	s.logCommitStage(ctx, scopeValue, generation, "post_commit_relationship_backfill", start)
}

// commitPostCommitRelationshipBackfillTx takes the deferred-maintenance shared
// barrier for scopeValue's repository partition and runs the new-repository
// relationship backfill, all inside one short transaction separate from the
// caller's main ingestion commit.
func (s IngestionStore) commitPostCommitRelationshipBackfillTx(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	knownRepoIDs map[string]struct{},
	newRepoIDs map[string]struct{},
) error {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin post-commit relationship backfill transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := lockstore.AcquireDeferredMaintenanceRepoSharedLock(ctx, tx, lockstore.DeferredMaintenanceRepoLockKey(scopeValue)); err != nil {
		return fmt.Errorf("acquire deferred maintenance shared barrier: %w", err)
	}

	catalog, _, err := backfill.LoadRepositoryCatalog(ctx, tx)
	if err != nil {
		return fmt.Errorf("reload repository catalog for relationship backfill: %w", err)
	}
	if err := backfill.BackfillRelationshipEvidenceForNewRepositories(
		ctx, tx, NewRelationshipStore(tx), generation.GenerationID, catalog, knownRepoIDs, newRepoIDs,
	); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit post-commit relationship backfill transaction: %w", err)
	}
	committed = true
	return nil
}

// recordSharedLockHoldDuration reports how long CommitScopeGeneration held the
// deferred-maintenance shared advisory barrier for one atomic commit (issue
// #4451, § T8). It is the operator-visible proof that the lock split shrank
// the hold window to the atomic commit, since the per-commit relationship
// backfill (previously run inside that same window) now runs after release in
// runPostCommitRelationshipBackfill.
func (s IngestionStore) recordSharedLockHoldDuration(ctx context.Context, d time.Duration) {
	if s.Instruments == nil || s.Instruments.IngestionSharedLockHoldDuration == nil {
		return
	}
	s.Instruments.IngestionSharedLockHoldDuration.Record(ctx, d.Seconds())
}
