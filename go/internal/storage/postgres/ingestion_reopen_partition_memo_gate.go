// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
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
// (partition memo hit under the CURRENT catalog fingerprint).
type reopenPartitionMemoGateResult struct {
	ToReopen []reopenWorkItemRef
	Skipped  []reopenWorkItemRef
}

// applyReopenPartitionMemoGate decides, for each succeeded deployment_mapping
// or code_import_repo_edge work item, whether replaying it this pass is
// redundant (issue #4770 / #3624 Track 2). It mirrors
// applyDeferredPartitionMemoGate's read-side logic exactly: a work item is
// skipped only when its (scope_id, generation_id) partition already has a
// memo row (its backward evidence previously committed) whose
// catalog_fingerprint equals the current pass's fingerprint.
//
// This is a correctness-preserving skip, not merely a scheduling one: when a
// partition's memo is a hit, the deferred backfill did NOT re-run
// DiscoverEvidence/UpsertEvidenceFacts for it this pass (see
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
// leave it empty) always reopens, matching the legacy unconditional-reopen
// contract exactly rather than risking an unintended skip on unrecognized
// shape.
//
// The ArgoCD carve-out needs no special-casing here, by the same invariant
// applyDeferredPartitionMemoGate documents: writeDeferredBackfillPartitionMemos
// never writes a memo row for an ArgoCD-bearing partition, so an ArgoCD-bearing
// partition's work item is always a memo miss and therefore always reopens.
func applyReopenPartitionMemoGate(
	ctx context.Context,
	memoStore *deferredBackfillPartitionMemoStore,
	domain string,
	items []reopenWorkItemRef,
	currentFingerprint string,
	instruments *telemetry.Instruments,
) (reopenPartitionMemoGateResult, error) {
	if len(items) == 0 {
		return reopenPartitionMemoGateResult{}, nil
	}
	if memoStore == nil || currentFingerprint == "" {
		// No memo store or no computable fingerprint: fall back to the legacy
		// unconditional-reopen contract rather than guessing at redundancy. The
		// gate is a performance optimization, never a correctness dependency.
		return reopenPartitionMemoGateResult{ToReopen: items}, nil
	}

	partitions := make([]scopeGenerationPartition, 0, len(items))
	seen := make(map[scopeGenerationPartition]struct{}, len(items))
	for _, item := range items {
		if item.Partition.ScopeID == "" || item.Partition.GenerationID == "" {
			continue
		}
		if _, ok := seen[item.Partition]; ok {
			continue
		}
		seen[item.Partition] = struct{}{}
		partitions = append(partitions, item.Partition)
	}

	memos, err := memoStore.LookupMany(ctx, partitions)
	if err != nil {
		return reopenPartitionMemoGateResult{}, fmt.Errorf("lookup reopen partition memos for %s: %w", domain, err)
	}

	result := reopenPartitionMemoGateResult{
		ToReopen: make([]reopenWorkItemRef, 0, len(items)),
		Skipped:  make([]reopenWorkItemRef, 0, len(items)),
	}
	for _, item := range items {
		if item.Partition.ScopeID == "" || item.Partition.GenerationID == "" {
			result.ToReopen = append(result.ToReopen, item)
			continue
		}
		fingerprint, memoized := memos[item.Partition]
		if memoized && fingerprint == currentFingerprint {
			result.Skipped = append(result.Skipped, item)
			continue
		}
		result.ToReopen = append(result.ToReopen, item)
	}

	if instruments != nil {
		instruments.ReopenSkippedByPartitionMemo.Add(ctx, int64(len(result.Skipped)),
			metric.WithAttributes(
				attribute.String("domain", domain),
				attribute.String("reason", "catalog_unchanged"),
			))
	}
	log.Printf(
		"reopen_partition_memo_gate_completed domain=%s candidate_work_items=%d skipped=%d reopened=%d",
		domain, len(items), len(result.Skipped), len(result.ToReopen),
	)

	return result, nil
}

// computeCurrentReopenCatalogFingerprint derives the SAME catalog fingerprint
// BackfillAllRelationshipEvidence computes for the just-completed pass, so the
// reopen gate compares against the fingerprint the memo table was just written
// under. It is a lightweight catalog load and hash — no fact scan — safe to
// recompute once per RunDeferredRelationshipMaintenance call. A nil queryer or
// an empty/unbuildable catalog (buildDeferredScopedFactQueryParams's ok=false
// case) returns an empty fingerprint, signalling the gate to fall back to the
// legacy unconditional-reopen contract rather than fabricate a fingerprint that
// could never match a real memo row.
func computeCurrentReopenCatalogFingerprint(ctx context.Context, queryer Queryer) (string, error) {
	if queryer == nil {
		return "", nil
	}
	catalog, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		return "", fmt.Errorf("load repository catalog for reopen partition memo gate: %w", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		return "", nil
	}
	return deferredCatalogFingerprint(params), nil
}
