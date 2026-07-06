// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reopenWorkItemRef identifies one succeeded reducer work item and the
// (scope_id, generation_id) partition it belongs to. It is the row shape
// listSucceededDeploymentMappingWorkItemsQuery and
// listSucceededCodeImportRepoEdgeWorkItemsQuery return (issue #4770): the
// partition is what the reopen memo gate keys on, not the work item id.
type reopenWorkItemRef struct {
	WorkItemID string
	Partition  scopeGenerationPartition
}

// reopenPartitionMemoGateResult splits a reopen pass's succeeded work items
// into the subset that must still be reopened (partition memo miss, or the
// work item's own partition is blank) and the subset that can be skipped
// (a provable memo hit — see applyReopenPartitionMemoGate).
type reopenPartitionMemoGateResult struct {
	ToReopen []reopenWorkItemRef
	Skipped  []reopenWorkItemRef
}

// applyReopenPartitionMemoGate decides, for each succeeded deployment_mapping
// or code_import_repo_edge work item, whether replaying it this pass is
// redundant (issue #4770 / #3624 Track 2).
//
// skippedThisPass, when non-nil, is the EXACT set of (scope_id, generation_id)
// partitions BackfillAllRelationshipEvidence's Track-1 read-side memo gate
// (applyDeferredPartitionMemoGate) itself skipped at the START of THIS SAME
// pass (see loadDeferredAnchorScopedRelationshipFacts's doc comment). When the
// caller supplies it — RunDeferredRelationshipMaintenance is the only such
// caller, via reopenDeploymentMappingWorkItemsInTransaction — the gate keys
// SOLELY on membership in that set and never touches the memo table: a work
// item is skipped only when its partition is a member of skippedThisPass.
//
// The nil check is deliberately on nil-ness, not len()==0: a same-pass call
// where the backfill skipped ZERO partitions (e.g. every candidate was a memo
// MISS) must still dispatch to the skip-set path with an empty-but-non-nil
// set — every candidate then correctly falls through to ToReopen by
// set-membership, not by accidentally matching the nil (reopen-all) branch
// below. loadDeferredScopedFactsAcrossPartitions always allocates a non-nil
// map whenever its gate produced a real decision, specifically so this
// distinction holds.
//
// When skippedThisPass is nil — every caller of the public
// ReopenDeploymentMappingWorkItems / ReopenCodeImportRepoEdgeWorkItems methods
// OTHER than RunDeferredRelationshipMaintenance, e.g. bootstrap-index's
// RelationshipMaintenanceCommitter phase and direct test calls — the gate
// REOPENS EVERY CANDIDATE UNCONDITIONALLY. It does not fall back to a
// memo-table lookup.
//
// A prior version of this gate fell back to a "legacy" memo-table lookup
// (computeCurrentReopenCatalogFingerprint + a fresh LookupMany) on a nil
// skip-set, reasoning that no backfill in that same call had just written a
// fresh memo row. That reasoning does not hold for bootstrap-index:
// bootstrap_pipeline.go calls BackfillAllRelationshipEvidence (Phase 2, which
// WRITES a fresh memo row for every partition it reprocesses) and then the
// public Reopen* methods (Phase 4, with other phases and a projector drain
// wait in between, so it has no same-pass skip-set to thread) in the SAME
// bootstrap run. The legacy re-read could not distinguish "this partition's
// evidence committed before this bootstrap run started" from "this
// partition's evidence was JUST committed by Phase 2 of THIS SAME run," and a
// partition reprocessed by Phase 2 read back as a memo hit in Phase 4 and was
// wrongly skipped — even though the succeeded work item resolved before that
// fresh cross-repo evidence existed (issue #4770/#4816 hostile re-review
// finding: a regression versus main's unconditional-reopen behavior on this
// exact path). Reopen-all-on-nil removes the only caller-observable
// distinction that mattered (same-pass skip-set vs. everything else) and
// matches main's pre-#4770 behavior for every nil-skip-set caller.
//
// This is a correctness-preserving skip only in the skip-set case: when a
// partition is a member of skippedThisPass, the deferred backfill did NOT
// re-run DiscoverEvidence/UpsertEvidenceFacts for it THIS pass (see
// loadDeferredScopedFactsAcrossPartitions), so no NEW backward evidence
// committed for that partition since the reducer already resolved it.
// DiscoverEvidence, the cross-repo Resolve handler, and UpsertIntents are pure
// functions of (facts, catalog, assertions) with no read-back of their own
// prior output, and evidence rows are content-addressed with
// ON CONFLICT DO NOTHING, so replaying a work item whose partition saw no new
// evidence this pass would recompute byte-identical intents — the replay is
// provably redundant, not merely likely so.
//
// A work item whose partition is blank (defensive: the schema requires
// scope_id/generation_id NOT NULL, but a legacy row or a fake in a test may
// leave it empty) always reopens, matching the unconditional-reopen contract
// exactly rather than risking an unintended skip on unrecognized shape.
//
// The ArgoCD carve-out needs no special-casing in the skip-set path, by the
// same invariant applyDeferredPartitionMemoGate documents:
// writeDeferredBackfillPartitionMemos never writes a memo row for an
// ArgoCD-bearing partition, so an ArgoCD-bearing partition never lands in
// skippedThisPass — it is always a miss and therefore always reopens.
func applyReopenPartitionMemoGate(
	ctx context.Context,
	domain string,
	items []reopenWorkItemRef,
	skippedThisPass map[scopeGenerationPartition]struct{},
	instruments *telemetry.Instruments,
) reopenPartitionMemoGateResult {
	if len(items) == 0 {
		return reopenPartitionMemoGateResult{}
	}

	if skippedThisPass != nil {
		return applyReopenPartitionMemoGateFromSkipSet(ctx, domain, items, skippedThisPass, instruments)
	}

	// nil skip-set: reopen every candidate unconditionally. See the doc
	// comment above for why this must not fall back to a memo-table re-read.
	result := reopenPartitionMemoGateResult{ToReopen: items}
	logReopenPartitionMemoGateResult(ctx, domain, len(items), result, instruments)
	return result
}

// applyReopenPartitionMemoGateFromSkipSet applies the issue #4770/#4816
// same-pass gate: a work item is skipped only when its partition is a member
// of skippedThisPass, the EXACT set BackfillAllRelationshipEvidence's Track-1
// gate skipped at the start of this same pass. No memo table read happens
// here, by design — see applyReopenPartitionMemoGate's doc comment for why a
// memo re-read after this same pass's backfill commit is the bug this fixes.
func applyReopenPartitionMemoGateFromSkipSet(
	ctx context.Context,
	domain string,
	items []reopenWorkItemRef,
	skippedThisPass map[scopeGenerationPartition]struct{},
	instruments *telemetry.Instruments,
) reopenPartitionMemoGateResult {
	result := reopenPartitionMemoGateResult{
		ToReopen: make([]reopenWorkItemRef, 0, len(items)),
		Skipped:  make([]reopenWorkItemRef, 0, len(items)),
	}
	for _, item := range items {
		if item.Partition.ScopeID == "" || item.Partition.GenerationID == "" {
			result.ToReopen = append(result.ToReopen, item)
			continue
		}
		if _, skipped := skippedThisPass[item.Partition]; skipped {
			result.Skipped = append(result.Skipped, item)
			continue
		}
		result.ToReopen = append(result.ToReopen, item)
	}

	logReopenPartitionMemoGateResult(ctx, domain, len(items), result, instruments)
	return result
}

func logReopenPartitionMemoGateResult(
	ctx context.Context,
	domain string,
	candidateCount int,
	result reopenPartitionMemoGateResult,
	instruments *telemetry.Instruments,
) {
	if instruments != nil {
		instruments.ReopenSkippedByPartitionMemo.Add(ctx, int64(len(result.Skipped)),
			metric.WithAttributes(
				attribute.String("domain", domain),
				attribute.String("reason", "catalog_unchanged"),
			))
	}
	log.Printf(
		"reopen_partition_memo_gate_completed domain=%s candidate_work_items=%d skipped=%d reopened=%d",
		domain, candidateCount, len(result.Skipped), len(result.ToReopen),
	)
}
