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
// SOLELY on membership in that set and never re-reads the memo table: a work
// item is skipped only when its partition is a member of skippedThisPass.
//
// The nil check is deliberately on nil-ness, not len()==0: a same-pass call
// where the backfill skipped ZERO partitions (e.g. every candidate was a memo
// MISS, the exact RED scenario for this fix) must still dispatch to the
// skip-set path with an empty-but-non-nil set — every candidate then correctly
// falls through to ToReopen by set-membership, not by accidentally matching
// the standalone nil-skip-set fallback below. loadDeferredScopedFactsAcrossPartitions
// always allocates a non-nil map whenever its gate produced a real decision,
// specifically so this distinction holds.
//
// This is deliberately NOT a fresh memo-table lookup in the same-pass case.
// BackfillAllRelationshipEvidence, which always runs immediately before this
// gate in the same pass, WRITES a fresh memo row for every partition it just
// reprocessed (every partition NOT in skippedThisPass) before this gate ever
// runs. Re-reading the memo table here — the pre-fix design — could no longer
// distinguish "this partition's evidence was already committed before this
// pass started" from "this partition's evidence was JUST committed by this
// very pass," and a work item whose partition fell in the latter case was
// wrongly skipped even though brand-new backward evidence had just landed for
// it (issue #4770 P1, codex finding on PR #4816). Gating on the pass's own
// skip-set instead of a post-write re-read closes that gap: only a partition
// already provably unchanged BEFORE this pass began can ever be treated as
// reopen-redundant.
//
// When skippedThisPass is nil — every caller of the public
// ReopenDeploymentMappingWorkItems / ReopenCodeImportRepoEdgeWorkItems methods
// OTHER than RunDeferredRelationshipMaintenance, e.g. bootstrap-index's
// RelationshipMaintenanceCommitter phase (a separate pipeline phase, not the
// same pass as its own backfill call) and direct test calls — the gate falls
// back to the legacy memo-table lookup against currentFingerprint. That
// lookup is safe in the standalone case precisely because no backfill in this
// same call just committed a fresh memo row: any memo row the lookup finds
// reflects evidence committed by a PRIOR, already-fully-committed pass, not
// this call's own in-flight write.
//
// This is a correctness-preserving skip, not merely a scheduling one: when a
// partition is a memo hit (by either path), the deferred backfill did NOT
// re-run DiscoverEvidence/UpsertEvidenceFacts for it this pass (see
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
// The ArgoCD carve-out needs no special-casing in either path, by the same
// invariant applyDeferredPartitionMemoGate documents:
// writeDeferredBackfillPartitionMemos never writes a memo row for an
// ArgoCD-bearing partition, so an ArgoCD-bearing partition never lands in
// skippedThisPass and never matches the legacy memo lookup either — it is
// always a miss and therefore always reopens.
func applyReopenPartitionMemoGate(
	ctx context.Context,
	memoStore *deferredBackfillPartitionMemoStore,
	domain string,
	items []reopenWorkItemRef,
	currentFingerprint string,
	skippedThisPass map[scopeGenerationPartition]struct{},
	instruments *telemetry.Instruments,
) (reopenPartitionMemoGateResult, error) {
	if len(items) == 0 {
		return reopenPartitionMemoGateResult{}, nil
	}

	if skippedThisPass != nil {
		return applyReopenPartitionMemoGateFromSkipSet(ctx, domain, items, skippedThisPass, instruments), nil
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

	logReopenPartitionMemoGateResult(ctx, domain, len(items), result, instruments)
	return result, nil
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

// computeCurrentReopenCatalogFingerprint derives the SAME catalog fingerprint
// BackfillAllRelationshipEvidence computes for its pass, so a standalone reopen
// call (skippedThisPass == nil in applyReopenPartitionMemoGate) compares
// against the fingerprint a PRIOR, already-committed pass's memo table was
// written under. It is a lightweight catalog load and hash — no fact scan.
// A nil queryer or an empty/unbuildable catalog
// (buildDeferredScopedFactQueryParams's ok=false case) returns an empty
// fingerprint, signalling the gate to fall back to the legacy
// unconditional-reopen contract rather than fabricate a fingerprint that could
// never match a real memo row.
//
// This intentionally diverges from the write side
// (backfillAllRelationshipEvidence, ingestion_backfill.go), which does NOT
// special-case ok=false: it always hashes the params (zero-value on ok=false)
// through deferredCatalogFingerprint, which returns a fixed non-empty digest
// even for zero-value input, and writeDeferredBackfillBatch writes a memo row
// under that fixed digest whenever catalogFingerprint != "" &&
// len(memoCandidates) > 0 — a condition that does not depend on ok, only on
// there being active repos to memoize. So an ok=false pass CAN legitimately
// produce a real memo row keyed to that fixed "empty-catalog" digest. The
// divergence is still safe because it can only ever bias TOWARD reopening,
// never toward an unsafe skip: this function's "" can never equal a stored
// "sha256:..." fingerprint (empty string vs. a 64-hex-char digest, by
// construction), so applyReopenPartitionMemoGate's currentFingerprint == ""
// short-circuit forces reopen-all every time, even on the rare pass where the
// write side did commit a real fixed-digest memo row for an empty catalog.
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
