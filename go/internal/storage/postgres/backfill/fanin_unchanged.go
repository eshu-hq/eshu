// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// activeGenerationForRequestedScope is ActiveScopeGenerationQuery with its $1
// scope bound to the unnest row, so the readiness check reads each scope's
// active generation exactly as the fan-in's under-lock re-read does
// (LoadActiveGenerationForScope). It is derived, never copied, so the two
// cannot drift.
var activeGenerationForRequestedScope = strings.Replace(
	ActiveScopeGenerationQuery, "WHERE generation.scope_id = $1", "WHERE generation.scope_id = requested.scope_id", 1,
)

// ListUnchangedPartitionsNeedingPublicationQuery returns, among the requested
// (scope_id, generation_id) partitions, the ones the whole-pass fan-in must
// still publish through the locked path, with why: readiness_missing when the
// cross-repo backward-evidence readiness row is absent, generation_advanced
// when the scope's active generation is no longer the requested one. $1 and $2
// are the zipped scope and generation arrays; $3 and $4 are the keyspace and
// phase the fan-in writes (reducer.GraphProjectionKeyspaceCrossRepoEvidence
// and reducer.GraphProjectionPhaseBackwardEvidenceCommitted), passed as
// parameters so the check cannot drift from the writer. The readiness probe
// uses every graph_projection_phase_state primary-key column in key order; the
// generation probe is the scope_generations_scope_latest_lookup_idx read
// LoadActiveGenerationForScope uses.
var ListUnchangedPartitionsNeedingPublicationQuery = `
SELECT checked.scope_id, checked.generation_id, checked.readiness_missing, checked.generation_advanced
FROM (
    SELECT
        requested.scope_id,
        requested.generation_id,
        NOT EXISTS (
            SELECT 1
            FROM graph_projection_phase_state AS phase
            WHERE phase.scope_id = requested.scope_id
              AND phase.acceptance_unit_id = requested.scope_id
              AND phase.source_run_id = requested.generation_id
              AND phase.generation_id = requested.generation_id
              AND phase.keyspace = $3
              AND phase.phase = $4
        ) AS readiness_missing,
        active.generation_id IS DISTINCT FROM requested.generation_id AS generation_advanced
    FROM unnest($1::text[], $2::text[]) AS requested(scope_id, generation_id)
    LEFT JOIN LATERAL (` + activeGenerationForRequestedScope + `) AS active ON true
) AS checked
WHERE checked.readiness_missing OR checked.generation_advanced
`

// unchangedPartitionState says why an unchanged-set partition still needs the
// locked publication path.
type unchangedPartitionState struct {
	readinessMissing   bool
	generationAdvanced bool
}

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
	// unchangedGenerationAdvanced counts unchanged-set partitions sent to the
	// locked path because their scope's active generation moved after the
	// batch; that path declines them with generation_advanced_since_batch.
	unchangedGenerationAdvanced int
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
//     generation each batch read under its lock, the batch kept a repository
//     only when that generation equals the fact-load snapshot the memo hit was
//     read against, and the batched check below finds the scope still on that
//     generation. A partition whose scope moved after its batch goes to the
//     locked path, whose under-lock re-read declines it with
//     generation_advanced_since_batch, as before #8082. A move that lands
//     after the check is counted unchanged_since_memo; either way the pass
//     writes nothing for the superseded generation.
//  4. Its cross_repo_evidence backward_evidence_committed row exists now.
//
// Conditions 3 and 4 are one batched read
// (ListUnchangedPartitionsNeedingPublicationQuery). Under 1-3 a republish
// would rewrite only phase committed_at/updated_at and memo committed_at with
// the same fingerprint. Readiness readers test that the phase row exists
// (GraphProjectionPhaseStateStore.Lookup, the activation phaseReadyQuery and
// obligation backlog). One reader does order on memo committed_at: the
// partition-scoped pass's targetedCatalogBaselineQuery picks each scope's
// latest memo by committed_at. That order still holds, because memos are
// written only for the scope's active generation under its lock and a
// superseded generation never becomes active again (#7130): a newer
// generation's first memo always commits after every memo of an older one, so
// committed_at orders a scope's memos by activation. Skipping a re-stamp keeps
// a memo at its first publication, which keeps that order. Condition 4 makes
// the skip self-healing if a readiness row was ever removed.
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
// An error from the check is not fatal: the plan degrades to publishing every
// partition, the pre-#8082 behaviour. Skipped partitions take no lock and open
// no transaction; the check is one statement on the pool.
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
	needing, err := loadUnchangedPartitionsNeedingPublication(ctx, s.DB, candidates)
	if err != nil {
		log.Printf("deferred_backfill_fanin_unchanged_check_failed error=%q partitions=%d falling_back=true",
			err, len(candidates))
		return deferredFanInSplit{toPublish: sorted}
	}
	split := deferredFanInSplit{toPublish: make([]ScopeGenerationPartition, 0, len(sorted)-len(candidates)+len(needing))}
	for _, partition := range sorted {
		if _, ok := unchanged[partition]; ok {
			state, needsPublication := needing[partition]
			switch {
			case !needsPublication:
				split.skippedUnchanged++
				continue
			case state.generationAdvanced:
				split.unchangedGenerationAdvanced++
			default:
				split.unchangedMissingPhase++
			}
		}
		split.toPublish = append(split.toPublish, partition)
	}
	log.Printf("deferred_backfill_fanin_unchanged_checked candidates=%d skipped_unchanged=%d republished_missing_phase=%d generation_advanced=%d",
		len(candidates), split.skippedUnchanged, split.unchangedMissingPhase, split.unchangedGenerationAdvanced)
	return split
}

// loadUnchangedPartitionsNeedingPublication runs
// ListUnchangedPartitionsNeedingPublicationQuery over candidates and returns
// the partitions that must still publish, with why. An empty candidate list
// returns nil without a query; a nil queryer is an error, so a store without a
// database cannot silently skip publication.
func loadUnchangedPartitionsNeedingPublication(
	ctx context.Context,
	queryer db.Queryer,
	candidates []ScopeGenerationPartition,
) (map[ScopeGenerationPartition]unchangedPartitionState, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if queryer == nil {
		return nil, fmt.Errorf("unchanged-partition check requires a database")
	}
	scopeIDs := make([]string, 0, len(candidates))
	generationIDs := make([]string, 0, len(candidates))
	for _, partition := range candidates {
		scopeIDs = append(scopeIDs, partition.ScopeID)
		generationIDs = append(generationIDs, partition.GenerationID)
	}
	rows, err := queryer.QueryContext(ctx, ListUnchangedPartitionsNeedingPublicationQuery,
		array.StringArray(scopeIDs), array.StringArray(generationIDs),
		string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
		string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted),
	)
	if err != nil {
		return nil, fmt.Errorf("list unchanged partitions needing publication: %w", err)
	}
	defer func() { _ = rows.Close() }()
	needing := make(map[ScopeGenerationPartition]unchangedPartitionState)
	for rows.Next() {
		var (
			partition ScopeGenerationPartition
			state     unchangedPartitionState
		)
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID, &state.readinessMissing, &state.generationAdvanced); err != nil {
			return nil, fmt.Errorf("scan unchanged partition needing publication: %w", err)
		}
		needing[partition] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unchanged partitions needing publication: %w", err)
	}
	return needing, nil
}

// ExcludePartitionsWithNewEvidence revises a same-pass memo-hit skip-set with
// the ground truth of what the pass's backfill actually wrote (issue #7636).
// skipped holds the partitions the fact loader skipped at pass start; a
// memo-hit partition can still receive NEW evidence during the pass when
// cross-partition discovery writes under its generation (for example a GCP
// cloud scope with no repository fact of its own). insertedRows maps each
// partition to the rows the pass's backfill actually INSERTED — ON CONFLICT
// DO NOTHING skips do not count — so a re-upsert of already-committed
// evidence never invalidates a skip. Every skipped partition with a positive
// count is excluded; the result is the set whose backward evidence the pass
// provably did NOT change.
//
// A nil skipped (gate failure upstream) stays nil: the reopen-all fallback is
// preserved, not converted into an empty skip-set. The input map is not
// mutated. Indexing a nil insertedRows is safe and excludes nothing.
func ExcludePartitionsWithNewEvidence(
	skipped map[ScopeGenerationPartition]struct{},
	insertedRows map[ScopeGenerationPartition]int64,
) map[ScopeGenerationPartition]struct{} {
	revised, unskipped := excludePartitionsWithNewEvidence(skipped, insertedRows)
	logSkipSetRevised(skipped, unskipped, revised)
	return revised
}

// excludePartitionsWithNewEvidence is ExcludePartitionsWithNewEvidence
// without the log line, so the whole pass can log it only once its fan-in
// has succeeded. It also returns how many partitions it excluded.
func excludePartitionsWithNewEvidence(
	skipped map[ScopeGenerationPartition]struct{},
	insertedRows map[ScopeGenerationPartition]int64,
) (map[ScopeGenerationPartition]struct{}, int) {
	if skipped == nil {
		return nil, 0
	}
	revised := make(map[ScopeGenerationPartition]struct{}, len(skipped))
	unskipped := 0
	for partition := range skipped {
		if insertedRows[partition] > 0 {
			unskipped++
			continue
		}
		revised[partition] = struct{}{}
	}
	return revised, unskipped
}

// logSkipSetRevised emits deferred_backfill_skip_set_revised; a nil skipped
// (the memo gate did not run) logs nothing.
func logSkipSetRevised(skipped map[ScopeGenerationPartition]struct{}, unskipped int, revised map[ScopeGenerationPartition]struct{}) {
	if skipped == nil {
		return
	}
	log.Printf(
		"deferred_backfill_skip_set_revised skipped_before=%d unskipped_new_evidence=%d skipped_after=%d",
		len(skipped), unskipped, len(revised),
	)
}
