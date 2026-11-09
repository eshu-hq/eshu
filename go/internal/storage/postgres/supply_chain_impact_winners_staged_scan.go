// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Lane A of the staged stale-winners audit scan (eshu-hq/eshu#10451): the
// per-(scope, generation) batch candidate fetch plus the census enumerator.
// A standby that cannot run the single-statement audit fetches one narrow
// batch per active pair instead, and lane B compacts the streamed rows into
// a per-key winner map in memory.
//
// The batch reuses the shipped winner select's candidate expressions and
// eligibility predicates through the shared builders beside
// supplyChainImpactWinnerBodySQL, so the staged scan cannot drift from the
// single-statement scan it replaces. The batch never selects the wide fact
// row or the raw payload: only fact_id and the derived candidate columns
// cross the reader.

// ScopeGeneration is one active (scope, generation) pair of the census. The
// scope is the loop variable of the staged scan and is not stored per row.
type ScopeGeneration struct {
	ScopeID      string
	GenerationID string
}

// StagedRow is one narrow candidate row of a staged batch. FindingID carries
// the payload finding_id, or the canonical-key fallback on source rows; it
// stays empty on operator-suppression rows, where HasPayloadFindingID tells
// whether the payload carried one.
type StagedRow struct {
	FactID              string
	CanonicalKey        string
	FindingID           string
	PriorityScore       int
	HasPayloadFindingID bool
	SuppressionState    string
}

// stagedWinnersCensusSQL enumerates every active (scope, generation) pair
// once, in (scope_id, generation_id) index order. The join mirrors the
// shipped active_facts membership: the scope's pinned generation must itself
// be active, so a mid-pass generation flip leaves the retired generation out
// of the tick.
const stagedWinnersCensusSQL = `
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE generation.status = 'active'
ORDER BY scope.scope_id, scope.active_generation_id`

// stagedWinnersBatchActiveFactsSQL pins one (scope_id, generation_id) pair as
// parameters over the fact_records_supply_chain_impact_active_scan_idx
// partial index on (scope_id, generation_id, fact_id), whose predicate is
// the fact-kind and tombstone filter below. It projects only the columns the
// candidate expressions and eligibility predicates read.
const stagedWinnersBatchActiveFactsSQL = `WITH active_facts AS (
    SELECT fact.fact_id, fact.scope_id, fact.payload
    FROM fact_records AS fact
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
)`

// stagedWinnersNarrowColumns is the exact output shape of every staged batch:
// fact_id plus the derived candidate columns, never the wide row.
const stagedWinnersNarrowColumns = `fact_id, canonical_key, finding_id, priority_score, has_payload_finding_id, suppression_state`

// stagedWinnersSourceBatchSQL fetches the narrow candidate rows of one
// source pair: the shipped candidate select list minus the wide
// scope/payload columns, with the shipped eligibility predicates verbatim.
var stagedWinnersSourceBatchSQL = stagedWinnersBatchActiveFactsSQL + `,
source_candidates AS (
    SELECT
        fact.fact_id,
        ` + supplyChainImpactCanonicalKeyConcat("fact.payload", "            ", "        ") + ` AS canonical_key,
        ` + supplyChainImpactFindingIDCoalesce("fact.payload") + ` AS finding_id,
        ` + supplyChainImpactPriorityScoreExpr("fact.payload") + ` AS priority_score,
        ` + supplyChainImpactHasPayloadFindingIDExpr("fact.payload") + ` AS has_payload_finding_id,
        ` + supplyChainImpactSuppressionStateExpr("fact.payload") + ` AS suppression_state
    ` + supplyChainImpactSourceCandidateEligibilitySQL + `
)
SELECT ` + stagedWinnersNarrowColumns + `
FROM source_candidates
ORDER BY fact_id ASC`

// stagedWinnersOperatorBatchSQL fetches the narrow candidate rows of one
// operator-suppressions pair on the same struct, with finding_id empty: an
// operator payload carries suppression decisions, not source findings.
var stagedWinnersOperatorBatchSQL = stagedWinnersBatchActiveFactsSQL + `,
operator_candidates AS (
    SELECT
        fact.fact_id,
        ` + supplyChainImpactCanonicalKeyConcat("fact.payload", "            ", "        ") + ` AS canonical_key,
        '' AS finding_id,
        ` + supplyChainImpactPriorityScoreExpr("fact.payload") + ` AS priority_score,
        ` + supplyChainImpactHasPayloadFindingIDExpr("fact.payload") + ` AS has_payload_finding_id,
        ` + supplyChainImpactSuppressionStateExpr("fact.payload") + ` AS suppression_state
    FROM active_facts AS fact
    WHERE fact.scope_id = '` + supplyChainImpactOperatorScopeID + `'
      AND ` + supplyChainImpactOperatorSuppressionPredicate + `
)
SELECT ` + stagedWinnersNarrowColumns + `
FROM operator_candidates
ORDER BY fact_id ASC`

// CensusPairs returns every active (scope, generation) pair once, in index
// order, for the staged scan to fetch one batch per pair.
func CensusPairs(ctx context.Context, reader db.Queryer) ([]ScopeGeneration, error) {
	if reader == nil {
		return nil, fmt.Errorf("staged winners census database is required")
	}
	rows, err := reader.QueryContext(ctx, stagedWinnersCensusSQL)
	if err != nil {
		return nil, fmt.Errorf("list staged winners census pairs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	pairs := make([]ScopeGeneration, 0, 64)
	for rows.Next() {
		var pair ScopeGeneration
		if err := rows.Scan(&pair.ScopeID, &pair.GenerationID); err != nil {
			return nil, fmt.Errorf("scan staged winners census pair: %w", err)
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list staged winners census pairs: %w", err)
	}
	return pairs, nil
}

// StageBatch fetches the narrow candidate rows of one (scope, generation)
// pair. The scope selects the arm: the operator-suppressions scope reads the
// operator arm, every other scope reads the source arm. The scope itself is
// the caller's loop variable and is not stored per row.
func StageBatch(ctx context.Context, reader db.Queryer, scope, generation string) ([]StagedRow, error) {
	if reader == nil {
		return nil, fmt.Errorf("staged winners batch database is required")
	}
	if scope == "" || generation == "" {
		return nil, fmt.Errorf("staged winners batch scope and generation are required")
	}
	query := stagedWinnersSourceBatchSQL
	if scope == supplyChainImpactOperatorScopeID {
		query = stagedWinnersOperatorBatchSQL
	}
	rows, err := reader.QueryContext(ctx, query, scope, generation)
	if err != nil {
		return nil, fmt.Errorf("stage winners batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	staged := make([]StagedRow, 0, 256)
	for rows.Next() {
		var row StagedRow
		var priority, hasPayload int
		if err := rows.Scan(&row.FactID, &row.CanonicalKey, &row.FindingID, &priority, &hasPayload, &row.SuppressionState); err != nil {
			return nil, fmt.Errorf("scan staged winners batch row: %w", err)
		}
		row.PriorityScore = priority
		row.HasPayloadFindingID = hasPayload != 0
		staged = append(staged, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stage winners batch: %w", err)
	}
	return staged, nil
}
