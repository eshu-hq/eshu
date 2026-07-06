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
// and deployment-mapping reopen. The backfill commits in bounded
// per-repository-batch transactions that each hold only their own repositories'
// exclusive maintenance locks, and the reopen runs in its own transaction. No
// step holds a fleet-wide lock, so a stall on one repository batch blocks only
// that batch's repositories; generation commits take the matching per-repository
// shared lock and wait only for maintenance touching their own repository.
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
	// One reopen transaction replays both deployment_mapping and
	// code_import_repo_edge work items — they share the same after-the-fact
	// dependency (cross-scope evidence committed by the backfill above).
	return s.reopenDeploymentMappingWorkItemsInTransaction(ctx, tracer, instruments, skippedPartitions)
}

// reopenDeploymentMappingWorkItemsInTransaction runs the corpus-wide
// deployment-mapping reopen in its own transaction. Reopen is not partitioned by
// repository, so it takes no per-repository maintenance lock; it commits
// independently of the per-batch evidence writes. Reopen is idempotent, so a
// re-run after partial maintenance failure converges to the same queue state.
//
// skippedPartitions is RunDeferredRelationshipMaintenance's same-pass backfill
// skip-set (issue #4770 P1), threaded straight into both reopen calls below in
// memory rather than re-derived from the memo table.
func (s IngestionStore) reopenDeploymentMappingWorkItemsInTransaction(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	skippedPartitions map[scopeGenerationPartition]struct{},
) error {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deployment mapping reopen transaction: %w", err)
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deployment mapping reopen transaction: %w", err)
	}
	committed = true
	return nil
}

// ReopenDeploymentMappingWorkItems replays succeeded deployment_mapping work
// items after deferred backward evidence is committed.
//
// This is a thin public wrapper over reopenDeploymentMappingWorkItemsWithSkipSet
// with a nil skip-set, keeping this method's signature unchanged for
// bootstrap-index's bootstrapCommitter interface (issue #4770 P1 fix):
// bootstrap-index calls BackfillAllRelationshipEvidence and this method as
// separate phases with other work in between (MaterializeIaCReachability, a
// projector drain wait), through a value-receiver interface, so it has no
// same-pass skip-set to offer. A nil skip-set makes
// applyReopenPartitionMemoGate fall back to the legacy memo-table lookup
// (computeCurrentReopenCatalogFingerprint + a fresh LookupMany), which remains
// safe here specifically because no backfill call in THIS stack frame just
// wrote a fresh memo row a moment ago — any memo row this lookup finds
// reflects a prior, fully-committed pass. RunDeferredRelationshipMaintenance
// calls reopenDeploymentMappingWorkItemsWithSkipSet directly with the real
// same-pass skip-set instead of going through this wrapper, which is the
// issue #4770/#4816 same-pass fix: the same-pass caller never re-reads the
// memo table at all.
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
// comment for why this must be a same-pass in-memory set, never a memo-table
// re-read). A nil skippedPartitions falls back to the legacy
// computeCurrentReopenCatalogFingerprint + memo-table lookup inside
// applyReopenPartitionMemoGate; a non-nil skippedPartitions bypasses that
// lookup entirely and gates solely on set membership.
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

	var currentFingerprint string
	if skippedPartitions == nil {
		var fpErr error
		currentFingerprint, fpErr = computeCurrentReopenCatalogFingerprint(ctx, s.db)
		if fpErr != nil {
			log.Printf("reopen_partition_memo_fingerprint_failed domain=deployment_mapping error=%q falling_back=true", fpErr)
			currentFingerprint = ""
		}
	}

	items, err := listSucceededDeploymentMappingWorkItems(ctx, s.db)
	if err != nil {
		return err
	}
	gateResult, err := applyReopenPartitionMemoGate(
		ctx, newDeferredBackfillPartitionMemoStore(s.db), "deployment_mapping", items, currentFingerprint, skippedPartitions, instruments,
	)
	if err != nil {
		return fmt.Errorf("apply reopen partition memo gate for deployment_mapping: %w", err)
	}

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
