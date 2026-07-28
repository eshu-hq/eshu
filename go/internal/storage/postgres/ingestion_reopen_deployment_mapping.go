// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RunDeferredRelationshipMaintenance runs the ingester's relationship backfill
// and then ONE reopen transaction covering everything the pass replays:
// deployment_mapping, code_import_repo_edge, and every domain in
// CrossScopeCorrelationReopenDomains. The backfill commits in bounded
// per-repository-batch transactions that each hold only their own repositories'
// exclusive maintenance locks, and the reopen runs in its own transaction. No
// step holds a fleet-wide lock, so a stall on one repository batch blocks only
// that batch's repositories; generation commits take the matching per-repository
// shared lock and wait only for maintenance touching their own repository.
//
// The reopen is not free and the pre-change baseline for its correlation half
// was zero: at 900 scopes x 25 generations it reopens one work item per active
// scope per correlation domain, one client round-trip each. See
// listSucceededReducerWorkItemsByDomainQuery and
// TestCorrelationReopenPerDrainCostProof for the measured cost and the bound
// that keeps it O(active scopes) rather than O(active scopes x generations).
func (s IngestionStore) RunDeferredRelationshipMaintenance(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required")
	}
	// Call the skip-set-returning variant directly (not the public
	// BackfillAllRelationshipEvidence wrapper) so this same stack frame can
	// thread the pass's own read-side skip decision straight into the reopen
	// step below, in memory, rather than letting the reopen gate re-derive it
	// from a memo table the backfill above has already written fresh rows
	// into (issue #4770 P1). See backfillAllRelationshipEvidence's doc comment.
	skippedPartitions, err := s.backfillAllRelationshipEvidence(ctx, tracer, instruments)
	if err != nil {
		return err
	}
	// One reopen transaction replays deployment_mapping, code_import_repo_edge,
	// and the cross-scope correlation domains. The first two share the same
	// after-the-fact dependency (cross-scope evidence committed by the backfill
	// above); the correlation domains wait on a different signal entirely and
	// are reopened for the reason documented on
	// crossScopeCorrelationReopenDomains.
	return s.reopenMaintenanceWorkItemsInTransaction(ctx, tracer, instruments, skippedPartitions)
}

// reopenMaintenanceWorkItemsInTransaction runs every corpus-wide reopen this
// maintenance pass owes in ONE transaction. Reopen is not partitioned by
// repository, so it takes no per-repository maintenance lock; it commits
// independently of the per-batch evidence writes. Reopen is idempotent, so a
// re-run after partial maintenance failure converges to the same queue state.
//
// One transaction, not one per domain group, for two reasons. The queue state a
// pass produces is then all-or-nothing: a partial reopen (deployment_mapping
// pending, the correlation chain not) never becomes observable, and a failure
// mid-pass leaves the queue exactly as the previous pass left it for the next
// drain to retry. And the added lock scope is the same kind already held —
// row locks on fact_work_items rows in status 'succeeded', which the reducer
// claim path never selects (it claims status IN ('pending', 'retrying',
// 'claimed', 'running') — claimReducerWorkQuery in
// reducer_queue_claim_query.go and the batch shape in
// reducer_queue_batch_query.go) — so widening the statement set does not widen
// the lock class or block a claim.
//
// skippedPartitions is RunDeferredRelationshipMaintenance's same-pass backfill
// skip-set (issue #4770 P1), threaded straight into the two relationship-domain
// reopens below in memory rather than re-derived from the memo table. It is
// deliberately NOT applied to the correlation reopen: that skip-set records
// which partitions committed no new BACKWARD EVIDENCE this pass, which says
// nothing about whether a producer scope's generation activated — the signal
// the correlation domains actually wait on. Gating them on it would skip
// exactly the replay the activation race needs.
func (s IngestionStore) reopenMaintenanceWorkItemsInTransaction(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	skippedPartitions map[scopeGenerationPartition]struct{},
) error {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deferred maintenance reopen transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	reopenStore := NewIngestionStore(tx)
	reopenStore.Now = s.Now
	reopenStore.Logger = s.Logger
	if err := reopenStore.reopenDeploymentMappingWorkItemsWithSkipSet(ctx, tracer, instruments, skippedPartitions); err != nil {
		return err
	}
	// Replay code_import_repo_edge in the same transaction: it shares the
	// after-the-fact dependency on cross-scope evidence and must re-run once that
	// evidence is committed, just like deployment_mapping.
	if err := reopenStore.reopenCodeImportRepoEdgeWorkItemsWithSkipSet(ctx, tracer, instruments, skippedPartitions); err != nil {
		return err
	}
	// Replay the cross-scope correlation chain in the same transaction. Before
	// this call the ingester replayed only the two relationship domains above,
	// so container_image_identity, ci_cd_run_correlation, and
	// supply_chain_impact were reopened solely by eshu-bootstrap-index: a
	// decision that lost the cross-scope activation race kept its empty-join
	// output forever under normal ingestion, and the golden-corpus gate — which
	// drives eshu-bootstrap-index for its maintenance passes — stayed green over
	// that gap.
	if err := reopenStore.ReopenSucceededReducerWorkItems(
		ctx, tracer, instruments, CrossScopeCorrelationReopenDomains(),
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deferred maintenance reopen transaction: %w", err)
	}
	committed = true
	return nil
}

// ReopenDeploymentMappingWorkItems replays succeeded deployment_mapping work
// items after deferred backward evidence is committed.
//
// This is a thin public wrapper over reopenDeploymentMappingWorkItemsWithSkipSet
// with a nil skip-set, keeping this method's signature unchanged for
// bootstrap-index's bootstrapCommitter interface. A nil skip-set means
// REOPEN EVERY CANDIDATE UNCONDITIONALLY (see applyReopenPartitionMemoGate's
// doc comment) — the safe default for any one-shot/standalone caller,
// including bootstrap-index, which calls BackfillAllRelationshipEvidence and
// this method as separate phases with other work in between
// (MaterializeIaCReachability, a projector drain wait), through a
// value-receiver interface, so it has no same-pass skip-set to offer.
// RunDeferredRelationshipMaintenance calls
// reopenDeploymentMappingWorkItemsWithSkipSet directly with the real
// same-pass skip-set instead of going through this wrapper, which gates only
// on that set's membership (issue #4770/#4816 same-pass optimization).
func (s IngestionStore) ReopenDeploymentMappingWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	return s.reopenDeploymentMappingWorkItemsWithSkipSet(ctx, tracer, instruments, nil)
}

// reopenDeploymentMappingWorkItemsWithSkipSet is
// ReopenDeploymentMappingWorkItems's implementation, gated by skippedPartitions
// — the set of (scope_id, generation_id) partitions this SAME maintenance
// pass's backfill step itself skipped (see applyReopenPartitionMemoGate's doc
// comment for why this must be a same-pass in-memory set). A nil
// skippedPartitions reopens every candidate unconditionally; a non-nil
// skippedPartitions gates solely on set membership.
func (s IngestionStore) reopenDeploymentMappingWorkItemsWithSkipSet(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	skippedPartitions map[scopeGenerationPartition]struct{},
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_deployment_mapping")
		defer span.End()
	}

	items, err := listSucceededDeploymentMappingWorkItems(ctx, s.db)
	if err != nil {
		return err
	}
	gateResult := applyReopenPartitionMemoGate(ctx, "deployment_mapping", items, skippedPartitions, instruments)

	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, item := range gateResult.ToReopen {
		if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
			return fmt.Errorf("reopen deployment_mapping work items: %w", err)
		}
	}

	if instruments != nil {
		instruments.DeploymentMappingReopened.Add(ctx, int64(len(gateResult.ToReopen)))
	}
	log.Printf(
		"deployment_mapping_reopened count=%d skipped_by_memo=%d",
		len(gateResult.ToReopen), len(gateResult.Skipped),
	)

	return nil
}

func listSucceededDeploymentMappingWorkItems(
	ctx context.Context,
	queryer Queryer,
) ([]reopenWorkItemRef, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededDeploymentMappingWorkItemsQuery)
	if err != nil {
		return nil, fmt.Errorf("list succeeded deployment_mapping work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]reopenWorkItemRef, 0)
	for rows.Next() {
		var item reopenWorkItemRef
		if err := rows.Scan(&item.WorkItemID, &item.Partition.ScopeID, &item.Partition.GenerationID); err != nil {
			return nil, fmt.Errorf("scan succeeded deployment_mapping work item: %w", err)
		}
		if strings.TrimSpace(item.WorkItemID) == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded deployment_mapping work items: %w", err)
	}
	return items, nil
}
