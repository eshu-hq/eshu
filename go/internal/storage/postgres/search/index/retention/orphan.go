// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// OrphanSweepRowCap is the orphan sweep's row budget when the caller passes
// none, the same as the retention pre-delete's default row cap: about 100
// full batches at the measured 20,000-row batch cost. The retention runner
// passes the policy's search index row cap, so one cap bounds both the
// pre-delete and the sweep (#8281 review). The first batch of a sweep always
// runs; a larger backlog drains over later sweeps.
const OrphanSweepRowCap int64 = 2_000_000

// Database is the connection the orphan sweep needs: read-only census
// statements and batch transactions.
type Database interface {
	db.Beginner
	db.Queryer
}

// OrphanSweep is one census and the sweep of the orphans it listed.
type OrphanSweep struct {
	Census Census
	// RowsSwept is the rows the sweep deleted, by table.
	RowsSwept map[string]int64
	// Batches is the batch transactions the sweep committed; a busy, yielded
	// or ineligible attempt rolls back and is not counted.
	Batches int
	// Unswept is the orphan generations this pass did not sweep: the listed
	// ones it did not drain (busy lock, lock yield, stalled page, spent
	// budget, or the generation exists again) plus Census.Unlisted, the ones
	// the census counted past OrphanListLimit. Later sweeps retry them.
	Unswept int
	// CensusDuration and SweepDuration time the two halves.
	CensusDuration time.Duration
	SweepDuration  time.Duration
}

// SweepOrphans takes a census of the search index tables and sweeps the
// orphan generations it listed with SweepOrphan, in scope then generation
// order, under rowCap (OrphanSweepRowCap when not positive) and the deadline
// (zero for none). It runs outside any
// retention transaction and holds no retention row lock: each batch is its
// own transaction under the per-generation advisory lock, so it never
// overlaps a pre-delete batch or a prune of the same generation id, and a
// second sweeper skips a generation the first holds. A row a late writer
// inserts behind the cursor is removed by the re-sweep; one inserted after
// the sweep is found by the next census. logger, when non-nil, receives the
// per-generation lines.
func SweepOrphans(ctx context.Context, database Database, deadline time.Time, rowCap int64, logger *slog.Logger) (OrphanSweep, error) {
	started := time.Now()
	census, err := TakeCensus(ctx, database)
	sweep := OrphanSweep{
		Census:         census,
		RowsSwept:      map[string]int64{TermsTable: 0, DocumentsTable: 0},
		CensusDuration: time.Since(started),
		Unswept:        int(census.Unlisted),
	}
	if err != nil {
		return sweep, err
	}
	if len(census.Orphans) == 0 {
		return sweep, nil
	}
	started = time.Now()
	if rowCap <= 0 {
		rowCap = OrphanSweepRowCap
	}
	pass := NewOrphanPass(rowCap, deadline, logger)
	runErr := pass.Run(ctx, database, census.Orphans)
	sweep.SweepDuration = time.Since(started)
	terms, documents, batches, _, _ := pass.Totals()
	sweep.RowsSwept[TermsTable] = terms
	sweep.RowsSwept[DocumentsTable] = documents
	sweep.Batches = batches
	for _, orphan := range census.Orphans {
		if !pass.Drained(orphan.GenerationID) {
			sweep.Unswept++
		}
	}
	if runErr != nil {
		return sweep, fmt.Errorf("search index orphan sweep: %w", runErr)
	}
	return sweep, nil
}
