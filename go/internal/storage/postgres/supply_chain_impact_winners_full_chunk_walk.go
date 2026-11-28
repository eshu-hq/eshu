// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Walk execution of the ruled #9805 chunked full rebuild: the temp-table
// lifecycle, the identifier paging, one chunk's probe-build-rank-apply, and
// the walk loop. The statements the walk runs live in
// supply_chain_impact_winners_full_chunked.go; this file only executes them.

// dropWinnersFullChunkTempSQL drops the full walk's working temp tables before
// it creates them: the keyed-cap fallback calls the full rebuild in the same
// transaction that already built the keyed identifier table
// (winnersKeyedBuildIdentsStmts), whose seven-column shape the full walk must
// not inherit, so a second same-name CREATE would fail with 42P07. The drop
// is the single lifecycle for every temp the walk owns; the consumed,
// scope-idents and batch tables it never touches keep their keyed lifecycle.
const dropWinnersFullChunkTempSQL = `DROP TABLE IF EXISTS winners_full_idents, winners_keyed_idents, winners_keyed_narrow, winners_keyed_active_facts`

// winnersFullChunkPage is one bounded identifier page of the full rebuild.
type winnersFullChunkPage struct {
	idents []winnersKeyedChunkIdent
}

// scanWinnersFullChunkPage reads one identifier page: the five-tuple per row
// in page order.
func scanWinnersFullChunkPage(rows db.Rows) (winnersFullChunkPage, error) {
	var page winnersFullChunkPage
	for rows.Next() {
		var id winnersKeyedChunkIdent
		if err := rows.Scan(&id.pkg, &id.repo, &id.digest, &id.c0, &id.advisory); err != nil {
			_ = rows.Close()
			return page, fmt.Errorf("scan supply chain impact winners full chunk identifier: %w", err)
		}
		page.idents = append(page.idents, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return page, fmt.Errorf("read supply chain impact winners full chunk identifiers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return page, fmt.Errorf("close supply chain impact winners full chunk identifiers: %w", err)
	}
	return page, nil
}

// applyWinnersFullChunk runs one identifier page: it stages the page, probes
// it, builds the chunk active_facts, ranks the chunk and writes only its new
// or changed winners, and reports the chunk counts. stamp is the resweep's
// stamp ceiling S, written as materialized_at on every row the chunk touches.
func applyWinnersFullChunk(
	ctx context.Context,
	tx db.Transaction,
	stamp time.Time,
	page winnersFullChunkPage,
	supportsReady bool,
) (inserted, updated, deleted, unchanged int64, err error) {
	if _, err := tx.ExecContext(ctx, clearWinnersKeyedIdentsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("clear supply chain impact winners full chunk identifiers: %w", err)
	}
	query, args, err := winnersKeyedIdentBatch(insertWinnersKeyedIdentsPrefix, page.idents)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("stage supply chain impact winners full chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("stage supply chain impact winners full chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, clearWinnersKeyedNarrowSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("clear supply chain impact winners full chunk probe: %w", err)
	}
	if _, err := tx.ExecContext(ctx, winnersFullChunkNarrowProbe(supportsReady)); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("probe supply chain impact winners full chunk copies: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedNarrowSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("analyze supply chain impact winners full chunk probe: %w", err)
	}
	if _, err := tx.ExecContext(ctx, clearWinnersKeyedActiveFactsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("clear supply chain impact winners full chunk active facts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, insertWinnersFullChunkActiveFactsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("build supply chain impact winners full chunk active facts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedActiveFactsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("analyze supply chain impact winners full chunk active facts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, addWinnersKeyedOperatorFactsNarrowFromSupportsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("add supply chain impact winners full chunk operator facts: %w", err)
	}
	rows, err := tx.QueryContext(ctx, applyWinnersFullChunkSQL, stamp)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("apply supply chain impact winners full chunk: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("apply supply chain impact winners full chunk: %w", err)
		}
		return 0, 0, 0, 0, fmt.Errorf("apply supply chain impact winners full chunk: statement returned no counts")
	}
	if err := rows.Scan(&inserted, &updated, &deleted, &unchanged); err != nil {
		_ = rows.Close()
		return 0, 0, 0, 0, fmt.Errorf("scan supply chain impact winners full chunk counts: %w", err)
	}
	if rows.Next() {
		return 0, 0, 0, 0, fmt.Errorf("apply supply chain impact winners full chunk: statement returned more than one row")
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("apply supply chain impact winners full chunk: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("close supply chain impact winners full chunk counts: %w", err)
	}
	return inserted, updated, deleted, unchanged, nil
}

// runWinnersFullChunks walks the enumerated identifier set in bounded pages
// and applies each page, accumulating the chunk counts. Pages are dense row
// ranges, so consecutive ranges tile the set: the walk ends on a short page.
func runWinnersFullChunks(
	ctx context.Context,
	tx db.Transaction,
	stamp time.Time,
	bounds winnersChunkBounds,
	supportsReady bool,
) (inserted, updated, deleted, unchanged int64, err error) {
	// The keyed-cap fallback reuses this transaction's keyed identifier
	// table (seven columns with age); the walk owns the five-column shape,
	// so it drops what it owns before creating it.
	if _, err := tx.ExecContext(ctx, dropWinnersFullChunkTempSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("drop supply chain impact winners full chunk temps: %w", err)
	}
	if _, err := tx.ExecContext(ctx, buildWinnersFullIdentsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("enumerate supply chain impact winners full identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersFullIdentsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("analyze supply chain impact winners full identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedIdentsSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("create supply chain impact winners full chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedNarrowSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("create supply chain impact winners full chunk probe: %w", err)
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedActiveFactsNarrowSQL); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("create supply chain impact winners full chunk active facts: %w", err)
	}
	size := bounds.identifiers
	var lower int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("walk supply chain impact winners full chunks: %w", err)
		}
		rows, err := tx.QueryContext(ctx, selectWinnersFullChunkPageSQL, lower, lower+size)
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("page supply chain impact winners full identifiers: %w", err)
		}
		page, err := scanWinnersFullChunkPage(rows)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		if len(page.idents) == 0 {
			return inserted, updated, deleted, unchanged, nil
		}
		var ins, upd, del, unc int64
		if ins, upd, del, unc, err = applyWinnersFullChunk(ctx, tx, stamp, page, supportsReady); err != nil {
			return 0, 0, 0, 0, err
		}
		inserted, updated, deleted, unchanged = inserted+ins, updated+upd, deleted+del, unchanged+unc
		lower += size
		if int64(len(page.idents)) < size {
			return inserted, updated, deleted, unchanged, nil
		}
	}
}
