// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listArgoCDBearingPartitionsQuery batch-detects which of a set of candidate
// (scope_id, generation_id) partitions actually hold an ArgoCD ApplicationSet —
// the only evidence shape whose cross-repo config resolution can change when a
// DIFFERENT repo (the external config repo) changes (issue #3624 Track 1 / B',
// design point 4). Such partitions must always reload regardless of memo state.
//
// The signal is deliberately PRECISE, not the broad argoCDOverSelectAnchors used
// by listDeferredScopedRelationshipFactRecordsQuery's $1 over-select arm. Those
// anchors include the substrings "argocd_applications" / "argocd_applicationsets",
// which are EMPTY STRUCT KEYS that parsed_file_data serializes into EVERY parsed
// file's payload (even when the arrays are empty). Reusing them here would flag
// every partition as ArgoCD-bearing and force a full reload each pass — defeating
// the memo entirely (measured: all ~910 partitions matched, versus ~23 genuine
// ApplicationSet holders). Over-selection is safe for the $1 arm (the in-memory
// matcher refines it) but catastrophic for this carve-out, whose whole purpose is
// to reload the SMALL genuinely-cross-repo subset and skip the rest.
//
// A partition is ArgoCD-bearing when it has a fact that (a) mentions the ArgoCD
// API group argoproj.io — present in every ApplicationSet manifest's apiVersion,
// in both raw content and parsed source — (b) has a NON-EMPTY parsed
// argocd_applicationsets / argocd_applications array (the parser resolved an
// ApplicationSet), or (c) is a content fact classified artifact_type = argocd.
// This is a provable SUPERSET of genuine ApplicationSet holders (over-detection
// only costs an extra reload; under-detection would drop a cross-repo edge), and
// it never matches the empty struct keys. The jsonb_typeof guards keep the array
// checks safe when the field is null or a scalar rather than an array.
//
// The probe projects one row per matching partition and no payload, and its
// fact_records access is restricted to the caller's candidate partitions — one
// partition per fan-in publication transaction
// (publishDeferredBackfillPartition), never the whole corpus.
//
// It is not free, though, and the fan-in made it less so. The shared
// latestGenerationCTE this query is built on carries no scope filter, so it is
// evaluated corpus-wide on EVERY call: the candidate join restricts fact_records,
// not the CTE. Measured at 910 scopes / 10,920 generations that is 2.34 ms per
// call, and the fan-in calls it once per partition instead of once per batch
// (~910 calls carrying one candidate, rather than ~29 carrying 32), so the CTE
// now runs roughly 31x more often — about 2 s of aggregate CPU per pass. That
// cost is accepted to keep the check inside the transaction whose memo write it
// gates; the measurement and the rejected hoisting alternative are in
// docs/internal/evidence/deferred-backfill-shared-partition-dupkey.md.
const listArgoCDBearingPartitionsQuery = latestGenerationCTE + `
SELECT DISTINCT fact.scope_id, fact.generation_id
FROM fact_records AS fact
JOIN (
    SELECT * FROM unnest($1::text[], $2::text[]) AS requested(scope_id, generation_id)
) AS requested
  ON requested.scope_id = fact.scope_id
 AND requested.generation_id = fact.generation_id
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND (
    lower(fact.payload::text) LIKE '%argoproj.io%'
    OR (
      jsonb_typeof(fact.payload -> 'parsed_file_data' -> 'argocd_applicationsets') = 'array'
      AND jsonb_array_length(fact.payload -> 'parsed_file_data' -> 'argocd_applicationsets') > 0
    )
    OR (
      jsonb_typeof(fact.payload -> 'parsed_file_data' -> 'argocd_applications') = 'array'
      AND jsonb_array_length(fact.payload -> 'parsed_file_data' -> 'argocd_applications') > 0
    )
    OR lower(COALESCE(fact.payload ->> 'artifact_type', '')) = 'argocd'
  )
`

// loadArgoCDBearingPartitions returns the subset of candidatePartitions that hold
// a genuine ArgoCD ApplicationSet (listArgoCDBearingPartitionsQuery), batched in
// ONE query rather than one probe per partition. An empty candidate list
// short-circuits without a query.
func loadArgoCDBearingPartitions(
	ctx context.Context,
	queryer Queryer,
	candidatePartitions []scopeGenerationPartition,
) (map[scopeGenerationPartition]struct{}, error) {
	if queryer == nil || len(candidatePartitions) == 0 {
		return nil, nil
	}

	scopeIDs := make([]string, 0, len(candidatePartitions))
	generationIDs := make([]string, 0, len(candidatePartitions))
	for _, partition := range candidatePartitions {
		scopeIDs = append(scopeIDs, partition.ScopeID)
		generationIDs = append(generationIDs, partition.GenerationID)
	}

	rows, err := queryer.QueryContext(
		ctx,
		listArgoCDBearingPartitionsQuery,
		pq.StringArray(scopeIDs),
		pq.StringArray(generationIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("load argocd-bearing partitions for deferred backfill memo gate: %w", err)
	}
	defer func() { _ = rows.Close() }()

	bearing := make(map[scopeGenerationPartition]struct{})
	for rows.Next() {
		var partition scopeGenerationPartition
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID); err != nil {
			return nil, fmt.Errorf("scan argocd-bearing partition: %w", err)
		}
		bearing[partition] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate argocd-bearing partitions: %w", err)
	}

	return bearing, nil
}

// deferredPartitionMemoGateResult splits a pass's partitions into the subset that
// can skip the fact load (memo hit: committed under the same catalog fingerprint)
// and the subset that must load (memo miss or catalog changed).
type deferredPartitionMemoGateResult struct {
	ToLoad  []scopeGenerationPartition
	Skipped []scopeGenerationPartition
}

// applyDeferredPartitionMemoGate decides, for each candidate partition, whether
// its fact load can be skipped this pass (issue #3624 Track 1 / B'). A partition
// is skipped only when it already has a memo row (its backward evidence
// previously committed) whose catalog_fingerprint equals the current pass's
// fingerprint.
//
// The ArgoCD carve-out is enforced entirely on the WRITE side
// (writeDeferredBackfillPartitionMemos never records a memo row for an
// ArgoCD-bearing partition), so this read-side gate needs no ArgoCD probe: an
// ArgoCD-bearing partition simply never has a memo row and therefore always
// falls into ToLoad. That keeps the gate a single indexed memo lookup per pass —
// no per-partition payload scan — which is the entire point of the memo (a
// re-detection scan over every skip candidate would re-serialize the payloads
// the memo exists to avoid touching).
//
// A partition with no memo row is a miss and always loads, matching the legacy
// full-load contract exactly when the memo table is empty. The causes are
// bootstrap, never-before-committed, ArgoCD-bearing, and — since publication
// moved into the fan-in — a pass that committed the partition's evidence but
// withheld publication, because a sibling batch failed or was canceled, the
// fan-in itself failed, the generation advanced under it, or the process died
// before the fan-in ran. That last group is the recoverable direction the fan-in
// depends on: the evidence is durable, the reload re-derives it, and the
// re-upsert is a content-addressed no-op.
func applyDeferredPartitionMemoGate(
	ctx context.Context,
	memoStore *deferredBackfillPartitionMemoStore,
	partitions []scopeGenerationPartition,
	currentFingerprint string,
	instruments *telemetry.Instruments,
) (deferredPartitionMemoGateResult, error) {
	if len(partitions) == 0 {
		return deferredPartitionMemoGateResult{}, nil
	}

	memos, err := memoStore.LookupMany(ctx, partitions)
	if err != nil {
		return deferredPartitionMemoGateResult{}, fmt.Errorf("lookup deferred backfill partition memos: %w", err)
	}

	result := deferredPartitionMemoGateResult{
		ToLoad:  make([]scopeGenerationPartition, 0, len(partitions)),
		Skipped: make([]scopeGenerationPartition, 0, len(partitions)),
	}
	for _, partition := range partitions {
		fingerprint, memoized := memos[partition]
		if memoized && fingerprint == currentFingerprint {
			result.Skipped = append(result.Skipped, partition)
			continue
		}
		result.ToLoad = append(result.ToLoad, partition)
	}

	if instruments != nil {
		instruments.DeferredBackfillPartitionsSkipped.Add(ctx, int64(len(result.Skipped)),
			metric.WithAttributes(attribute.String("reason", "catalog_unchanged")))
		instruments.DeferredBackfillPartitionsLoaded.Add(ctx, int64(len(result.ToLoad)),
			metric.WithAttributes(attribute.String("reason", "memo_miss")))
	}
	log.Printf(
		"deferred_backfill_partition_memo_gate_completed candidate_partitions=%d skipped=%d loaded=%d",
		len(partitions), len(result.Skipped), len(result.ToLoad),
	)

	return result, nil
}

// writeDeferredBackfillPartitionMemos upserts the memo rows for the partitions
// the caller is publishing, EXCLUDING any ArgoCD-bearing partition so the memo
// table never claims completion for a partition the pass must always reload
// (see listArgoCDBearingPartitionsQuery).
//
// It runs inside the fan-in publication transaction
// (publishDeferredBackfillPartition), NOT inside an evidence batch. The memo row
// therefore commits atomically with that partition's phase row, and both commit
// strictly AFTER every evidence batch contributing to the partition has already
// committed in its own earlier transaction. A memo row consequently never exists
// without complete evidence and readiness behind it. The reverse — committed
// evidence with no memo row — is the tolerated, self-healing direction: such a
// partition is a gate MISS in applyDeferredPartitionMemoGate and reloads on the
// next pass.
//
// The ArgoCD-bearing check is one batched query over the caller's candidate set
// — never the whole corpus — and runs in this transaction so it observes a
// snapshot consistent with the MEMO WRITE it gates. That is deliberately not the
// evidence write's snapshot, which is earlier: a partition that became
// ArgoCD-bearing between its evidence commit and this transaction is seen as
// bearing here and denied a memo row, which is the safe direction, because a
// missing memo only costs a reload.
func writeDeferredBackfillPartitionMemos(
	ctx context.Context,
	tx Transaction,
	candidates []scopeGenerationPartition,
	catalogFingerprint string,
	committedAt time.Time,
) error {
	argoCDBearing, err := loadArgoCDBearingPartitions(ctx, tx, candidates)
	if err != nil {
		return fmt.Errorf("check argocd-bearing partitions before memo write: %w", err)
	}

	rows := make([]deferredBackfillPartitionMemoRow, 0, len(candidates))
	for _, partition := range candidates {
		if _, bearing := argoCDBearing[partition]; bearing {
			continue
		}
		rows = append(rows, deferredBackfillPartitionMemoRow{
			ScopeID:            partition.ScopeID,
			GenerationID:       partition.GenerationID,
			CatalogFingerprint: catalogFingerprint,
			CommittedAt:        committedAt,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if err := newDeferredBackfillPartitionMemoStore(tx).Upsert(ctx, rows); err != nil {
		return fmt.Errorf("upsert deferred backfill partition memos: %w", err)
	}
	return nil
}
