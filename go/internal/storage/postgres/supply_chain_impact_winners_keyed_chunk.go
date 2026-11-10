// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Defaults of the chunked consume (#9805 CODE-NEEDED-1), sized from the QA
// shim recorded in docs/internal/evidence/9805-suppression-expiry-and-tick-shim-20261110.md:
//
//   - The 25 hottest triples fetched 43,081 candidate rows at 1.144
//     buffers/row, mostly cold reads. A 50,000 candidate-row budget therefore
//     bounds one transaction at roughly the shim's 25-hot-triple probe
//     (~57k buffers, ~4.6s cold) plus its apply — far under the maintainer's
//     60s lease, with the 30s cadence between chunks, so a drain is a run of
//     short ticks, never back-to-back 60s ones.
//   - Winners per triple on QA were p50=2, p95=18, so 200 triples is a
//     typical tick of a few hundred keys and at most 200 probe statements,
//     while a tick of only hot triples stops on the row budget first.
//   - 5,000 admitted dirty rows bound the window the expansion reads; QA's
//     ~10.5k backlog at shim time is at least three windows. The existing
//     50,000-identifier cap still sends a backlog whose expansion exceeds it
//     to the full rebuild.
const (
	defaultWinnersChunkMaxDirtyRows     = 5000
	defaultWinnersChunkMaxTriples       = 200
	defaultWinnersChunkMaxCandidateRows = 50000
)

// winnersChunkBounds is one keyed transaction's resolved chunk bounds.
type winnersChunkBounds struct {
	dirtyRows     int64
	triples       int64
	candidateRows int64
}

func (s SupplyChainImpactWinnersStore) chunkBounds() winnersChunkBounds {
	b := winnersChunkBounds{
		dirtyRows:     int64(s.ChunkMaxDirtyRows),
		triples:       int64(s.ChunkMaxTriples),
		candidateRows: int64(s.ChunkMaxCandidateRows),
	}
	if b.dirtyRows <= 0 {
		b.dirtyRows = defaultWinnersChunkMaxDirtyRows
	}
	if b.triples <= 0 {
		b.triples = defaultWinnersChunkMaxTriples
	}
	if b.candidateRows <= 0 {
		b.candidateRows = defaultWinnersChunkMaxCandidateRows
	}
	return b
}

// winnersChunkOutcome is what one bounded build did: the triples probed, the
// candidate rows those probes fetched, and whether the identifier set
// continued past the chunk (more triples, or the row budget stopped it).
type winnersChunkOutcome struct {
	triples       int64
	candidateRows int64
	truncated     bool
}

// buildWinnersKeyedChunk builds the chunked active_facts: it probes the
// identifier set's distinct triples in fixed order, one statement per triple,
// and stops admitting triples at the triple bound or once the fetched
// candidate rows reach the row budget. The first triple always completes, so
// a single triple hotter than the whole budget still makes progress and the
// log always drains. It records the probed triples and restricts the
// identifier table to them, so the apply that follows is bounded by the same
// chunk in the other truncation order.
func buildWinnersKeyedChunk(
	ctx context.Context,
	tx db.Transaction,
	bounds winnersChunkBounds,
) (winnersChunkOutcome, error) {
	var outcome winnersChunkOutcome
	rows, err := tx.QueryContext(ctx, selectWinnersKeyedChunkTriplesSQL, bounds.triples+1)
	if err != nil {
		return outcome, fmt.Errorf("list supply chain impact winners chunk triples: %w", err)
	}
	type triple struct{ pkg, repo, digest string }
	var triples []triple
	for rows.Next() {
		var tr triple
		if err := rows.Scan(&tr.pkg, &tr.repo, &tr.digest); err != nil {
			_ = rows.Close()
			return outcome, fmt.Errorf("scan supply chain impact winners chunk triple: %w", err)
		}
		triples = append(triples, tr)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return outcome, fmt.Errorf("list supply chain impact winners chunk triples: %w", err)
	}
	if err := rows.Close(); err != nil {
		return outcome, fmt.Errorf("close supply chain impact winners chunk triples: %w", err)
	}
	if int64(len(triples)) > bounds.triples {
		outcome.truncated = true
		triples = triples[:bounds.triples]
	}

	for _, stmt := range []string{createWinnersKeyedChunkTriplesSQL, createWinnersKeyedActiveFactsEmptySQL} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return outcome, fmt.Errorf("create supply chain impact winners chunk tables: %w", err)
		}
	}
	for _, tr := range triples {
		if err := ctx.Err(); err != nil {
			return outcome, fmt.Errorf("build supply chain impact winners chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertWinnersKeyedChunkTripleSQL, tr.pkg, tr.repo, tr.digest); err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk triple: %w", err)
		}
		fetched, err := insertWinnersKeyedTripleFacts(ctx, tx, tr.pkg, tr.repo, tr.digest)
		if err != nil {
			return outcome, err
		}
		outcome.triples++
		outcome.candidateRows += fetched
		if outcome.candidateRows >= bounds.candidateRows {
			if outcome.triples < int64(len(triples)) {
				outcome.truncated = true
			}
			break
		}
	}
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

// insertWinnersKeyedTripleFacts probes one triple into the chunked
// active_facts and returns the candidate rows the probe fetched.
func insertWinnersKeyedTripleFacts(ctx context.Context, tx db.Transaction, pkg, repo, digest string) (int64, error) {
	rows, err := tx.QueryContext(ctx, insertWinnersKeyedTripleFactsSQL, pkg, repo, digest)
	if err != nil {
		return 0, fmt.Errorf("probe supply chain impact winners triple: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var fetched, kept int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("probe supply chain impact winners triple: %w", err)
		}
		return 0, fmt.Errorf("probe supply chain impact winners triple: statement returned no counts")
	}
	if err := rows.Scan(&fetched, &kept); err != nil {
		return 0, fmt.Errorf("scan supply chain impact winners triple counts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("probe supply chain impact winners triple: %w", err)
	}
	return fetched, nil
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
