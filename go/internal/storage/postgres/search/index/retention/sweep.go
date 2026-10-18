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

// SupersededSweepRowCap is the #8231 superseded sweep's row budget when the
// caller passes none, the same as the retention pre-delete's default row cap:
// about 100 full batches at the measured 20,000-row batch cost. The
// retention runner passes the policy's search index row cap, so one cap
// bounds the pre-delete, the orphan sweep, and this sweep. The first batch
// of a sweep always runs; a larger backlog drains over later sweeps.
const SupersededSweepRowCap int64 = 2_000_000

// SupersededSweep is one #8231 sweep: the listed superseded generations and
// the rows it removed from each search index table.
type SupersededSweep struct {
	// Candidates is the superseded generations the sweep listed.
	Candidates int
	// RowsSwept is the rows the sweep deleted, by table
	// (eshu_search_index_terms, _documents, _stats).
	RowsSwept map[string]int64
	// Batches is the batch transactions the sweep committed; a busy, yielded
	// or ineligible attempt rolls back and is not counted.
	Batches int
	// Unswept is the listed generations this pass did not fully sweep: the
	// ones it did not drain (busy lock, lock yield, stalled page, spent
	// budget, or a batch that found the generation ineligible). A listing
	// that hits SupersededSweepCandidateLimit leaves further generations for
	// later sweeps without counting them here. Later sweeps retry all of
	// them.
	Unswept int
	// ListDuration and SweepDuration time the two halves.
	ListDuration  time.Duration
	SweepDuration time.Duration
}

// supersededTables drains a superseded generation's terms then documents
// with the fail-closed #8231 batches. The stats row follows in sweepStats
// once both tables drain: it is one row, not a keyset walk.
var supersededTables = []table{
	{
		label: TermsTable,
		query: sweepSupersededTermsBatchQuery,
		args: func(g Generation, c cursor) []any {
			return []any{g.ScopeID, g.GenerationID, c.first, c.second, BatchRows}
		},
		add: func(r *Report, n int64) { r.TermsDeleted += n },
	},
	{
		label: DocumentsTable,
		query: sweepSupersededDocumentsBatchQuery,
		args: func(g Generation, c cursor) []any {
			return []any{g.ScopeID, g.GenerationID, c.first, BatchRows}
		},
		add: func(r *Report, n int64) { r.DocumentsDeleted += n },
	},
}

// SweepSuperseded removes the search index rows (terms, documents, stats) of
// superseded generations whose facts the history hold keeps, in committed
// batches, never of the active generation (#8231). It lists the candidates
// oldest first and drains each with drainSuperseded under rowCap
// (SupersededSweepRowCap when not positive) and the deadline (zero for
// none). It runs outside any retention transaction and holds no retention
// row lock: each batch is its own transaction under the per-generation
// advisory lock, so it never overlaps a pre-delete batch or a prune of the
// same generation id. logger, when non-nil, receives one line per committed
// batch plus one completion line per generation.
//
// Safety nets, shared with #7427 rather than reimplemented: every batch
// re-checks in its own snapshot that the generation row still exists, is
// terminal superseded, is not the scope's live active generation, and has
// no live work — the same fail-closed outcomes as NewGenerationFreshnessCheck
// (missing row or NULL active pointer deletes nothing). A generation pruned
// between the listing and a batch turns its leftover rows into orphans,
// which the unchanged orphan census and sweep remove on their own cadence.
// Facts are never touched, so a swept generation's index rebuilds on demand
// by re-running the search-document projection from the held facts; the
// sweep itself never rebuilds eagerly.
func SweepSuperseded(ctx context.Context, database Database, deadline time.Time, rowCap int64, logger *slog.Logger) (SupersededSweep, error) {
	started := time.Now()
	candidates, err := listSupersededCandidates(ctx, database)
	sweep := SupersededSweep{
		RowsSwept:    map[string]int64{TermsTable: 0, DocumentsTable: 0, StatsTable: 0},
		ListDuration: time.Since(started),
	}
	if err != nil {
		return sweep, err
	}
	sweep.Candidates = len(candidates)
	if len(candidates) == 0 {
		return sweep, nil
	}
	started = time.Now()
	if rowCap <= 0 {
		rowCap = SupersededSweepRowCap
	}
	pass := NewSupersededPass(rowCap, deadline, logger)
	runErr := pass.Run(ctx, database, candidates)
	sweep.SweepDuration = time.Since(started)
	terms, documents, batches, _, _ := pass.Totals()
	sweep.RowsSwept[TermsTable] = terms
	sweep.RowsSwept[DocumentsTable] = documents
	sweep.RowsSwept[StatsTable] = pass.StatsDeleted()
	sweep.Batches = batches
	for _, candidate := range candidates {
		if !pass.Drained(candidate.GenerationID) {
			sweep.Unswept++
		}
	}
	if runErr != nil {
		return sweep, fmt.Errorf("search index superseded sweep: %w", runErr)
	}
	return sweep, nil
}

// listSupersededCandidates reads the sweep's candidates in one snapshot.
func listSupersededCandidates(ctx context.Context, q db.Queryer) ([]Generation, error) {
	rows, err := q.QueryContext(ctx, supersededSweepCandidatesQuery, SupersededSweepCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("search index superseded sweep: list candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var candidates []Generation
	for rows.Next() {
		var candidate Generation
		if err := rows.Scan(&candidate.ScopeID, &candidate.GenerationID); err != nil {
			return nil, fmt.Errorf("search index superseded sweep: scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search index superseded sweep: list candidates: %w", err)
	}
	return candidates, nil
}

// drainSuperseded drains one superseded generation's terms and documents
// with the fail-closed batches, then removes its stats row. A generation
// whose tables drain but whose stats batch finds it ineligible keeps the
// committed deletes (the #8276 accepted residual: superseded is terminal and
// no reader joins a superseded generation) and ends OutcomeIneligible so a
// later pass retries the stats row.
func drainSuperseded(ctx context.Context, database db.Beginner, generation Generation, budget *Budget) (Report, error) {
	report, err := drain(ctx, database, generation, budget, supersededTables)
	if err != nil || !report.Drained() {
		return report, err
	}
	outcome, err := sweepStats(ctx, database, generation, budget, &report)
	if err != nil {
		return report, err
	}
	if outcome != "" {
		report.Outcome = outcome
	}
	return report, nil
}

// sweepStats deletes one drained generation's stats row in its own batch
// transaction: advisory lock, lock_timeout, and the fail-closed eligibility
// re-check. A non-empty outcome means the batch applied nothing.
func sweepStats(ctx context.Context, database db.Beginner, generation Generation, budget *Budget, report *Report) (Outcome, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("search index superseded sweep: %w", err)
	}
	if budget.Exhausted(time.Now()) {
		return OutcomeBudget, nil
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("search index superseded sweep: begin: %w", err)
	}
	// Every attempt spends the first-batch allowance, committed or not, like
	// drainTable; only committed batches charge rows and the batch count.
	budget.batches++
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, setLockTimeoutQuery, lockTimeoutSetting); err != nil {
		return "", fmt.Errorf("search index superseded sweep: set lock_timeout: %w", err)
	}
	locked, err := queryBool(ctx, tx, tryLockQuery, generation.GenerationID)
	if err != nil {
		return "", fmt.Errorf("search index superseded sweep: try lock: %w", err)
	}
	if !locked {
		return OutcomeLockBusy, nil
	}
	rows, err := tx.QueryContext(ctx, deleteSupersededStatsBatchQuery, generation.ScopeID, generation.GenerationID)
	if err != nil {
		if isLockYield(err) {
			return OutcomeLockYield, nil
		}
		return "", fmt.Errorf("search index superseded sweep: stats batch: %w", err)
	}
	eligible, deleted, err := scanStatsBatch(rows)
	if err != nil {
		if isLockYield(err) {
			return OutcomeLockYield, nil
		}
		return "", fmt.Errorf("search index superseded sweep: stats batch: %w", err)
	}
	if !eligible {
		return OutcomeIneligible, nil
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("search index superseded sweep: commit: %w", err)
	}
	committed = true
	budget.committed++
	report.Batches++
	report.StatsDeleted += deleted
	budget.rows += deleted
	if budget.onBatch != nil {
		budget.onBatch(ctx, report.Generation, StatsTable, deleted)
	}
	return "", nil
}

// scanStatsBatch reads the stats batch statement's single result row:
// whether the generation was still eligible and the rows deleted.
func scanStatsBatch(rows db.Rows) (bool, int64, error) {
	defer func() { _ = rows.Close() }()
	var eligible bool
	var deleted int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, 0, err
		}
		return false, 0, fmt.Errorf("stats batch returned no row")
	}
	if err := rows.Scan(&eligible, &deleted); err != nil {
		return false, 0, err
	}
	return eligible, deleted, rows.Err()
}
