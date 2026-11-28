// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Defaults of the chunked consume (#9805, Dec 6(ii)). The per-tick bound is
// on post-expansion identifiers AND on the pre-filter copies the 291 probe
// scans. The identifier-only bound was falsified on QA: at scope-expanded
// fan-out one identifier fans out to hundreds of copies (50,123 scanned over
// 200 identifiers on the replica; live ticks 150-260 s with 50-61k kept and
// scanned about equal), so an identifier chunk still admits an unbounded
// probe. The scanned-copies budget is the primary bound: kept facts always
// cost a subset of the scanned copies' heap fetches (kept <= scanned), so
// one scanned-copies unit bounds both heap terms with one in-statement
// observable. The identifier default is derived from the arbiter-required
// linearity probe (EXPLAIN ANALYZE BUFFERS of the apply statement at 400
// vs 4,000 identifiers, stored scale 5,000 winners, warm cache, disposable
// Postgres 18): 31.6ms vs 281.7ms, i.e. apply scales linearly at ~70us per
// identifier with a few ms of fixed winner-body cost (a third point at 1,000
// identifiers, 74.5ms, sits on the same line). The QA calibration uses
// the accepted QA anchors (shim 2026-11-10: ~71k stored winners, pre-chunk
// apply p95 4.04s at ~865 keys p95, i.e. a ~5ms/key upper envelope; ~13 kept
// facts per identifier): 200 identifiers imply on the order of a one-second
// apply, 5x under the 15s per-tick bar with headroom for cold I/O, and a
// ~10.5k-row QA backlog drains in short spaced ticks. The deleted node
// (identifier Semi Join over a stored-winners Seq Scan, 295 buffers at 5k
// stored) extrapolates to tens of milliseconds at the QA stored count, far
// below the narrow (B)-trigger's 1.5s, so no winners-table index is added.
//
//   - 5,000 admitted dirty rows bound the window the expansion reads; QA's
//     ~10.5k backlog at shim time is at least three windows. The existing
//     50,000-identifier cap still sends a backlog whose expansion exceeds it
//     to the full rebuild.
//
// The identifier bound stays 200. A 1000 bound was tried on QA (#10839) and
// rolled back: its first tick took 710.8 s for 237,063 kept rows, about 1.8x
// the per-kept-row cost of the 200 bound. See evidence-10839-chunk-bound-1000.md.
const (
	defaultWinnersChunkMaxDirtyRows = 5000
	defaultWinnersChunkMaxIdents    = 200
	// defaultWinnersChunkMaxScannedCopies is the per-tick scanned-copies
	// budget (Dec 6(ii)): about 10,000 pre-filter copies. Sized from the
	// Dec 5(a) theory proof on the oldest-first representative chunk: the
	// active-facts build costs about 0.97 ms per kept fact at cold I/O, so
	// 10k kept facts fit the 15 s per-tick bar, and live scanned copies
	// run about equal to kept facts, so one scanned-copies unit covers
	// both heap terms. A tick may admit one identifier past the budget
	// (the current identifier is always admitted and never split, iff its
	// probe finishes within ResweepStatementTimeout — see
	// buildWinnersKeyedChunk and eshu-hq/eshu#11442); the tick log
	// reports the actual scanned count, so the overshoot stays visible.
	defaultWinnersChunkMaxScannedCopies = 10000
)

// winnersChunkBounds is one keyed transaction's resolved chunk bounds.
type winnersChunkBounds struct {
	dirtyRows     int64
	identifiers   int64
	scannedCopies int64
}

func (s SupplyChainImpactWinnersStore) chunkBounds() winnersChunkBounds {
	b := winnersChunkBounds{
		dirtyRows:     int64(s.ChunkMaxDirtyRows),
		identifiers:   int64(s.ChunkMaxIdentifiers),
		scannedCopies: int64(s.ChunkMaxScannedCopies),
	}
	if b.dirtyRows <= 0 {
		b.dirtyRows = defaultWinnersChunkMaxDirtyRows
	}
	if b.identifiers <= 0 {
		b.identifiers = defaultWinnersChunkMaxIdents
	}
	if b.scannedCopies <= 0 {
		b.scannedCopies = defaultWinnersChunkMaxScannedCopies
	}
	return b
}

// winnersChunkOutcome is what one bounded build admitted: the identifiers it
// applies, whether the identifier set continued past the chunk, and which
// bound stopped admission.
type winnersChunkOutcome struct {
	identifiers int64
	truncated   bool
	// budgetTruncated reports the scanned-copies budget stopped admission
	// (as opposed to the identifier bound): the tick maps it to
	// ChunkTruncatedBy so the log distinguishes the two.
	budgetTruncated bool
	// keyPresenceProbe reports the tick probed with the triple-guarded
	// plain-column probe (#9975 round 6) instead of the shipped
	// payload-expression one: the key-presence fill marker was complete when
	// the chunk was built. It is recorded for future wiring, not for tests
	// or operator telemetry: an operator tells which probe text a tick ran
	// from the marker row and pg_stat_statements (the two texts are
	// distinct statements).
	keyPresenceProbe bool
}

// selectWinnersKeyedChunkIdentsSQL lists the identifier set's five-tuples
// oldest first (#10706), $1 of them: MIN(captured_at), tie-break MIN(seq),
// then the five-tuple for determinism. The caller asks for one past the
// bound, so a full page proves the set continues past this chunk. The first
// identifier is always admitted and never split, so even a bound of one
// drains (completion still needs the probe inside ResweepStatementTimeout;
// see eshu-hq/eshu#11442).
const selectWinnersKeyedChunkIdentsSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM ` + winnersKeyedIdentsTable + `
ORDER BY age_captured_at, age_seq, package_id, repository_id, subject_digest, c0, advisory_id
LIMIT $1
`

// createWinnersKeyedChunkIdentsSQL and insertWinnersKeyedChunkIdentPrefix record
// the identifiers this chunk applies, for the identifier-table restriction
// and the applied-row delete below.
const createWinnersKeyedChunkIdentsSQL = `
CREATE TEMP TABLE winners_keyed_chunk_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP
`

// indexWinnersKeyedChunkIdentsSQL turns the chunk-identifier probes of the
// scope-remainder convert and the applied delete into index probes: without
// it each of the expansion's 42k-class rows checks chunk membership with a
// sequential scan, the statistics-blind nested loop the capacity lane (#10839)
// bounds. Unique because the chunk is filled from the distinct identifier
// table. Session-local to the tick transaction, like the table.
const indexWinnersKeyedChunkIdentsSQL = `
CREATE UNIQUE INDEX winners_keyed_chunk_idents_all_idx ON winners_keyed_chunk_idents (package_id, repository_id, subject_digest, c0, advisory_id)
`

// analyzeWinnersKeyedChunkIdentsSQL gives the planner the real chunk size
// before the convert and the applied delete plan on it.
const analyzeWinnersKeyedChunkIdentsSQL = `ANALYZE ` + winnersKeyedChunkIdentsTable

// restrictWinnersKeyedIdentsToChunkSQL drops the identifiers this chunk does
// not apply, so the probe and the apply recompute (and delete stored winners)
// only inside the chunk. The dropped identifiers are not lost: their dirty
// rows were never deleted, or were converted to fact rows below.
var restrictWinnersKeyedIdentsToChunkSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS ident
WHERE NOT ` + winnersKeyedChunkIdentMatch("ident") + `
`

// winnersKeyedChunkIdentMatch reports, for a row carrying the five identifier
// columns under alias, whether the chunk applies its identifier.
func winnersKeyedChunkIdentMatch(alias string) string {
	return `EXISTS (
    SELECT 1 FROM ` + winnersKeyedChunkIdentsTable + ` AS chunk
    WHERE (chunk.package_id, chunk.repository_id, chunk.subject_digest, chunk.c0, chunk.advisory_id)
        = (` + alias + `.package_id, ` + alias + `.repository_id, ` + alias + `.subject_digest, ` + alias + `.c0, ` + alias + `.advisory_id)
)`
}

// buildWinnersKeyedChunk admits the chunk: the oldest-first budgeted prefix
// of the post-expansion identifier set (#10706 oldest-first), probed one
// identifier at a time into the narrow table until the identifier bound or
// the scanned-copies budget stops admission, then one batched statement into
// the chunk table, then restricts the identifier table to the admitted prefix
// so the active-facts build and the apply that follow are bounded by the
// same chunk. The first identifier is always admitted and never split, so
// a single hot identifier still makes progress and the log drains —
// provided its probe and the tick's following statements finish within
// ResweepStatementTimeout (120 s default): a probe past the guard aborts
// the tick with statement_timeout, rolls back with no progress, and
// re-ticks identically, because the over-cap full fallback keys on
// identifier count and stored fraction, never scanned volume (eshu-hq/eshu#11442).
// Past ~401k warm (~124k cold) copies for one identifier, the guard, not
// the budget, stops the tick. It reports whether the set continued past
// the chunk and which bound stopped it.
func buildWinnersKeyedChunk(
	ctx context.Context,
	tx db.Transaction,
	bounds winnersChunkBounds,
) (winnersChunkOutcome, error) {
	var outcome winnersChunkOutcome
	rows, err := tx.QueryContext(ctx, selectWinnersKeyedChunkIdentsSQL, bounds.identifiers+1)
	if err != nil {
		return outcome, fmt.Errorf("list supply chain impact winners chunk identifiers: %w", err)
	}
	var idents []winnersKeyedChunkIdent
	for rows.Next() {
		var id winnersKeyedChunkIdent
		if err := rows.Scan(&id.pkg, &id.repo, &id.digest, &id.c0, &id.advisory); err != nil {
			_ = rows.Close()
			return outcome, fmt.Errorf("scan supply chain impact winners chunk identifier: %w", err)
		}
		idents = append(idents, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return outcome, fmt.Errorf("list supply chain impact winners chunk identifiers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return outcome, fmt.Errorf("close supply chain impact winners chunk identifiers: %w", err)
	}
	if int64(len(idents)) > bounds.identifiers {
		outcome.truncated = true
		idents = idents[:bounds.identifiers]
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("create supply chain impact winners chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedBatchIdentsSQL); err != nil {
		return outcome, fmt.Errorf("create supply chain impact winners batch identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedNarrowSQL); err != nil {
		return outcome, fmt.Errorf("create supply chain impact winners keyed narrow probe: %w", err)
	}
	// The probe flip (#9975 round 6): the triple-guarded plain-column probe
	// is correct only once every support carries its key-presence bits, so
	// it runs only after the fill's disagreement-0 verification completes
	// the marker. Before that — including when the marker row does not exist
	// yet — the shipped payload-expression probe runs, which ignores the
	// bits and is correct on unfilled rows. One indexed row read per tick.
	outcome.keyPresenceProbe = winnersKeyPresenceFillCompleted(ctx, tx)
	probeBatchSQL := winnersKeyedProbeVariant(outcome.keyPresenceProbe)
	// Budgeted admission (Dec 6(ii)): probe the identifiers oldest-first one
	// at a time and stop admitting new ones once the scanned-copies budget
	// is spent. RowsAffected of the per-identifier probe is that
	// identifier's scanned-copy count, so the probe runs once per
	// identifier and the budget binds admission instead of counting after
	// the fact. The first identifier is always admitted and never split,
	// so a single hot identifier still makes progress and the log drains —
	// iff its probe finishes within ResweepStatementTimeout (see the
	// buildWinnersKeyedChunk contract and eshu-hq/eshu#11442): no
	// identifier's copies split across ticks, because only wholly probed
	// identifiers reach the chunk table below. A non-positive budget
	// resolves to the default, the same rule as chunkBounds: admission is
	// always bounded.
	budget := bounds.scannedCopies
	if budget <= 0 {
		budget = defaultWinnersChunkMaxScannedCopies
	}
	var admitted []winnersKeyedChunkIdent
	var scanned int64
	for i, id := range idents {
		if i > 0 && scanned >= budget {
			outcome.budgetTruncated = true
			break
		}
		if err := ctx.Err(); err != nil {
			return outcome, fmt.Errorf("build supply chain impact winners chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, deleteWinnersKeyedBatchIdentsSQL); err != nil {
			return outcome, fmt.Errorf("clear supply chain impact winners batch identifier: %w", err)
		}
		query, args, err := winnersKeyedIdentBatch(insertWinnersKeyedBatchIdentPrefix, []winnersKeyedChunkIdent{id})
		if err != nil {
			return outcome, fmt.Errorf("stage supply chain impact winners batch identifier: %w", err)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return outcome, fmt.Errorf("stage supply chain impact winners batch identifier: %w", err)
		}
		res, err := tx.ExecContext(ctx, probeBatchSQL)
		if err != nil {
			return outcome, fmt.Errorf("probe supply chain impact winners chunk scanned copies: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return outcome, fmt.Errorf("count supply chain impact winners chunk scanned copies: %w", err)
		}
		scanned += n
		admitted = append(admitted, id)
	}
	// One batched INSERT carries the admitted chunk (#10839): the admitted
	// set is the oldest-first budgeted prefix probed above, and a saturated
	// tick spends one round trip here for the chunk write. The first
	// identifier is still always admitted and never split, so a single hot
	// identifier keeps draining while its probe stays inside
	// ResweepStatementTimeout (see eshu-hq/eshu#11442).
	if len(admitted) > 0 {
		query, args, err := winnersKeyedChunkIdentBatch(admitted)
		if err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk identifiers: %w", err)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk identifiers: %w", err)
		}
	}
	outcome.identifiers = int64(len(admitted))
	// outcome.truncated keeps its identifier-bound meaning (the existing
	// Bounded and restrict sites OR in budgetTruncated beside it); the
	// reason mapping reads the pair, so a budget-only trip reports
	// scanned_copies while a double trip reports both.
	if _, err := tx.ExecContext(ctx, indexWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("index supply chain impact winners chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("analyze supply chain impact winners chunk identifiers: %w", err)
	}
	// Restrict on either truncation: identifiers outside the admitted prefix
	// — past the identifier bound or past the spent budget — stay queued
	// for the next tick, exactly like the identifier-bound restriction
	// before it.
	if outcome.truncated || outcome.budgetTruncated {
		if _, err := tx.ExecContext(ctx, restrictWinnersKeyedIdentsToChunkSQL); err != nil {
			return outcome, fmt.Errorf("restrict supply chain impact winners identifiers to chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedIdentsSQL); err != nil {
			return outcome, fmt.Errorf("analyze supply chain impact winners chunk identifiers: %w", err)
		}
	}
	return outcome, nil
}
