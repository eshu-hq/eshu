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

// phase names where the walk ended for the operator. It is reported as
// search.ended_in and on the partial log line. Each issued statement sets
// it, and so does each decision to skip the tail: budget when the floor
// ruled the tail out, tail_skipped_no_trigram when the pattern did. A
// post-skip continuation keeps the skip's name instead of overwriting it,
// so the signal names the decision that shaped the walk, not merely the
// last statement issued.
type phase string

const (
	// phaseProbe is the first key-ordered window.
	phaseProbe phase = "probe"
	// phaseContinuation is the continuation capped at plan.phaseOneCap.
	phaseContinuation phase = "continuation"
	// phaseTail is the trigram tail.
	phaseTail phase = "tail"
	// phaseContinuationAfterTail is the continuation with the budget left
	// after a cancelled tail.
	phaseContinuationAfterTail phase = "continuation_after_tail"
	// phaseBudget means the walk stopped because less than the tail floor was
	// left after the capped continuation, so the tail was skipped.
	phaseBudget phase = "budget"
	// phaseTailSkippedNoTrigram means the pattern yields no extractable
	// trigram, so the walk skipped the tail and spent the remaining budget
	// in key order (#8002).
	phaseTailSkippedNoTrigram phase = "tail_skipped_no_trigram"
)

// walk is the state of one budgeted search over one table shape: the engine.
// The phases are, in order: the probe, continuation steps capped at a
// fraction of the budget, the trigram tail with a bounded timeout, then
// continuation steps with whatever budget is left. The phases cover
// contiguous, non-overlapping key ranges: every statement reads strictly
// after the last visited key, and only a completed statement advances it.
// Nothing here depends on the table; the shape supplies the statements, the
// key binding, the row scanner and the figures.
type walk[R any, K key] struct {
	tx      db.ReadTransaction
	shape   *shape[R, K]
	plan    plan
	now     func() time.Time
	start   time.Time
	pattern string
	want    int // matches needed: offset + limit + 1

	cursor    K // last visited key (request cursor until a step completes)
	requested K // the cursor the call started from
	matches   []R
	visited   int  // rows visited by completed windows, up to the cursor
	finished  bool // want reached, or the key space is exhausted
	exhausted bool // the key space ended before want matches were found

	timeout  time.Duration // statement_timeout currently set on the transaction
	expected time.Duration // predicted duration of the next continuation step
	overrun  time.Duration // largest time a cancelled statement ran past its timeout

	probeRowsVisited int
	steps            int
	stepsCancelled   int // key-ordered windows (probe included) the server cancelled
	stepRowsVisited  int
	tailRan          bool
	tailCancelled    bool
	noTrigram        bool // pattern yields no trigram; tail skipped by design
	endedIn          phase
}

func (w *walk[R, K]) elapsed() time.Duration { return w.now().Sub(w.start) }

func (w *walk[R, K]) remaining() time.Duration { return w.plan.budget - w.elapsed() }

// run executes the phases until the page is proven or the budget is spent.
func (w *walk[R, K]) run(ctx context.Context) error {
	probeStart := w.now()
	w.endedIn = phaseProbe
	if err := w.window(ctx, w.plan.probeRows, w.remaining()); err != nil {
		return err
	}
	if w.finished {
		return nil
	}
	// The first continuation step is predicted from the probe's measured cost,
	// scaled to the step size, because no step has run yet.
	w.expected = w.now().Sub(probeStart) * time.Duration(w.plan.stepRows) / time.Duration(w.plan.probeRows)
	if err := w.continuation(ctx, w.plan.phaseOneCap, phaseContinuation); err != nil || w.finished {
		return err
	}
	if !patternHasTrigram(w.pattern) {
		// No extractable trigram: the tail could only plan a sequential
		// scan far past its cap, so it would always end cancelled. Skip it
		// and give the whole remaining budget to the key-ordered walk,
		// which is exact on its own; the partial states this limit (#8002).
		// The phase name stays on ended_in through the steps below, so the
		// signal names the skip rather than the last issued step.
		w.noTrigram = true
		return w.continuation(ctx, w.remaining(), phaseTailSkippedNoTrigram)
	}
	timeout := min(w.remaining(), w.plan.tailCap)
	if timeout < w.plan.tailFloor {
		w.endedIn = phaseBudget
		return nil
	}
	if err := w.tail(ctx, timeout); err != nil || w.finished {
		return err
	}
	return w.continuation(ctx, w.remaining(), phaseContinuationAfterTail)
}

// continuation issues steps of plan.stepRows rows while the phase allowance
// and the overall budget can pay for the next one. The next step's cost is
// predicted by the previous step's measured duration. A step's statement
// timeout is the room left in the phase, so a window that holds one large
// document completes when the phase can pay for it; a cancelled step ends the
// phase. name is recorded as the walk's last phase once a step is issued.
func (w *walk[R, K]) continuation(ctx context.Context, allowance time.Duration, name phase) error {
	var used time.Duration
	for !w.finished {
		room := min(allowance-used, w.remaining())
		if room <= 0 || w.expected > room {
			return nil
		}
		before := w.now()
		w.endedIn = name
		cancelled, err := w.step(ctx, w.plan.stepRows, room)
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
		w.stepRowsVisited += w.plan.stepRows
	}
	return nil
}

// window runs the probe: one step that must not be cancelled silently. Its
// timeout is the remaining budget, so only a window costing more than the
// whole budget is cancelled.
func (w *walk[R, K]) window(ctx context.Context, size int, timeout time.Duration) error {
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
func (w *walk[R, K]) step(ctx context.Context, size int, timeout time.Duration) (cancelled bool, err error) {
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
		cancelled, err := w.settleFailure(ctx, err, queryStart, timeout)
		if cancelled {
			w.stepsCancelled++
		}
		return cancelled, err
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
func (w *walk[R, K]) tail(ctx context.Context, timeout time.Duration) error {
	w.tailRan = true
	w.endedIn = phaseTail
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
func (w *walk[R, K]) settleFailure(ctx context.Context, err error, queryStart time.Time, timeout time.Duration) (bool, error) {
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

func (w *walk[R, K]) stepStatement(size int) (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return w.shape.stepFirst, []any{execMode, w.pattern, int64(size), asked}
	}
	return w.shape.stepAfter, append([]any{execMode, w.pattern, int64(size), asked}, w.shape.keyArgs(w.cursor)...)
}

func (w *walk[R, K]) tailStatement() (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return w.shape.tailFirst, []any{execMode, w.pattern, asked}
	}
	return w.shape.tailAfter, append([]any{execMode, w.pattern, asked}, w.shape.keyArgs(w.cursor)...)
}

// setTimeout sets the transaction-local statement timeout when it differs from
// the value already in force. The value persists across RELEASE and ROLLBACK
// TO, so the tracked value stays exact.
func (w *walk[R, K]) setTimeout(ctx context.Context, timeout time.Duration) error {
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
func (w *walk[R, K]) exec(ctx context.Context, query string, args ...any) error {
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
func (w *walk[R, K]) collect(ctx context.Context, query string, args []any) ([]R, *K, error) {
	rows, err := w.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	var (
		hits []R
		edge *K
	)
	for rows.Next() {
		kind, row, err := w.shape.scan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("scan %s row: %w", w.shape.name, err)
		}
		if kind == kindEdge {
			last := w.shape.edgeKey(row)
			edge = &last
			continue
		}
		hits = append(hits, row)
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
func (w *walk[R, K]) outcome() outcome {
	switch {
	case !w.finished:
		return outcomePartial
	case w.exhausted:
		return outcomeExhausted
	default:
		return outcomeExact
	}
}

// page returns the page rows from the ordered matches, whether a match
// exists past the page, and the partial view when the budget ended the walk
// first (nil when the walk finished). more is false on a partial page.
func (w *walk[R, K]) page(offset, limit int) (rows []R, more bool, p *partial[K]) {
	from := min(offset, len(w.matches))
	to := min(offset+limit, len(w.matches))
	rows = append([]R(nil), w.matches[from:to]...)
	if !w.finished {
		return rows, false, w.partial()
	}
	return rows, len(w.matches) > offset+limit, nil
}

// progressed reports whether the call completed its answer or advanced the
// cursor. Only a partial that completed no window is false: resuming it
// repeats the identical request.
func (w *walk[R, K]) progressed() bool {
	return w.finished || w.cursor != w.requested
}

func (w *walk[R, K]) partial() *partial[K] {
	reason := querycontract.SearchPartialCandidateBudgetExceeded
	if w.noTrigram {
		reason = querycontract.SearchPartialPatternHasNoTrigram
	}
	if w.overrun.Milliseconds() > querycontract.SearchLargeDocumentOverrunMS {
		reason = querycontract.SearchPartialBudgetExceededOnLargeDocument
	}
	progressed := w.progressed()
	hint := querycontract.SearchPartialHint
	if !progressed {
		hint = querycontract.SearchPartialNoProgressHint
	}
	return &partial[K]{
		reason:             reason,
		rowsScannedInOrder: w.visited,
		rowsMatched:        len(w.matches),
		cursor:             w.cursor,
		budgetMS:           w.plan.budget.Milliseconds(),
		elapsedMS:          w.elapsed().Milliseconds(),
		overrunMS:          w.overrun.Milliseconds(),
		progressed:         progressed,
		hint:               hint,
	}
}
