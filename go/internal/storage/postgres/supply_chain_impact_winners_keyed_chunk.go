// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Defaults of the chunked consume (#9805). The per-tick bound is on
// post-expansion identifiers; the kept facts the 291 five-tuple probe builds
// per identifier decide the heap and apply work, so no candidate-row budget
// is needed. The identifier default is derived from the arbiter-required
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
const (
	defaultWinnersChunkMaxDirtyRows = 5000
	defaultWinnersChunkMaxIdents    = 200
)

// winnersChunkBounds is one keyed transaction's resolved chunk bounds.
type winnersChunkBounds struct {
	dirtyRows   int64
	identifiers int64
}

func (s SupplyChainImpactWinnersStore) chunkBounds() winnersChunkBounds {
	b := winnersChunkBounds{
		dirtyRows:   int64(s.ChunkMaxDirtyRows),
		identifiers: int64(s.ChunkMaxIdentifiers),
	}
	if b.dirtyRows <= 0 {
		b.dirtyRows = defaultWinnersChunkMaxDirtyRows
	}
	if b.identifiers <= 0 {
		b.identifiers = defaultWinnersChunkMaxIdents
	}
	return b
}

// winnersChunkOutcome is what one bounded build admitted: the identifiers it
// applies and whether the identifier set continued past the chunk.
type winnersChunkOutcome struct {
	identifiers int64
	truncated   bool
}

// buildWinnersKeyedChunk admits the chunk: the first identifiers of the
// ordered post-expansion identifier set, one statement each into the chunk
// table, then restricts the identifier table to them so the probe and the
// apply that follow are bounded by the same chunk. The first identifier
// always completes, so a single hot identifier still makes progress and the
// log always drains. It reports whether the set continued past the chunk.
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
	type ident struct{ pkg, repo, digest, c0, advisory string }
	var idents []ident
	for rows.Next() {
		var id ident
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
	for _, id := range idents {
		if err := ctx.Err(); err != nil {
			return outcome, fmt.Errorf("build supply chain impact winners chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertWinnersKeyedChunkIdentSQL, id.pkg, id.repo, id.digest, id.c0, id.advisory); err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk identifier: %w", err)
		}
	}
	outcome.identifiers = int64(len(idents))
	if outcome.truncated {
		if _, err := tx.ExecContext(ctx, restrictWinnersKeyedIdentsToChunkSQL); err != nil {
			return outcome, fmt.Errorf("restrict supply chain impact winners identifiers to chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedIdentsSQL); err != nil {
			return outcome, fmt.Errorf("analyze supply chain impact winners chunk identifiers: %w", err)
		}
	}
	return outcome, nil
}

// settleWinnersKeyedDirtyLog makes the dirty log match what the chunk
// applied, inside the tick transaction: it converts a partially applied
// scope row's remainder to fact rows, deletes exactly the applied rows
// (deleteWinnersKeyedAppliedDirtySQL, or every row for the full-rebuild
// fallback), tallies the deleted rows by capture source, and counts what
// remains. The caller stamps the watermark only when nothing remains and the
// chunk was not truncated.
func settleWinnersKeyedDirtyLog(ctx context.Context, tx db.Transaction, fallback bool) (winnersCaptureTally, int64, error) {
	deleteSQL := deleteWinnersKeyedAppliedDirtySQL
	if fallback {
		deleteSQL = deleteWinnersKeyedAllDirtySQL
	} else if _, err := tx.ExecContext(ctx, convertWinnersKeyedScopeRemainderSQL); err != nil {
		return nil, 0, fmt.Errorf("convert supply chain impact winners scope remainder: %w", err)
	}
	rows, err := tx.QueryContext(ctx, deleteSQL)
	if err != nil {
		return nil, 0, fmt.Errorf("delete applied supply chain impact winners dirty rows: %w", err)
	}
	tally := winnersCaptureTally{}
	for rows.Next() {
		var source string
		var n int64
		if err := rows.Scan(&source, &n); err != nil {
			_ = rows.Close()
			return nil, 0, fmt.Errorf("scan deleted supply chain impact winners dirty rows: %w", err)
		}
		tally[source] += n
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, fmt.Errorf("delete applied supply chain impact winners dirty rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close deleted supply chain impact winners dirty rows: %w", err)
	}
	remaining, err := scanWinnersInt64(ctx, tx, countWinnersKeyedRemainingSQL, "count remaining dirty rows")
	if err != nil {
		return nil, 0, err
	}
	return tally, remaining, nil
}

// countWinnersKeyedKeptFacts counts the kept facts the identifier probe built.
// It runs before the operator facts are added, so it measures exactly what
// the identifier chunk implies per tick.
func countWinnersKeyedKeptFacts(ctx context.Context, tx db.Transaction) (int64, error) {
	return scanWinnersInt64(ctx, tx, countWinnersKeyedActiveFactsSQL, "count kept winner facts")
}
