// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SQL of the lane-A TOP-1-per-key rewrite (#11120). When the key-presence
// fill marker is complete, the keyed tick probes the new plain-column
// covering winner index (migration 366) once per identifier, keeps only
// narrow ranking rows, applies eligibility in bulk over the narrow set, and
// ranks per canonical key with a per-key point lookup instead of scanning,
// detoasting, and sorting every copy. The wide payload is fetched only for
// winners, exactly like the streamed body. The pre-marker payload-expression
// probe and the chunked-full 363 probe are untouched: the marker selects
// this path, and correctness never depends on the new index being valid
// (the planner degrades to heap paths on an unbuilt index).
//
// The winner body shares the streamed final select verbatim
// (winnersKeyedStreamedFinalSelectSQL), so the projection, payload merge,
// and operator overlay cannot drift from what the full path and audit read.

// Temp tables of one TOP-1 keyed build. Every one is created ON COMMIT DROP
// inside the tick transaction that uses it, like the legacy narrow table.
const (
	winnersKeyedTop1NarrowTable = "winners_keyed_top1_narrow"
	winnersKeyedTop1ActiveTable = "winners_keyed_top1_active"
	winnersKeyedTop1CountsTable = "winners_keyed_top1_counts"
	winnersKeyedTop1OpTable     = "winners_keyed_top1_op"
)

// winnersKeyedTop1NarrowColumns is the probe output column list: the copy
// identity plus the plain ranking inputs the winner step orders by. The
// narrow CREATE below declares these same six columns (the DDL parity scan
// reads only literal SQL); TestWinnersScannedCopiesProbeMatchesActiveFactsProbe
// pins the legacy agreement, and the TOP-1 hermetic pins below pin this one.
const winnersKeyedTop1NarrowColumns = `fact_id, scope_id, generation_id, canonical_key, priority_score, has_payload_finding_id`

// winnersTop1NarrowProbeSelect renders the TOP-1 identifier probe body over
// an identifier source table: one narrow row per pre-filter copy, carrying
// the plain ranking columns. The triple-guard legs are the shipped
// triple-guard equalities verbatim
// (TestWinnersTop1ProbeKeepsTripleGuardLegs pins each one), extended only by
// the ranking select list. The probe meters every five-tuple match including
// derived-image copies (F2): the counted set is the legacy pre-filter set,
// and the bulk active build below drops what the body would drop. Liveness
// and classification stay in the bulk active build, where hash joins serve
// them without per-row probes. The MATERIALIZED marker and OFFSET 0 guard
// are load-bearing for the same reason as the shipped probe's.
func winnersTop1NarrowProbeSelect(identsTable string) string {
	return `WITH narrow AS MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id,
        fact.canonical_key, fact.priority_score, fact.has_payload_finding_id
    FROM ` + identsTable + ` AS ident
    CROSS JOIN LATERAL (
        SELECT s.fact_id, s.scope_id, s.generation_id,
            s.canonical_key, s.priority_score, s.has_payload_finding_id
        FROM supply_chain_impact_finding_supports AS s
        WHERE s.package_id = ident.package_id
          AND (ident.package_id <> '' OR (s.key_presence & 1) <> 0)
          AND s.repository_id = ident.repository_id
          AND (ident.repository_id <> '' OR (s.key_presence & 2) <> 0)
          AND s.subject_digest = ident.subject_digest
          AND (ident.subject_digest <> '' OR (s.key_presence & 4) <> 0)
          AND COALESCE(NULLIF(s.cve_id, ''), NULLIF(s.advisory_id, ''), '') = ident.c0
          AND s.advisory_id = ident.advisory_id
        OFFSET 0
    ) AS fact
)
SELECT ` + winnersKeyedTop1NarrowColumns + ` FROM narrow
`
}

// createWinnersKeyedTop1NarrowSQL creates the budgeted probe's empty narrow
// table before the per-identifier accumulation fills it. Literal SQL (the
// DDL parity scan reads only literals): the six columns are the
// winnersKeyedTop1NarrowColumns output, with the supports row types for the
// ranking inputs.
const createWinnersKeyedTop1NarrowSQL = `
CREATE TEMP TABLE winners_keyed_top1_narrow (
    fact_id text, scope_id text, generation_id text,
    canonical_key text, priority_score integer, has_payload_finding_id smallint
) ON COMMIT DROP
`

// insertWinnersKeyedTop1NarrowBatchSQL probes exactly the staged identifier
// with the TOP-1 probe and appends its copies to the narrow table.
// RowsAffected is that identifier's scanned index-tuple count: the admission
// loop meters the redefined scanned-copies budget from it (index tuples
// consumed, no heap fetch), so the probe runs once per identifier and no
// second count statement re-reads the probe. The counted unit is redefined
// from pre-filter heap copies to pre-filter index tuples (decision 5); the
// bound value, the metering loop, and the stop behavior are unchanged, so a
// tick still stops at a finite, defaulted, test-pinned bound.
var insertWinnersKeyedTop1NarrowBatchSQL = `INSERT INTO ` + winnersKeyedTop1NarrowTable + `
` + winnersTop1NarrowProbeSelect(winnersKeyedBatchIdentsTable)

// analyzeWinnersKeyedTop1NarrowSQL gives the planner the real probe output
// size before the bulk eligibility build plans on it.
const analyzeWinnersKeyedTop1NarrowSQL = `ANALYZE ` + winnersKeyedTop1NarrowTable

// buildWinnersKeyedTop1LiveGensSQL materializes the tick-invariant live
// generation set once per tick (the #10839 shape): one row per live
// (scope_id, generation_id) pair. The bulk eligibility join reads this temp
// instead of probing liveness per copy.
const buildWinnersKeyedTop1LiveGensSQL = `
CREATE TEMP TABLE winners_keyed_top1_live_gens ON COMMIT DROP AS
SELECT s.scope_id, s.active_generation_id AS generation_id
FROM ingestion_scopes AS s
JOIN scope_generations AS generation
  ON generation.scope_id = s.scope_id
 AND generation.generation_id = s.active_generation_id
WHERE generation.status = 'active'
`

// analyzeWinnersKeyedTop1LiveGensSQL plans the bulk eligibility join on real
// live-set size.
const analyzeWinnersKeyedTop1LiveGensSQL = `ANALYZE winners_keyed_top1_live_gens`

// buildWinnersKeyedTop1ActiveSQL keeps the eligibility-filtered narrow rows:
// the live-generation semi-join plus the classification and scope guards,
// over narrow columns only. No payload is read or detoasted here; the COUNT
// and the winner step that follow see no heap. The operator scope is
// excluded from sources (its overlay rides the op temp below); the
// derived-image scope is excluded here, in the bulk build (F2): the probe
// meters derived copies in the pre-filter counted set, and this build drops
// them before ranking, exactly like the shipped body eligibility.
const buildWinnersKeyedTop1ActiveSQL = `
CREATE TEMP TABLE winners_keyed_top1_active ON COMMIT DROP AS
SELECT n.fact_id, n.scope_id, n.canonical_key, n.priority_score, n.has_payload_finding_id
FROM ` + winnersKeyedTop1NarrowTable + ` AS n
JOIN winners_keyed_top1_live_gens AS g
  ON g.scope_id = n.scope_id
 AND g.generation_id = n.generation_id
WHERE n.scope_id <> 'operator:vulnerability_suppressions'
  AND n.scope_id NOT LIKE 'image://derived/%'
  AND NOT EXISTS (
    SELECT 1 FROM scanner_image_result_fact_associations AS a
    WHERE a.finding_fact_id = n.fact_id
  )
  AND NOT EXISTS (
    SELECT 1 FROM scanner_image_result_fact_classifications AS c
    WHERE c.finding_fact_id = n.fact_id
      AND NOT (c.source_class = 'static' AND c.lifecycle = 'active')
  )
`

// indexWinnersKeyedTop1ActiveSQL materializes the winner ordering for the
// per-key point lookup: the covering index key order on the active set, so
// the winner step is an index-ordered LIMIT 1 per key with no Sort node at
// any scale (the planner will not pick a temp index scan for a whole-set
// DISTINCT ON at scratch scale, but always uses it for per-key LIMIT 1).
// No VACUUM runs here: VACUUM cannot run inside the tick transaction, and
// the temp heap is tiny — the few temp heap fetches are explained noise,
// while the order (the property the winner step needs) holds regardless of
// the visibility map. The Heap Fetches 0 bar applies to the covering winner
// index on supports, whose visibility map autovacuum maintains.
const indexWinnersKeyedTop1ActiveSQL = `
CREATE INDEX winners_keyed_top1_active_winner_idx ON winners_keyed_top1_active
    (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC)
`

// analyzeWinnersKeyedTop1ActiveSQL gives the planner the real active size
// before the counts and winner step plan on it.
const analyzeWinnersKeyedTop1ActiveSQL = `ANALYZE ` + winnersKeyedTop1ActiveTable

// buildWinnersKeyedTop1CountsSQL counts the eligible copies per canonical key
// in one HashAggregate pass over the narrow active set: the index-only
// source_count replacement (same cost class as the probe). It reads no
// payload and sorts nothing.
const buildWinnersKeyedTop1CountsSQL = `
CREATE TEMP TABLE winners_keyed_top1_counts ON COMMIT DROP AS
SELECT canonical_key, count(*) AS source_count FROM ` + winnersKeyedTop1ActiveTable + ` GROUP BY canonical_key
`

// indexWinnersKeyedTop1CountsSQL steers the final assembly joins to nested
// loop instead of merge-sorting the per-key inputs (the counts side would
// otherwise sort its tens of rows for a merge join).
const indexWinnersKeyedTop1CountsSQL = `
CREATE INDEX winners_keyed_top1_counts_key_idx ON winners_keyed_top1_counts (canonical_key)
`

// analyzeWinnersKeyedTop1CountsSQL plans the final assembly on real counts.
const analyzeWinnersKeyedTop1CountsSQL = `ANALYZE ` + winnersKeyedTop1CountsTable

// buildWinnersKeyedTop1OpSQL stages the operator overlay rows for the tick's
// keys: every operator suppression support in the ACTIVE operator
// generation, read through the scope-generation index (the production
// operator insert shape), never heap scanned. The liveness join is
// load-bearing: without it a superseded operator generation's overlay would
// never die (the removal tick would keep joining the dead row). The final
// LEFT JOIN restricts to the tick's keys by canonical key.
const buildWinnersKeyedTop1OpSQL = `
CREATE TEMP TABLE winners_keyed_top1_op ON COMMIT DROP AS
SELECT s.canonical_key, s.fact_id, s.suppression_state, s.priority_score, s.has_payload_finding_id
FROM supply_chain_impact_finding_supports AS s
JOIN ingestion_scopes AS scope
  ON scope.scope_id = s.scope_id
 AND scope.active_generation_id = s.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = s.scope_id
 AND generation.generation_id = s.generation_id
WHERE s.scope_id = 'operator:vulnerability_suppressions'
  AND generation.status = 'active'
  AND s.suppression_state <> 'active'
`

// indexWinnersKeyedTop1OpSQL materializes the winner ordering for the
// per-key override point lookup (F3): the covering winner-index key order on
// the op temp, so the override step is an index-ordered LIMIT 1 per source
// key with no Sort node at any scale, exactly like the winner step. The
// DISTINCT ON it replaces sorted the whole op temp through a Sort node;
// driving from the counts temp also skips operator-only noise keys the final
// LEFT JOIN could never match.
const indexWinnersKeyedTop1OpSQL = `
CREATE INDEX winners_keyed_top1_op_key_idx ON winners_keyed_top1_op
    (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC)
`

// analyzeWinnersKeyedTop1OpSQL plans the override join on real input.
const analyzeWinnersKeyedTop1OpSQL = `ANALYZE ` + winnersKeyedTop1OpTable

// winnersKeyedStreamedFinalAnchor is the exact seam where the streamed winner
// body turns into its final projection: the final SELECT over the winner and
// override CTEs. The TOP-1 body reuses everything from this seam verbatim.
// extractWinnersKeyedStreamedFinal panics unless the seam occurs exactly
// once, so a change to the shipped body fails loudly instead of running a
// split final.
const winnersKeyedStreamedFinalAnchor = "\nSELECT\n    source.canonical_key,"

// extractWinnersKeyedStreamedFinal returns the streamed winner body's final
// projection (the final SELECT through the end of the statement), shared
// verbatim by the TOP-1 body.
func extractWinnersKeyedStreamedFinal() string {
	if n := strings.Count(winnersKeyedStreamBodySQL, winnersKeyedStreamedFinalAnchor); n != 1 {
		panic(fmt.Sprintf("winners TOP-1 final seam occurs %d times, want 1", n))
	}
	_, tail, _ := strings.Cut(winnersKeyedStreamBodySQL, winnersKeyedStreamedFinalAnchor)
	return winnersKeyedStreamedFinalAnchor + tail
}

// winnersKeyedStreamedFinalSelectSQL is the shared final projection: the
// winner columns, the source and override payloads fetched by fact_id only
// for the emitted rows, and the operator suppression overlay. Both the
// streamed body and the TOP-1 body end in this text.
var winnersKeyedStreamedFinalSelectSQL = extractWinnersKeyedStreamedFinal()

// countWinnersKeyedTop1ScannedCopiesSQL counts the pre-filter index tuples
// the TOP-1 probe consumed: every finding the five-tuple probe matched,
// before the eligibility build keeps the live ones (#11308). It reads the
// persisted TOP-1 narrow table instead of re-running the probe, so a keyed
// tick probes once: the build and this count read the same rows from the
// same transaction and snapshot. Under the redefined budget the counted unit
// is index tuples consumed (no heap fetch), not pre-filter heap copies; the
// bound value, the metering loop, and the stop behavior are unchanged.
const countWinnersKeyedTop1ScannedCopiesSQL = `
SELECT count(*) FROM ` + winnersKeyedTop1NarrowTable + `
`

// countWinnersKeyedTop1KeptFactsSQL counts the kept facts the TOP-1 build
// kept, before the operator overlay is joined: the per-tick kept-fact
// volume the chunk bound implies. Redefinition (F6): the legacy kept count
// reads the active_facts temp after the live-generation join but before the
// body eligibility, while this count reads the post-eligibility active temp
// (live, classification, and scope guards already applied), so the same field
// counts a smaller set on the TOP-1 path. No control consumer reads it
// (emit-only span/log attribute), so this is an observability-contract
// restatement, not a bound change; the bound test's uniform all-eligible
// fan-out cannot distinguish the definitions by construction.
const countWinnersKeyedTop1KeptFactsSQL = `
SELECT count(*) FROM ` + winnersKeyedTop1ActiveTable + `
`

// winnersKeyedNarrowCreateSQL selects the budgeted narrow probe's empty table
// for one tick: the TOP-1 narrow once the key-presence fill marker is
// complete, the shipped narrow before that. The budgeted admission loop
// probes into exactly one of them per tick, so the create must match the
// probe text the variant selected.
func winnersKeyedNarrowCreateSQL(keyPresenceReady bool) string {
	if keyPresenceReady {
		return createWinnersKeyedTop1NarrowSQL
	}
	return createWinnersKeyedNarrowSQL
}

// winnersKeyedTop1Statements lists every TOP-1 statement the keyed tick runs
// on the marker-gated path: the narrow create the admission loop probes
// into, the budgeted probe, the phased temp builds, the two phase counts,
// and the TOP-1 apply. Tests pin shape and row-source properties over this
// closed set, so a later statement cannot join the path unpinned.
func winnersKeyedTop1Statements() []string {
	return []string{
		createWinnersKeyedTop1NarrowSQL,
		insertWinnersKeyedTop1NarrowBatchSQL,
		analyzeWinnersKeyedTop1NarrowSQL,
		buildWinnersKeyedTop1LiveGensSQL,
		analyzeWinnersKeyedTop1LiveGensSQL,
		buildWinnersKeyedTop1ActiveSQL,
		indexWinnersKeyedTop1ActiveSQL,
		analyzeWinnersKeyedTop1ActiveSQL,
		buildWinnersKeyedTop1CountsSQL,
		indexWinnersKeyedTop1CountsSQL,
		analyzeWinnersKeyedTop1CountsSQL,
		buildWinnersKeyedTop1OpSQL,
		indexWinnersKeyedTop1OpSQL,
		analyzeWinnersKeyedTop1OpSQL,
		countWinnersKeyedTop1ScannedCopiesSQL,
		countWinnersKeyedTop1KeptFactsSQL,
		applyWinnersKeyedTop1SQL,
	}
}

// buildWinnersKeyedTop1ActiveFacts runs the TOP-1 build over the budgeted
// narrow the chunk admission probed: analyze, the tick-invariant live set,
// the bulk eligibility build (narrow columns only, never the payload), the
// temp winner index that serves the per-key point lookup sort-free, the
// HashAggregate per-key counts, and the scope-fed operator overlay. It
// returns the kept facts the probe built and the pre-filter index tuples it
// consumed (the redefined scanned-copies unit). Every statement here is part
// of the build_active_facts phase and is covered by the winners contract.
func (s SupplyChainImpactWinnersStore) buildWinnersKeyedTop1ActiveFacts(
	ctx context.Context,
	tx db.Transaction,
) (kept, scanned int64, err error) {
	for _, stmt := range []string{
		analyzeWinnersKeyedTop1NarrowSQL,
		buildWinnersKeyedTop1LiveGensSQL,
		analyzeWinnersKeyedTop1LiveGensSQL,
		buildWinnersKeyedTop1ActiveSQL,
		indexWinnersKeyedTop1ActiveSQL,
		analyzeWinnersKeyedTop1ActiveSQL,
		buildWinnersKeyedTop1CountsSQL,
		indexWinnersKeyedTop1CountsSQL,
		analyzeWinnersKeyedTop1CountsSQL,
		buildWinnersKeyedTop1OpSQL,
		indexWinnersKeyedTop1OpSQL,
		analyzeWinnersKeyedTop1OpSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return 0, 0, fmt.Errorf("build supply chain impact winners keyed TOP-1 active facts: %w", err)
		}
	}
	if kept, err = scanWinnersInt64(ctx, tx, countWinnersKeyedTop1KeptFactsSQL, "count kept TOP-1 winner facts"); err != nil {
		return 0, 0, err
	}
	// The pre-filter scan count (#11308) rides the same phase: it counts the
	// persisted narrow rows, so the build's phase sample keeps covering both
	// reads while the probe runs once.
	if scanned, err = scanWinnersInt64(ctx, tx, countWinnersKeyedTop1ScannedCopiesSQL, "count scanned TOP-1 winner copies"); err != nil {
		return 0, 0, err
	}
	return kept, scanned, nil
}

// winnersKeyedTop1SelectSQL is the TOP-1 winner select over the tick's temp
// tables: per-key winners via point lookup over the temp winner index, the
// HashAggregate counts, and the ranked operator overlay, ending in the
// shared final select. source_winners exposes exactly the columns the final
// reads (narrow ranking rows enriched by one supports point lookup per
// winner for the carried projection columns). Ecosystem is read from the
// winner payload at the point lookup (F1), not from the plain column: the
// writer lowercases that column for filtering while the oracle body carries
// the fact's own spelling. operator_overrides matches the streamed override
// shape (fact_id for the payload fetch, suppression state for the overlay,
// rank columns for the per-key lateral below, sort-free by construction).
var winnersKeyedTop1SelectSQL = `WITH source_winners AS (
    SELECT k.canonical_key, w.fact_id, ws.scope_id, ws.finding_id, w.priority_score,
        k.source_count, ws.impact_status, COALESCE(ws.payload->>'ecosystem', '') AS ecosystem,
        ws.repository_id, ws.cve_id,
        ws.advisory_id, ws.package_id, ws.subject_digest, ws.image_ref, ws.priority_bucket,
        ws.detection_profile, ws.observed_version, ws.match_reason,
        ws.suppression_state, ws.source_confidence
    FROM ` + winnersKeyedTop1CountsTable + ` AS k
    CROSS JOIN LATERAL (
        SELECT a.fact_id, a.priority_score
        FROM ` + winnersKeyedTop1ActiveTable + ` AS a
        WHERE a.canonical_key = k.canonical_key
        ORDER BY a.priority_score DESC, a.has_payload_finding_id DESC, a.fact_id ASC
        LIMIT 1
    ) AS w
    JOIN supply_chain_impact_finding_supports AS ws ON ws.fact_id = w.fact_id
),
operator_overrides AS (
    SELECT k.canonical_key, o.fact_id, o.suppression_state
    FROM ` + winnersKeyedTop1CountsTable + ` AS k
    LEFT JOIN LATERAL (
        SELECT a.fact_id, a.suppression_state
        FROM ` + winnersKeyedTop1OpTable + ` AS a
        WHERE a.canonical_key = k.canonical_key
        ORDER BY a.priority_score DESC, a.has_payload_finding_id DESC, a.fact_id ASC
        LIMIT 1
    ) AS o ON true
)` + winnersKeyedStreamedFinalSelectSQL
