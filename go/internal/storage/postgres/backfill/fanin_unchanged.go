// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ListPartitionsMissingReadinessQuery returns the subset of the requested
// (scope_id, generation_id) partitions that have NO cross-repo
// backward-evidence readiness row. $1 and $2 are the zipped scope and
// generation arrays; $3 and $4 are the keyspace and phase the fan-in writes
// (reducer.GraphProjectionKeyspaceCrossRepoEvidence and
// reducer.GraphProjectionPhaseBackwardEvidenceCommitted), passed as parameters
// so the check cannot drift from the writer. Every predicate column is part of
// the graph_projection_phase_state primary key, in key order, so each requested
// partition is one primary-key probe.
const ListPartitionsMissingReadinessQuery = `
SELECT requested.scope_id, requested.generation_id
FROM unnest($1::text[], $2::text[]) AS requested(scope_id, generation_id)
WHERE NOT EXISTS (
    SELECT 1
    FROM graph_projection_phase_state AS phase
    WHERE phase.scope_id = requested.scope_id
      AND phase.acceptance_unit_id = requested.scope_id
      AND phase.source_run_id = requested.generation_id
      AND phase.generation_id = requested.generation_id
      AND phase.keyspace = $3
      AND phase.phase = $4
)
`

// deferredFanInSplit is the whole-pass fan-in's publication plan.
type deferredFanInSplit struct {
	// toPublish is every partition that still takes the per-partition
	// publication transaction, in the caller's sorted order.
	toPublish []ScopeGenerationPartition
	// skippedUnchanged counts unchanged partitions left untouched.
	skippedUnchanged int
	// unchangedMissingPhase counts unchanged partitions republished because
	// their readiness row was missing.
	unchangedMissingPhase int
}

// splitUnchangedDeferredPartitions removes from sorted every partition this
// pass provably did not change and whose readiness row already exists.
//
// # Invariant
//
// A partition is skipped only when all of these hold:
//
//  1. It was a memo hit at the START of this pass: its memo row carried the
//     current catalog fingerprint, so ApplyDeferredPartitionMemoGate skipped
//     its fact load and this pass derived no evidence from its own facts.
//  2. This pass inserted zero evidence rows under it
//     (ExcludePartitionsWithNewEvidence removed every partition with a
//     positive inserted count, so cross-partition evidence is covered).
//  3. Its (scope, generation) is unchanged: contributions is keyed by the
//     generation each batch read under its lock, and the batch kept a
//     repository only when that generation equals the fact-load snapshot the
//     memo hit was read against.
//  4. Its cross_repo_evidence backward_evidence_committed row exists now
//     (ListPartitionsMissingReadinessQuery, one batched read).
//
// Under 1-3 republishing would rewrite only phase committed_at/updated_at and
// memo committed_at with the same fingerprint, and no reader decides on those
// timestamps (readiness lookups and activation obligations test existence).
// Condition 4 makes the skip self-healing if a readiness row was ever removed.
//
// A pending generation without an active pointer can have its facts rewritten
// in place by a same-ID retry (upsertScopeGenerationQuery refreshes only
// pending rows; the fact upsert is ON CONFLICT (fact_id) DO UPDATE). The
// catalog fingerprint does not see that change. This skip does not widen that
// window: the memo gate already skipped the fact load in condition 1, and the
// republish this function replaces could not repair it either, because the
// memo write only upserts (an ArgoCD-bearing result withholds the upsert but
// never deletes the existing memo row).
//
// An error from the readiness check is not fatal: the plan degrades to
// publishing every partition, the pre-#8082 behaviour. Skipped partitions take
// no lock and open no transaction; the check is one statement on the pool.
func (s Store) splitUnchangedDeferredPartitions(
	ctx context.Context,
	sorted []ScopeGenerationPartition,
	unchanged map[ScopeGenerationPartition]struct{},
) deferredFanInSplit {
	if len(unchanged) == 0 {
		return deferredFanInSplit{toPublish: sorted}
	}
	candidates := make([]ScopeGenerationPartition, 0, len(unchanged))
	for _, partition := range sorted {
		if _, ok := unchanged[partition]; ok {
			candidates = append(candidates, partition)
		}
	}
	if len(candidates) == 0 {
		return deferredFanInSplit{toPublish: sorted}
	}
	missing, err := LoadPartitionsMissingReadiness(ctx, s.DB, candidates)
	if err != nil {
		log.Printf("deferred_backfill_fanin_unchanged_check_failed error=%q partitions=%d falling_back=true",
			err, len(candidates))
		return deferredFanInSplit{toPublish: sorted}
	}
	split := deferredFanInSplit{toPublish: make([]ScopeGenerationPartition, 0, len(sorted)-len(candidates)+len(missing))}
	for _, partition := range sorted {
		if _, ok := unchanged[partition]; ok {
			if _, gone := missing[partition]; !gone {
				split.skippedUnchanged++
				continue
			}
			split.unchangedMissingPhase++
		}
		split.toPublish = append(split.toPublish, partition)
	}
	log.Printf("deferred_backfill_fanin_unchanged_checked candidates=%d skipped_unchanged=%d republished_missing_phase=%d",
		len(candidates), split.skippedUnchanged, split.unchangedMissingPhase)
	return split
}

// LoadPartitionsMissingReadiness returns the partitions among candidates that
// have no cross-repo backward-evidence readiness row, in one query. An empty
// candidate list returns nil without a query; a nil queryer is an error, so a
// store without a database cannot silently skip publication.
func LoadPartitionsMissingReadiness(
	ctx context.Context,
	queryer db.Queryer,
	candidates []ScopeGenerationPartition,
) (map[ScopeGenerationPartition]struct{}, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if queryer == nil {
		return nil, fmt.Errorf("readiness check requires a database")
	}
	scopeIDs := make([]string, 0, len(candidates))
	generationIDs := make([]string, 0, len(candidates))
	for _, partition := range candidates {
		scopeIDs = append(scopeIDs, partition.ScopeID)
		generationIDs = append(generationIDs, partition.GenerationID)
	}
	rows, err := queryer.QueryContext(ctx, ListPartitionsMissingReadinessQuery,
		array.StringArray(scopeIDs), array.StringArray(generationIDs),
		string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
		string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted),
	)
	if err != nil {
		return nil, fmt.Errorf("list partitions missing backward evidence readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	missing := make(map[ScopeGenerationPartition]struct{})
	for rows.Next() {
		var partition ScopeGenerationPartition
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID); err != nil {
			return nil, fmt.Errorf("scan partition missing readiness: %w", err)
		}
		missing[partition] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partitions missing readiness: %w", err)
	}
	return missing, nil
}
