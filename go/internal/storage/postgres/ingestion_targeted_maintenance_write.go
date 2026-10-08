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
)

// targetedMaintenanceSkipSet is the same-pass memo-hit set over every
// partition the pass loads or reopens. Loaded partitions take the decision the
// fact loader already made; affected partitions that were not loaded are
// looked up through the same applyDeferredPartitionMemoGate before any memo
// row of this pass is written. A gate failure in either place yields nil,
// which reopens every relationship-domain candidate, and is logged with the
// same event the whole pass uses.
func (s IngestionStore) targetedMaintenanceSkipSet(
	ctx context.Context,
	fingerprint string,
	loadSkipped map[scopeGenerationPartition]struct{},
	load map[scopeGenerationPartition]struct{},
	affected map[scopeGenerationPartition]struct{},
) map[scopeGenerationPartition]struct{} {
	if loadSkipped == nil {
		return nil
	}
	skipped := make(map[scopeGenerationPartition]struct{}, len(loadSkipped))
	for partition := range loadSkipped {
		skipped[partition] = struct{}{}
	}
	unloaded := make(map[scopeGenerationPartition]struct{})
	for partition := range affected {
		if _, ok := load[partition]; !ok {
			unloaded[partition] = struct{}{}
		}
	}
	if len(unloaded) == 0 {
		return skipped
	}
	gate, err := applyDeferredPartitionMemoGate(
		ctx, newDeferredBackfillPartitionMemoStore(s.database), sortedPartitions(unloaded), fingerprint, nil)
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
) (int, map[scopeGenerationPartition]int64, error) {
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
		batchSize = deferredMaintenanceRepoBatchSize
	}
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		bounds = append(bounds, [2]int{start, min(start+batchSize, len(repoIDs))})
	}
	workers := max(s.maintenanceWorkers, 1)
	workers = min(workers, len(bounds))

	contributions, insertedRows, err := s.runDeferredBackfillBatchesWith(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshot, nil,
		loadActiveRepositoryGenerationsForRepos,
	)
	if err != nil {
		return 0, nil, err
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, fmt.Errorf("partition-scoped maintenance canceled before readiness publication: %w", err)
	}
	published, err := s.publishDeferredBackfillPartitions(ctx, contributions, snapshot, catalogFingerprint, workers, nil)
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
	affected []scopeGenerationPartition,
	skipped map[scopeGenerationPartition]struct{},
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
		items, err := listSucceededRelationshipWorkItemsForPartitions(ctx, tx, relationship.query, relationship.domain, affected)
		if err != nil {
			return nil, err
		}
		gate := applyReopenPartitionMemoGate(ctx, relationship.domain, items, skipped, nil)
		for _, item := range gate.ToReopen {
			if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
				return nil, fmt.Errorf("reopen %s work items: %w", relationship.domain, err)
			}
		}
		reopened[relationship.domain] = len(gate.ToReopen)
	}
	for _, domain := range CrossScopeCorrelationReopenDomains() {
		workItemIDs, err := listSucceededReducerWorkItemIDsForDomainAndPartitions(ctx, tx, domain, affected)
		if err != nil {
			return nil, err
		}
		for _, workItemID := range workItemIDs {
			if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
				return nil, fmt.Errorf("reopen %s work items: %w", domain, err)
			}
		}
		reopened[domain] = len(workItemIDs)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit partition-scoped reopen transaction: %w", err)
	}
	committed = true
	return reopened, nil
}
