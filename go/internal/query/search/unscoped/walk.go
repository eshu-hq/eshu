// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// queryCanceledCode is the SQLSTATE the server raises when statement_timeout
// fires.
const queryCanceledCode = "57014"

// outcome names how a search ended.
type outcome string

const (
	// outcomeExact means the page was filled: the walk found offset+limit+1
	// matches, or fewer exist and it proved so.
	outcomeExact outcome = "exact"
	// outcomeExhausted means the walk reached the end of the key space and the
	// answer is every match; the page is complete and may be short.
	outcomeExhausted outcome = "exhausted"
	// outcomePartial means the budget ended the walk first.
	outcomePartial outcome = "partial"
)

// walk is the state of one budgeted search. The phases are, in order: the
// probe, continuation steps capped at a fraction of the budget, the trigram
// tail with a bounded timeout, then continuation steps with whatever budget
// is left. The phases cover contiguous, non-overlapping key ranges: every
// statement reads strictly after the last visited key, and only a completed
// statement advances it.
type walk struct {
	tx      db.ReadTransaction
	plan    plan
	now     func() time.Time
	start   time.Time
	pattern string
	want    int // matches needed: offset + limit + 1

	cursor    querycontract.SearchCursor // last visited key (request cursor until a step completes)
	requested querycontract.SearchCursor // the cursor the call started from
	matches   []querycontract.FileContent
	visited   int  // rows visited by completed windows, up to the cursor
	finished  bool // want reached, or the key space is exhausted
	exhausted bool // the key space ended before want matches were found

	timeout  time.Duration // statement_timeout currently set on the transaction
	expected time.Duration // predicted duration of the next continuation step
	overrun  time.Duration // largest time a cancelled statement ran past its timeout

	probeRowsVisited int
	steps            int
	stepRowsVisited  int
	tailRan          bool
	tailCancelled    bool
}

func (w *walk) elapsed() time.Duration { return w.now().Sub(w.start) }

func (w *walk) remaining() time.Duration { return w.plan.budget - w.elapsed() }

// run executes the phases until the page is proven or the budget is spent.
func (w *walk) run(ctx context.Context) error {
	probeStart := w.now()
	if err := w.window(ctx, probeRows, w.plan.probeTimeout); err != nil {
		return err
	}
	if w.finished {
		return nil
	}
	// The first continuation step is predicted from the probe's measured cost,
	// scaled to the step size, because no step has run yet.
	w.expected = w.now().Sub(probeStart) * stepRows / probeRows
	if err := w.continuation(ctx, w.plan.phaseOneCap); err != nil || w.finished {
		return err
	}
	timeout := min(w.remaining(), w.plan.tailCap)
	if timeout < w.plan.tailFloor {
		return nil
	}
	if err := w.tail(ctx, timeout); err != nil || w.finished {
		return err
	}
	return w.continuation(ctx, w.remaining())
}

// continuation issues 500-row steps while the phase allowance and the overall
// budget can pay for the next one. The next step's cost is predicted by the
// previous step's measured duration; a cancelled step ends the phase.
func (w *walk) continuation(ctx context.Context, allowance time.Duration) error {
	var used time.Duration
	for !w.finished {
		room := min(allowance-used, w.remaining())
		if room <= 0 || w.expected > room {
			return nil
		}
		before := w.now()
		cancelled, err := w.step(ctx, stepRows, min(w.plan.stepTimeout, room))
		if err != nil {
			return err
		}
		spent := w.now().Sub(before)
		used += spent
		if cancelled {
			return nil
		}
		w.expected = spent
		w.steps++
		w.stepRowsVisited += stepRows
	}
	return nil
}

// window runs the probe: one step that must not be cancelled silently.
func (w *walk) window(ctx context.Context, size int, timeout time.Duration) error {
	cancelled, err := w.step(ctx, size, timeout)
	if err != nil {
		return err
	}
	if !cancelled && !w.exhausted {
		w.probeRowsVisited = size
	}
	return nil
}

// step reads one key-ordered window inside a savepoint with its own statement
// timeout. A server cancel rolls back to the savepoint and advances nothing.
func (w *walk) step(ctx context.Context, size int, timeout time.Duration) (cancelled bool, err error) {
	if err := w.setTimeout(ctx, timeout); err != nil {
		return false, err
	}
	if err := w.exec(ctx, savepointSQL); err != nil {
		return false, err
	}
	query, args := w.stepStatement(size)
	queryStart := w.now()
	hits, edge, err := w.collect(ctx, query, args)
	if err != nil {
		return w.settleFailure(ctx, err, queryStart, timeout)
	}
	if err := w.exec(ctx, releaseSQL); err != nil {
		return false, err
	}
	w.matches = append(w.matches, hits...)
	switch {
	case len(w.matches) >= w.want:
		w.finished = true
	case edge == nil:
		w.finished, w.exhausted = true, true
	default:
		w.cursor = *edge
		w.visited += size
	}
	return false, nil
}

// tail runs the trigram statement from the cursor with index scans disabled
// for this statement only, so the planner can choose only the trigram bitmap
// whatever its row estimate says. It reads the whole remaining key range, so
// when it completes the answer is complete.
func (w *walk) tail(ctx context.Context, timeout time.Duration) error {
	w.tailRan = true
	if err := w.setTimeout(ctx, timeout); err != nil {
		return err
	}
	if err := w.exec(ctx, savepointSQL); err != nil {
		return err
	}
	if err := w.exec(ctx, disableIndexScanSQL); err != nil {
		return err
	}
	query, args := w.tailStatement()
	queryStart := w.now()
	hits, _, err := w.collect(ctx, query, args)
	if err != nil {
		cancelled, err := w.settleFailure(ctx, err, queryStart, timeout)
		w.tailCancelled = cancelled
		return err
	}
	if err := w.exec(ctx, releaseSQL); err != nil {
		return err
	}
	if err := w.exec(ctx, restoreIndexScanSQL); err != nil {
		return err
	}
	asked := w.want - len(w.matches)
	w.matches = append(w.matches, hits...)
	w.finished = true
	w.exhausted = len(hits) < asked
	return nil
}

// settleFailure handles a failed bounded statement. A server cancel rolls the
// transaction back to the savepoint, records how far the statement ran past
// its timeout, and reports cancelled; any other error is returned unchanged.
func (w *walk) settleFailure(ctx context.Context, err error, queryStart time.Time, timeout time.Duration) (bool, error) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != queryCanceledCode {
		return false, err
	}
	if over := w.now().Sub(queryStart) - timeout; over > w.overrun {
		w.overrun = over
	}
	if rollbackErr := w.exec(ctx, rollbackToSQL); rollbackErr != nil {
		return false, rollbackErr
	}
	if releaseErr := w.exec(ctx, releaseSQL); releaseErr != nil {
		return false, releaseErr
	}
	return true, nil
}

func (w *walk) stepStatement(size int) (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return stepFirstSQL, []any{execMode, w.pattern, int64(size), asked}
	}
	return stepAfterSQL, []any{execMode, w.pattern, int64(size), asked, w.cursor.RepoID, w.cursor.RelativePath}
}

func (w *walk) tailStatement() (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return tailFirstSQL, []any{execMode, w.pattern, asked}
	}
	return tailAfterSQL, []any{execMode, w.pattern, asked, w.cursor.RepoID, w.cursor.RelativePath}
}

// setTimeout sets the transaction-local statement timeout when it differs from
// the value already in force. The value persists across RELEASE and ROLLBACK
// TO, so the tracked value stays exact.
func (w *walk) setTimeout(ctx context.Context, timeout time.Duration) error {
	timeout = max(timeout, time.Millisecond)
	if timeout == w.timeout {
		return nil
	}
	if err := w.exec(ctx, setTimeoutSQL, execMode, fmt.Sprintf("%dms", timeout.Milliseconds())); err != nil {
		return err
	}
	w.timeout = timeout
	return nil
}

// exec runs a statement whose result rows are not needed.
func (w *walk) exec(ctx context.Context, query string, args ...any) error {
	rows, err := w.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	return err
}

// collect runs a bounded statement and splits its rows into matches and the
// edge key (the last row of the window, absent when the window was short).
func (w *walk) collect(ctx context.Context, query string, args []any) ([]querycontract.FileContent, *querycontract.SearchCursor, error) {
	rows, err := w.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	var (
		hits []querycontract.FileContent
		edge *querycontract.SearchCursor
	)
	for rows.Next() {
		var (
			kind string
			file querycontract.FileContent
		)
		if err := rows.Scan(&kind, &file.RepoID, &file.RelativePath, &file.CommitSHA, &file.Content,
			&file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("scan unscoped search row: %w", err)
		}
		if kind == kindEdge {
			edge = &querycontract.SearchCursor{RepoID: file.RepoID, RelativePath: file.RelativePath}
			continue
		}
		hits = append(hits, file)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, nil, err
	}
	return hits, edge, nil
}

// outcome classifies how the walk ended.
func (w *walk) outcome() outcome {
	switch {
	case !w.finished:
		return outcomePartial
	case w.exhausted:
		return outcomeExhausted
	default:
		return outcomeExact
	}
}

// page assembles the response page from the ordered matches.
func (w *walk) page(offset, limit int) querycontract.FileSearchPage {
	from := min(offset, len(w.matches))
	to := min(offset+limit, len(w.matches))
	files := append([]querycontract.FileContent(nil), w.matches[from:to]...)
	if !w.finished {
		return querycontract.FileSearchPage{Files: files, Partial: w.partial()}
	}
	return querycontract.FileSearchPage{Files: files, More: len(w.matches) > offset+limit}
}

func (w *walk) partial() *querycontract.SearchPartial {
	reason := querycontract.SearchPartialCandidateBudgetExceeded
	if w.overrun.Milliseconds() > querycontract.SearchLargeDocumentOverrunMS {
		reason = querycontract.SearchPartialBudgetExceededOnLargeDocument
	}
	progressed := w.cursor != w.requested
	hint := querycontract.SearchPartialHint
	if !progressed {
		hint = querycontract.SearchPartialNoProgressHint
	}
	return &querycontract.SearchPartial{
		Reason:             reason,
		RowsScannedInOrder: w.visited,
		RowsMatched:        len(w.matches),
		Cursor:             w.cursor,
		BudgetMS:           w.plan.budget.Milliseconds(),
		ElapsedMS:          w.elapsed().Milliseconds(),
		OverrunMS:          w.overrun.Milliseconds(),
		Progressed:         progressed,
		Hint:               hint,
	}
}
