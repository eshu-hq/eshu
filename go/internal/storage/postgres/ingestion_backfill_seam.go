// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/obligation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill/targeted"
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

// successorPrior carries the scope's current generation past the duplicate
// check into the commit path, so the post-stream destructive-successor guard
// can compare without re-reading: the candidate the duplicate check loaded,
// whether one exists, and its committed fact count.
type successorPrior struct {
	candidate backfill.UnchangedGenerationCandidate
	found     bool
	factCount int
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
// executes the re-projection transaction, and records the skip. On the store
// path it also loads the candidate's committed fact count for the
// destructive-successor guard, which the commit path evaluates after the
// fact stream lands.
func (s IngestionStore) skipUnchangedGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) (bool, successorPrior, error) {
	if !backfill.GenerationNeedsDuplicateCheck(generation) {
		return false, successorPrior{}, nil
	}
	candidate, found, err := s.unchangedGeneration(ctx, scopeValue.ScopeID)
	if err != nil {
		return true, successorPrior{}, errors.Join(
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
		prior, err := s.successorPriorForGuard(ctx, scopeValue.ScopeID, candidate, found, generation)
		if err != nil {
			return true, successorPrior{}, errors.Join(
				err,
				drainFactsAndCheckStream(factStream, factStreamErr),
			)
		}
		return false, prior, nil
	}
	if err := drainFactsAndCheckStream(factStream, factStreamErr); err != nil {
		return true, successorPrior{}, fmt.Errorf("read fact stream: %w", err)
	}
	if streamDropped(generation) {
		generation.PreparseIdentity = "" // review F-7: a dropped file stamps no identity
	}
	if action == backfill.UnchangedActionReprojectActive {
		reason, err = s.reprojectActiveGeneration(ctx, mutation, requireClaimFence, scopeValue, generation, candidate)
		if err != nil {
			return true, successorPrior{}, fmt.Errorf("re-project active generation: %w", err)
		}
	}
	s.backfillStore().RecordSkippedRefresh(ctx, scopeValue, generation, candidate.GenerationID, reason)
	return true, successorPrior{}, nil
}

// successorPriorForGuard reads the candidate generation's committed fact
// count for the destructive-successor guard. It runs only when the guard
// can refuse — a found candidate without a trusted-deletion signal — so
// trusted targets and first writes pay no extra read. The read is a single
// indexed COUNT(*) over one generation's fact_records rows
// (fact_records_scope_generation_idx), reusing FactStore.CountFacts, issued
// on the base connection before the commit transaction opens so it never
// holds the commit's shared maintenance barrier. A failure fails closed:
// the commit cannot prove the successor is safe, so the snapshot is dropped
// with an error and retried rather than stored.
func (s IngestionStore) successorPriorForGuard(
	ctx context.Context,
	scopeID string,
	candidate backfill.UnchangedGenerationCandidate,
	found bool,
	generation scope.ScopeGeneration,
) (successorPrior, error) {
	prior := successorPrior{candidate: candidate, found: found}
	if !found || generation.TrustedDeletion {
		return prior, nil
	}
	count, err := NewFactStore(s.database).CountFacts(ctx, scopeID, candidate.GenerationID)
	if err != nil {
		return successorPrior{}, fmt.Errorf("count prior generation facts for scope %s: %w", scopeID, err)
	}
	prior.factCount = count
	return prior, nil
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

// ActivationMaintainer is the production maintenance port of the activation
// obligation consumer (#7584): for one claimed obligation it runs the
// partition-scoped pass on exactly the obligation's (scope, generation) and
// maps the owed partition's result to the consumer's contract. It never runs
// the whole-corpus pass; that stays on the ingester's epoch.
type ActivationMaintainer struct {
	pass func(context.Context, []OwedPartition) (TargetedMaintenanceResult, error)
}

var _ obligation.Maintainer = ActivationMaintainer{}

// NewActivationMaintainer returns the maintainer backed by store's
// RunDeferredRelationshipMaintenanceForPartitions.
func NewActivationMaintainer(store IngestionStore, tracer trace.Tracer, instruments *telemetry.Instruments) ActivationMaintainer {
	return ActivationMaintainer{pass: func(ctx context.Context, owed []OwedPartition) (TargetedMaintenanceResult, error) {
		return store.RunDeferredRelationshipMaintenanceForPartitions(ctx, tracer, instruments, owed)
	}}
}

// MaintainActivation runs the partition-scoped pass for the obligation.
//
// The owed partition's own outcome decides first, because the pass
// classifies before any refusal: not_active returns nil so the consumer's
// Finalize retires the obligation as obsolete on the raw active pointer, and
// inapplicable returns obligation.ErrInapplicable so the consumer
// retires it inapplicable. A typed catalog_changed, no_memo_baseline or
// closure_too_deep refusal becomes a hold with that reason (lease kept, retry
// at lease cadence, no fallback pass). A published phase returns nil so
// Finalize wakes and completes. A retry outcome, a missing outcome or any
// other error is a maintenance failure; the lease expires and the obligation
// is retried.
func (m ActivationMaintainer) MaintainActivation(ctx context.Context, work obligation.Obligation) error {
	owed := OwedPartition{ScopeID: work.ScopeID, GenerationID: work.GenerationID}
	result, err := m.pass(ctx, []OwedPartition{owed})
	kind, found := TargetedMaintenanceOutcomeKind(""), false
	for _, outcome := range result.Outcomes {
		if outcome.Partition == owed {
			kind, found = outcome.Kind, true
			break
		}
	}
	switch {
	case found && kind == TargetedMaintenanceNotActive:
		return nil
	case found && kind == TargetedMaintenanceInapplicable:
		return fmt.Errorf("%w: %w", obligation.ErrInapplicable, ErrTargetedMaintenanceInapplicable)
	case err != nil:
		switch reason := TargetedMaintenanceReason(err); reason {
		case ErrTargetedMaintenanceCatalogChanged.Reason(),
			ErrTargetedMaintenanceNoMemoBaseline.Reason(),
			ErrTargetedMaintenanceClosureTooDeep.Reason():
			return obligation.Hold(reason, err)
		}
		return fmt.Errorf("partition-scoped maintenance for %s/%s: %w", owed.ScopeID, owed.GenerationID, err)
	case !found:
		return fmt.Errorf("partition-scoped maintenance returned no outcome for %s/%s", owed.ScopeID, owed.GenerationID)
	case kind == TargetedMaintenancePublished:
		return nil
	default:
		return fmt.Errorf("owed partition %s/%s: %w", owed.ScopeID, owed.GenerationID, ErrTargetedMaintenanceRetry)
	}
}

// backwardEvidencePhaseCommitted reports whether partition already carries the
// backward_evidence_committed readiness row publishDeferredBackfillPartition
// writes.
func backwardEvidencePhaseCommitted(
	ctx context.Context,
	database db.ExecQueryer,
	partition backfill.ScopeGenerationPartition,
) (bool, error) {
	ready, found, err := NewGraphProjectionPhaseStateStore(database).Lookup(ctx, reducer.GraphProjectionPhaseKey{
		ScopeID:          partition.ScopeID,
		AcceptanceUnitID: partition.ScopeID,
		SourceRunID:      partition.GenerationID,
		GenerationID:     partition.GenerationID,
		Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
	}, reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
	if err != nil {
		return false, fmt.Errorf("read backward evidence phase for %q/%q: %w", partition.ScopeID, partition.GenerationID, err)
	}
	return found && ready, nil
}

// targetedCatalogBaselineQuery remains a local alias for the whole-pass
// unchanged-partition proof, which compares the shipped targeted guard.
const targetedCatalogBaselineQuery = targeted.CatalogBaselineQuery

// targetedStore supplies the targeted engine with its existing parent-owned
// phase, queue, and same-pass memo-gate implementations.
func (s IngestionStore) targetedStore() targeted.Store {
	return targeted.Store{
		Backfill:       s.backfillStore(),
		PhaseCommitted: backwardEvidencePhaseCommitted,
		GateReopen: func(ctx context.Context, domain string, items []targeted.WorkItem,
			skipped map[backfill.ScopeGenerationPartition]struct{},
		) ([]string, int) {
			refs := make([]reopenWorkItemRef, len(items))
			for i, item := range items {
				refs[i] = reopenWorkItemRef{WorkItemID: item.WorkItemID, Partition: item.Partition}
			}
			gate := applyReopenPartitionMemoGate(ctx, domain, refs, skipped, nil)
			ids := make([]string, len(gate.ToReopen))
			for i, item := range gate.ToReopen {
				ids[i] = item.WorkItemID
			}
			return ids, len(gate.Skipped)
		},
		ReopenSucceeded: func(ctx context.Context, tx db.Transaction, workItemID string) (bool, error) {
			return (ReducerQueue{database: tx, Now: s.Now}).ReopenSucceeded(ctx, workItemID)
		},
		CorrelationDomains: CrossScopeCorrelationReopenDomains(),
		Queries:            targeted.ProductionQueries,
	}
}

// RunDeferredRelationshipMaintenanceForPartitions runs the exact
// partition-scoped maintenance engine for the owed partitions.
func (s IngestionStore) RunDeferredRelationshipMaintenanceForPartitions(
	ctx context.Context, tracer trace.Tracer, instruments *telemetry.Instruments, owed []OwedPartition,
) (TargetedMaintenanceResult, error) {
	return s.targetedStore().RunDeferredRelationshipMaintenanceForPartitions(ctx, tracer, instruments, owed)
}

// OwedPartition names one activation obligation's scope and generation.
type OwedPartition = targeted.OwedPartition

// TargetedMaintenanceResult reports one targeted pass.
type TargetedMaintenanceResult = targeted.TargetedMaintenanceResult

// TargetedMaintenanceError is a stable typed refusal.
type TargetedMaintenanceError = targeted.TargetedMaintenanceError

// TargetedMaintenanceOutcomeKind names a per-owed result.
type TargetedMaintenanceOutcomeKind = targeted.TargetedMaintenanceOutcomeKind

// TargetedMaintenanceOutcome pairs an owed partition with its result.
type TargetedMaintenanceOutcome = targeted.TargetedMaintenanceOutcome

const (
	// TargetedMaintenancePublished reports a committed phase.
	TargetedMaintenancePublished = targeted.TargetedMaintenancePublished
	// TargetedMaintenanceNotActive reports a superseded generation.
	TargetedMaintenanceNotActive = targeted.TargetedMaintenanceNotActive
	// TargetedMaintenanceInapplicable reports a partition no pass can publish.
	TargetedMaintenanceInapplicable = targeted.TargetedMaintenanceInapplicable
	// TargetedMaintenanceRetry reports an active partition still awaiting a phase.
	TargetedMaintenanceRetry = targeted.TargetedMaintenanceRetry
)

var (
	// ErrTargetedMaintenanceCatalogChanged refuses a stale catalog.
	ErrTargetedMaintenanceCatalogChanged = targeted.ErrTargetedMaintenanceCatalogChanged
	// ErrTargetedMaintenanceNoMemoBaseline refuses an absent baseline.
	ErrTargetedMaintenanceNoMemoBaseline = targeted.ErrTargetedMaintenanceNoMemoBaseline
	// ErrTargetedMaintenanceClosureTooDeep refuses an unsettled closure.
	ErrTargetedMaintenanceClosureTooDeep = targeted.ErrTargetedMaintenanceClosureTooDeep
	// ErrTargetedMaintenanceInapplicable identifies an impossible phase.
	ErrTargetedMaintenanceInapplicable = targeted.ErrTargetedMaintenanceInapplicable
	// ErrTargetedMaintenanceNotActive identifies a superseded generation.
	ErrTargetedMaintenanceNotActive = targeted.ErrTargetedMaintenanceNotActive
	// ErrTargetedMaintenanceRetry identifies a phase still owed.
	ErrTargetedMaintenanceRetry = targeted.ErrTargetedMaintenanceRetry
)

// TargetedMaintenanceReasons returns the sorted closed error-label set.
func TargetedMaintenanceReasons() []string { return targeted.TargetedMaintenanceReasons() }

// TargetedMaintenanceReason maps an error to its stable label.
func TargetedMaintenanceReason(err error) string { return targeted.TargetedMaintenanceReason(err) }

// deriveQueryAtMarker keeps the shared fail-closed SQL derivation for root
// activation listings that use the targeted predicate marker.
func deriveQueryAtMarker(shipped, marker, conjunct string) string {
	return targeted.DeriveQueryAtMarker(shipped, marker, conjunct)
}

const correlationReopenStageMarker = targeted.CorrelationReopenStageMarker
