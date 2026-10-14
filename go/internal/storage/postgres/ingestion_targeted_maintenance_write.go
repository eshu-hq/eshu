// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"sort"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// targetedMaintenanceSkipSet is the same-pass memo-hit set over every
// partition the pass loads or reopens. Loaded partitions take the decision the
// fact loader already made; affected partitions that were not loaded are
// looked up through the same backfill.ApplyDeferredPartitionMemoGate before any memo
// row of this pass is written. A gate failure in either place yields nil,
// which reopens every relationship-domain candidate, and is logged with the
// same event the whole pass uses.
func (s IngestionStore) targetedMaintenanceSkipSet(
	ctx context.Context,
	fingerprint string,
	loadSkipped map[backfill.ScopeGenerationPartition]struct{},
	load map[backfill.ScopeGenerationPartition]struct{},
	affected map[backfill.ScopeGenerationPartition]struct{},
) map[backfill.ScopeGenerationPartition]struct{} {
	if loadSkipped == nil {
		return nil
	}
	skipped := make(map[backfill.ScopeGenerationPartition]struct{}, len(loadSkipped))
	for partition := range loadSkipped {
		skipped[partition] = struct{}{}
	}
	unloaded := make(map[backfill.ScopeGenerationPartition]struct{})
	for partition := range affected {
		if _, ok := load[partition]; !ok {
			unloaded[partition] = struct{}{}
		}
	}
	if len(unloaded) == 0 {
		return skipped
	}
	gate, err := backfill.ApplyDeferredPartitionMemoGate(
		ctx, backfill.NewDeferredBackfillPartitionMemoStore(s.database), sortedPartitions(unloaded), fingerprint, nil)
	if err != nil {
		log.Printf("deferred_backfill_partition_memo_gate_failed error=%q partitions=%d falling_back=true path=targeted",
			err, len(unloaded))
		return nil
	}
	for _, partition := range gate.Skipped {
		skipped[partition] = struct{}{}
	}
	return skipped
}

// writeTargetedMaintenanceEvidence commits the affected repositories' evidence
// in bounded per-repository batches with the repo-bounded under-lock read, and
// then publishes readiness and memo rows through the unchanged fan-in, which
// runs only after every batch of this pass committed. Shared instruments are
// off: the targeted pass has its own (see recordTargetedMaintenance). The
// second return carries the per-partition actually-inserted evidence row
// counts for the pass's memo-hit skip-set revision (issue #7636).
func (s IngestionStore) writeTargetedMaintenanceEvidence(
	ctx context.Context,
	closure targetedMaintenanceClosure,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshot map[string]string,
	catalogFingerprint string,
) (int, map[backfill.ScopeGenerationPartition]int64, error) {
	repoIDs := make([]string, 0, len(closure.affectedRepos))
	for repoID := range closure.affectedRepos {
		repoIDs = append(repoIDs, repoID)
	}
	if len(repoIDs) == 0 {
		return 0, nil, nil
	}
	sort.Strings(repoIDs)
	batchSize := s.maintenanceBatchSize
	if batchSize <= 0 {
		batchSize = backfill.DeferredMaintenanceRepoBatchSize
	}
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		bounds = append(bounds, [2]int{start, min(start+batchSize, len(repoIDs))})
	}
	workers := max(s.maintenanceWorkers, 1)
	workers = min(workers, len(bounds))

	contributions, insertedRows, err := s.backfillStore().RunDeferredBackfillBatchesWith(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshot, nil,
		loadActiveRepositoryGenerationsForRepos,
	)
	if err != nil {
		return 0, nil, err
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, fmt.Errorf("partition-scoped maintenance canceled before readiness publication: %w", err)
	}
	published, err := s.backfillStore().PublishDeferredBackfillPartitions(ctx, contributions, snapshot, catalogFingerprint, workers, nil)
	if err != nil {
		return 0, nil, err
	}
	return published, insertedRows, nil
}

// reopenTargetedMaintenanceWorkItems reopens, in ONE transaction, the
// succeeded reducer work items of the exact affected partitions: the
// deployment_mapping and code_import_repo_edge items gated by the same-pass
// memo-hit set, and every CrossScopeCorrelationReopenDomains item NOT gated by
// it (see reopenMaintenanceWorkItemsInTransaction for why). Each listing is the
// whole pass's shipped listing AND an exact-partition predicate.
//
// The correlation reopen is partition-scoped on purpose: the fleet-wide replay
// of those domains stays on the epoch whole pass and is not part of the
// activation obligation (#7584).
func (s IngestionStore) reopenTargetedMaintenanceWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	affected []backfill.ScopeGenerationPartition,
	skipped map[backfill.ScopeGenerationPartition]struct{},
) (map[string]int, error) {
	reopened := make(map[string]int)
	if len(affected) == 0 {
		return reopened, nil
	}
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_targeted_work_items")
		defer span.End()
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin partition-scoped reopen transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	queue := ReducerQueue{database: tx, Now: s.Now}

	for _, relationship := range []struct {
		domain string
		query  string
	}{
		{"deployment_mapping", listSucceededDeploymentMappingWorkItemsForPartitionsQuery},
		{"code_import_repo_edge", listSucceededCodeImportRepoEdgeWorkItemsForPartitionsQuery},
	} {
		block := startTargetedReopenBlock(ctx, tracer)
		items, err := listSucceededRelationshipWorkItemsForPartitions(ctx, tx, relationship.query, relationship.domain, affected)
		if err != nil {
			endTargetedReopenBlock(block, err, relationship.domain, 0, 0)
			return nil, err
		}
		gate := applyReopenPartitionMemoGate(ctx, relationship.domain, items, skipped, nil)
		for _, item := range gate.ToReopen {
			if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
				err = fmt.Errorf("reopen %s work items: %w", relationship.domain, err)
				endTargetedReopenBlock(block, err, relationship.domain, 0, len(gate.Skipped))
				return nil, err
			}
		}
		reopened[relationship.domain] = len(gate.ToReopen)
		endTargetedReopenBlock(block, nil, relationship.domain, len(gate.ToReopen), len(gate.Skipped))
	}
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		block := startTargetedReopenBlock(ctx, tracer)
		workItemIDs, err := listSucceededReducerWorkItemIDsForDomainAndPartitions(ctx, tx, domain, affected)
		if err != nil {
			endTargetedReopenBlock(block, err, domain, 0, 0)
			return nil, err
		}
		for _, workItemID := range workItemIDs {
			if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
				err = fmt.Errorf("reopen %s work items: %w", domain, err)
				endTargetedReopenBlock(block, err, domain, 0, 0)
				return nil, err
			}
		}
		reopened[domain] = len(workItemIDs)
		endTargetedReopenBlock(block, nil, domain, len(workItemIDs), 0)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit partition-scoped reopen transaction: %w", err)
	}
	committed = true
	return reopened, nil
}

// startTargetedReopenBlock starts the relationship.backfill.reopen block of one
// partition-scoped reopen domain call. It passes nil instruments on purpose:
// the targeted pass runs with instruments off so the whole pass's
// eshu_dp_deferred_backfill_* series keep measuring only the whole pass, and its
// reopen cost is read off the span and log line, tagged pass=targeted.
func startTargetedReopenBlock(ctx context.Context, tracer trace.Tracer) *backfill.BlockSpan {
	return backfill.StartBlock(ctx, tracer, nil, backfill.BlockReopen)
}

// endTargetedReopenBlock ends a block from startTargetedReopenBlock with the
// reopen block's domain, reopened, and skipped_by_memo attributes plus
// pass=targeted, so a targeted reopen is told apart from a whole-pass one.
func endTargetedReopenBlock(block *backfill.BlockSpan, err error, domain string, reopened, skippedByMemo int) {
	block.End(err, append(reopenBlockAttrs(domain, reopened, skippedByMemo), backfill.StringAttr("pass", "targeted"))...)
}
