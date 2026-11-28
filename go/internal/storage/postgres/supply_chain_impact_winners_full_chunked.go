// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded chunked full rebuild of the #3389 canonical winners read model
// (ruled #9805 v3-full, re-sourced round 5). The full resweep walks the table
// as bounded keyed chunks over the supports table — the only copy of a
// finding since the #10880 write stop — the way the shipped keyed tick does:
//
//   - (a) each chunk is identifier-bounded by the existing chunk sizing
//     (chunkBounds, default 200 identifiers) and staged with the existing
//     batch builder (winnersKeyedIdentBatch, with its parameter-ceiling
//     guard). The chunk probe reuses the triple-guard flip
//     (winnersKeyedProbeVariant is the keyed twin; winnersFullChunkNarrowProbe
//     is this path's): supports serve candidate fact_id presence, through
//     the triple-guarded plain-column probe while the supports-ready gate is
//     open and through the supports payload-expression probe when it is
//     closed. Both variants read supports; no chunk statement reads
//     fact_records.
//   - (b) ranking reads PAYLOAD-derived canonical_key, priority_score and
//     has_payload_finding_id through the shipped detoast extraction over the
//     verbatim supports payload — never stored supports columns (a stored
//     derived column can go stale against the payload, so ordering by one
//     would move winners after a dirt the capture missed).
//   - (c) the winner and override payloads arrive by fact_id point lookup
//     over supports and the shipped final projection runs verbatim.
//   - (d) each chunk sorts at default work_mem: no SET work_mem anywhere on
//     this path. The resweep lock and the audit oracle are unchanged.
//
// Rejected on this path: a covering index, a precise-only path, a work_mem
// raise, and serialization to batch 1. Chunks run back to back in one
// resweep transaction; concurrency still comes from the keyed ticks and the
// single-flight lock, never from shrinking a chunk to one row.

// winnersFullStoredIdentsSQL selects the stored winners' identifiers: the
// same five columns the delete match reads, over the canonical winners
// table. The select list derives from winnersStoredIdentSelectList, which
// shares winnersStoredIdentC0 with the match tuple, so the enumeration's
// stored arm cannot drift from the delete's notion of identity.
var winnersFullStoredIdentsSQL = `SELECT ` + winnersStoredIdentSelectList("w") + `
FROM supply_chain_impact_canonical_winners AS w`

// buildWinnersFullIdentsSQL enumerates the full rebuild's identifier set
// once: the UNION of the live arm (the distinct payload-derived five-tuples
// of the exact supports candidate set,
// supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL, the row source the
// keyed tick and the audit run) and the stored arm (every identifier the
// winners table still holds). The stored arm is what lets a chunk delete a
// key the live set no longer produces: a tombstoned key, a rotated
// canonical key, or a key left behind by a resweep to zero. Without it the
// per-chunk deletes tile only the live set and the stored-only rows survive
// every resweep. Numbered deterministically so chunks page it in bounded
// ranges. The extraction reads the once-detosted record over the verbatim
// supports payload; liveness stays with the per-chunk live_gens join,
// exactly as the shipped supports probe resolves it.
var buildWinnersFullIdentsSQL = `
CREATE TEMP TABLE winners_full_idents ON COMMIT DROP AS
SELECT package_id, repository_id, subject_digest, c0, advisory_id,
       ROW_NUMBER() OVER (ORDER BY package_id, repository_id, subject_digest, c0, advisory_id) AS rn
FROM (
    SELECT ` + winnersIdentExprsAs() + `
    FROM (` + supplyChainImpactWinnerActiveFactsInnerFromSupportsSQL + `) AS fact` + winnersDetoastFactLateral + `
    UNION
    ` + winnersFullStoredIdentsSQL + `
) AS idents
`

// analyzeWinnersFullIdentsSQL gives the planner the real identifier count
// before the chunk pages run on it.
const analyzeWinnersFullIdentsSQL = `ANALYZE winners_full_idents`

// selectWinnersFullChunkPageSQL reads one bounded identifier page by its row
// range. rn is dense from the enumeration, so consecutive ranges tile the
// set with no overlap and no OFFSET rescan.
const selectWinnersFullChunkPageSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM winners_full_idents
WHERE rn > $1 AND rn <= $2
`

// createWinnersKeyedIdentsSQL creates the working identifier table the chunk
// probe and the chunk apply read. The keyed tick builds this table from its
// admitted window; the full chunks refill it per page, so the CREATE is
// empty and each chunk deletes and restages it.
const createWinnersKeyedIdentsSQL = `
CREATE TEMP TABLE winners_keyed_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP
`

// clearWinnersKeyedIdentsSQL empties the working identifier table before the
// next chunk page is staged.
const clearWinnersKeyedIdentsSQL = `DELETE FROM ` + winnersKeyedIdentsTable

// insertWinnersKeyedIdentsPrefix heads the batched INSERT that stages one
// chunk page. It packs through winnersKeyedIdentBatch, so the chunk inherits
// the parameter-ceiling guard and the staged columns cannot drift from the
// keyed chunk's.
const insertWinnersKeyedIdentsPrefix = `INSERT INTO ` + winnersKeyedIdentsTable + ` VALUES `

// createWinnersKeyedActiveFactsNarrowSQL creates the chunk active_facts temp
// the chunk select ranks over: the copy identity plus source_confidence plus
// the once-extracted detoast fields, in winnersKeyedNarrowTempSelectList
// order, without the payload (the payloads arrive by fact_id point lookup in
// the final select). Column order is pinned to that list by
// TestWinnersFullChunkNarrowTempMatchesSelectList, so an INSERT position can
// never land a field in the wrong column.
const createWinnersKeyedActiveFactsNarrowSQL = `
CREATE TEMP TABLE winners_keyed_active_facts (
    fact_id text, scope_id text, generation_id text, source_confidence text,
    cve_id text, advisory_id text, package_id text, purl text, product_criteria text,
    match_criteria_id text, observed_version text, requested_range text,
    impact_status text, repository_id text, subject_digest text, finding_id text,
    priority_score text, suppression_state text, ecosystem text, image_ref text,
    priority_bucket text, detection_profile text, match_reason text
) ON COMMIT DROP
`

// clearWinnersKeyedActiveFactsSQL empties the chunk active_facts temp before
// the next chunk fills it.
const clearWinnersKeyedActiveFactsSQL = `DELETE FROM ` + winnersKeyedActiveFactsTable

// clearWinnersKeyedNarrowSQL empties the probe temp before the next chunk
// probes into it.
const clearWinnersKeyedNarrowSQL = `DELETE FROM ` + winnersKeyedNarrowTable

// Create-head and insert-head pairs for the per-chunk INSERT derivations. A
// chunk reuses the existing CREATE builds with only the head swapped, so the
// probe, the live-generation set and the operator filter cannot drift between
// the first chunk and any later one. Each head must occur exactly once in its
// build, and TestWinnersFullChunkInsertDerivationsRoundTrip pins the swap.
const (
	winnersFullChunkNarrowCreateHead      = "\nCREATE TEMP TABLE " + winnersKeyedNarrowTable + " ON COMMIT DROP AS\n"
	winnersFullChunkNarrowInsertHead      = "INSERT INTO " + winnersKeyedNarrowTable + "\n"
	winnersFullChunkActiveFactsCreateHead = "\nCREATE TEMP TABLE " + winnersKeyedActiveFactsTable + " ON COMMIT DROP AS\n"
	winnersFullChunkActiveFactsInsertHead = "INSERT INTO " + winnersKeyedActiveFactsTable + "\n"
)

// winnersFullChunkInsertFromCreate derives a per-chunk INSERT build from an
// existing CREATE build by swapping only the head. A head that does not
// match exactly once means the CREATE text changed shape, and the INSERT
// would silently run against the wrong temp: it panics at package
// initialization instead.
func winnersFullChunkInsertFromCreate(name, createSQL, createHead, insertHead string) string {
	if n := strings.Count(createSQL, createHead); n != 1 {
		panic(fmt.Sprintf("winners full chunk %s: create head occurs %d times, want 1", name, n))
	}
	return strings.Replace(createSQL, createHead, insertHead, 1)
}

// insertWinnersKeyedNarrowSupportsFullChunkSQL probes one chunk page over
// supports with the shipped payload expression: the closed-gate fallback,
// derived from the supports-swapped whole-chunk probe so it cannot drift
// from what the keyed tick runs. The audit keeps the legacy fact_records
// statement as its derivation base, never as a runtime row source.
var insertWinnersKeyedNarrowSupportsFullChunkSQL = winnersFullChunkInsertFromCreate(
	"narrow probe", buildWinnersKeyedNarrowFromSupportsSQL,
	winnersFullChunkNarrowCreateHead, winnersFullChunkNarrowInsertHead)

// insertWinnersKeyedNarrowPresenceFullChunkSQL probes one chunk page over the
// supports table with the triple guard: candidate fact_id presence only,
// with no stored ranking columns and no payload text reads. It runs only
// while the supports-ready gate is open.
var insertWinnersKeyedNarrowPresenceFullChunkSQL = winnersFullChunkInsertFromCreate(
	"presence probe", buildWinnersKeyedNarrowKeyPresenceSQL,
	winnersFullChunkNarrowCreateHead, winnersFullChunkNarrowInsertHead)

// winnersFullChunkNarrowProbe selects the chunk probe: the triple-guarded
// presence probe over supports while the gate is open, the supports
// payload-expression probe once it is closed. Both read supports; the flip
// is the probe shape, never the row source.
func winnersFullChunkNarrowProbe(supportsReady bool) string {
	if supportsReady {
		return insertWinnersKeyedNarrowPresenceFullChunkSQL
	}
	return insertWinnersKeyedNarrowSupportsFullChunkSQL
}

// insertWinnersFullChunkActiveFactsSQL fills the chunk active_facts temp from
// the narrow probe output, once per chunk. It derives the per-chunk INSERT
// from the keyed tick's own supports narrow build, so the chunk and the tick
// cannot drift; the head swap is pinned by
// TestWinnersFullChunkInsertDerivationsRoundTrip.
var insertWinnersFullChunkActiveFactsSQL = winnersFullChunkInsertFromCreate(
	"chunk active facts", buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
	winnersFullChunkActiveFactsCreateHead, winnersFullChunkActiveFactsInsertHead)

// winnersFullChunkStreamBodySQL is the CTE-streaming winner body over
// supports: the shipped body with extraction moved to the narrow chunk
// temp and the payload fetched by fact_id for the emitted winner and
// override rows only. It is derived from the shipped body, never copied, and
// TestWinnersFullChunkStreamBodyReversesToShipped inverts the derivation
// byte-for-byte, so the two differ only by the declared point-lookup
// transform.
var winnersFullChunkStreamBodySQL = streamWinnersKeyedBody(supplyChainImpactWinnerBodySQL)

// winnersFullChunkSelectSQL is the winner select one full chunk ranks: the
// streamed body over the chunk's narrow active_facts temp.
var winnersFullChunkSelectSQL = `WITH active_facts AS (SELECT * FROM ` + winnersKeyedActiveFactsTable + `)` +
	winnersFullChunkStreamBodySQL

// applyWinnersFullChunkBodySQL recomputes one chunk's winners and writes only
// the rows that are new or changed, and deletes stored winners of the chunk's
// identifier set that the chunk no longer produces. It is the keyed apply
// body with the select swapped to the chunk's fact_records select; the
// changed/upserted/deleted shape and the row comparison are untouched, so one
// edit cannot move the chunk apply off the keyed contract.
var applyWinnersFullChunkBodySQL = func() string {
	const name = "full chunk apply"
	if n := strings.Count(applyWinnersKeyedBodySQL, winnersKeyedSelectSQL); n != 1 {
		panic(fmt.Sprintf("winners %s: keyed select occurs %d times, want 1", name, n))
	}
	return strings.Replace(applyWinnersKeyedBodySQL, winnersKeyedSelectSQL, winnersFullChunkSelectSQL, 1)
}()

// applyWinnersFullChunkSQL runs one chunk's apply and reports its counts. It
// carries no watermark write: the final stamp runs once, after every chunk.
// Its $1 is the stamp ceiling S the keyed body writes as materialized_at on
// every row it touches, bound per chunk exactly as the keyed apply binds
// its candidate.
var applyWinnersFullChunkSQL = applyWinnersFullChunkBodySQL + `
SELECT
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed)
`

// stampWinnersFullWatermarkSQL stamps the full resweep's watermark after the
// last chunk commits its winners: GREATEST(previous, LEAST(S, residual
// min - 1us)) (#11117), so a mark that committed after the drain stays queued
// with the watermark under it. It runs unconditionally, so it stamps even a
// resweep whose chunks produced zero winners.
const stampWinnersFullWatermarkSQL = `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, LEAST($1::timestamptz, (
    SELECT min(r.captured_at) - interval '1 microsecond'
    FROM (
        SELECT min(captured_at) AS captured_at FROM supply_chain_impact_winners_dirty_log
        UNION ALL
        SELECT min(captured_at) FROM supply_chain_impact_winners_dirty_staging
    ) AS r
)))
ON CONFLICT (singleton) DO UPDATE SET materialized_at = GREATEST(
    supply_chain_impact_winners_materialization.materialized_at, EXCLUDED.materialized_at)
RETURNING (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

// fullChunkSupportsProbeReady reports whether the full-chunk probe may use
// the triple-guarded plain-column presence probe. Any probe error reads as
// not ready: the chunk then runs the supports payload-expression probe, so
// an unreadable gate can never flip the full onto the guarded text. Both
// variants read supports; the full never refuses, it only picks the probe.
// The recorded outcome names the pick: served ran the presence probe,
// not_ready ran the payload-expression probe, failed read no gate and ran
// the payload-expression probe.
func fullChunkSupportsProbeReady(ctx context.Context, tx db.Transaction, instruments *telemetry.Instruments) bool {
	ready, err := SupplyChainImpactSupportsReady(ctx, tx)
	if err != nil {
		instruments.RecordSupplyChainImpactSupportsRead(ctx,
			telemetry.SupplyChainImpactSupportsReaderWinnersFull, telemetry.SupplyChainImpactSupportsReadFailed)
		return false
	}
	if !ready {
		instruments.RecordSupplyChainImpactSupportsRead(ctx,
			telemetry.SupplyChainImpactSupportsReaderWinnersFull, telemetry.SupplyChainImpactSupportsReadNotReady)
		return false
	}
	instruments.RecordSupplyChainImpactSupportsRead(ctx,
		telemetry.SupplyChainImpactSupportsReaderWinnersFull, telemetry.SupplyChainImpactSupportsReadServed)
	return true
}

// winnersFullChunkContractStatements lists the full-chunk statements that
// define what a resweep writes: the enumeration, the probe pair, the chunk
// active-facts refill, rank and apply, and the final stamp. The operator
// insert and the other shared supports builds live in the keyed section of
// the contract pin (the chunk runs them verbatim), so this list holds only
// what the chunk owns. The contract pin and the supports-input split test
// both read this list, so the two cannot drift.
func winnersFullChunkContractStatements() []string {
	return []string{
		buildWinnersFullIdentsSQL,
		selectWinnersFullChunkPageSQL,
		insertWinnersKeyedNarrowSupportsFullChunkSQL,
		insertWinnersKeyedNarrowPresenceFullChunkSQL,
		insertWinnersFullChunkActiveFactsSQL,
		winnersFullChunkSelectSQL,
		applyWinnersFullChunkSQL,
		stampWinnersFullWatermarkSQL,
	}
}

// runWinnersFullStamp runs the final watermark stamp and reads its one
// RETURNING row for arity: the value itself is re-read by the caller after
// GREATEST resolves it.
func runWinnersFullStamp(ctx context.Context, tx db.Transaction, stamp time.Time) error {
	rows, err := tx.QueryContext(ctx, stampWinnersFullWatermarkSQL, stamp)
	if err != nil {
		return fmt.Errorf("stamp supply chain impact winners full watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("stamp supply chain impact winners full watermark: %w", err)
		}
		return fmt.Errorf("stamp supply chain impact winners full watermark: statement returned no watermark")
	}
	// The RETURNING subquery reads the post-upsert value; it is scanned for
	// arity only, exactly as the keyed stamp does. On a first resweep the
	// plain-insert path's self-table subquery reads the pre-statement
	// snapshot and yields NULL while the inserted row itself carries the
	// stamp; the caller re-reads the committed watermark afterwards.
	var stamped sql.NullTime
	if err := rows.Scan(&stamped); err != nil {
		_ = rows.Close()
		return fmt.Errorf("scan supply chain impact winners full watermark: %w", err)
	}
	if rows.Next() {
		return fmt.Errorf("stamp supply chain impact winners full watermark: statement returned more than one row")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("stamp supply chain impact winners full watermark: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close supply chain impact winners full watermark: %w", err)
	}
	return nil
}
