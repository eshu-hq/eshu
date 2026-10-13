// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// entityWalk is the state of one budgeted entity search. It mirrors walk:
// the probe, continuation steps capped at a fraction of the budget, the
// trigram tail with a bounded timeout, then continuation steps with whatever
// budget is left. The phases cover contiguous, non-overlapping key ranges in
// (repo_id, relative_path, start_line, entity_id) order: every statement
// reads strictly after the last visited entity, and only a completed
// statement advances it.
type entityWalk struct {
	tx      db.ReadTransaction
	plan    plan
	now     func() time.Time
	start   time.Time
	pattern string
	want    int // matches needed: offset + limit + 1

	cursor    querycontract.SearchCursor // last visited key (request cursor until a step completes)
	requested querycontract.SearchCursor // the cursor the call started from
	matches   []querycontract.EntitySearchHit
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
	endedIn          phase
}

func (w *entityWalk) elapsed() time.Duration { return w.now().Sub(w.start) }

func (w *entityWalk) remaining() time.Duration { return w.plan.budget - w.elapsed() }

// run executes the phases until the page is proven or the budget is spent.
func (w *entityWalk) run(ctx context.Context) error {
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

// continuation issues 500-row steps while the phase allowance and the overall
// budget can pay for the next one. The next step's cost is predicted by the
// previous step's measured duration. A step's statement timeout is the room
// left in the phase, so a window that holds one large document completes when
// the phase can pay for it; a cancelled step ends the phase. name is recorded
// as the walk's last phase once a step is issued.
func (w *entityWalk) continuation(ctx context.Context, allowance time.Duration, name phase) error {
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
func (w *entityWalk) window(ctx context.Context, size int, timeout time.Duration) error {
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
func (w *entityWalk) step(ctx context.Context, size int, timeout time.Duration) (cancelled bool, err error) {
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
func (w *entityWalk) tail(ctx context.Context, timeout time.Duration) error {
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
func (w *entityWalk) settleFailure(ctx context.Context, err error, queryStart time.Time, timeout time.Duration) (bool, error) {
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

// saturateCursorStartLine clamps a cursor start line to the int32 range of
// the start_line column. A bare int32 conversion wraps out-of-range values:
// math.MaxInt64 becomes -1, which sorts before every row and resumes the
// walk from the wrong position, returning rows the caller already has.
// Saturation keeps the key-order comparison on the correct side of the key
// space: above MaxInt32 sorts past every storable row, below MinInt32 sorts
// before the first one.
func saturateCursorStartLine(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	// #nosec G115 -- v is clamped to the int32 range above before narrowing.
	return int32(v)
}

func (w *entityWalk) stepStatement(size int) (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return entityStepFirstSQL, []any{execMode, w.pattern, int64(size), asked}
	}
	return entityStepAfterSQL, []any{
		execMode, w.pattern, int64(size), asked,
		w.cursor.RepoID, w.cursor.RelativePath, saturateCursorStartLine(w.cursor.StartLine), w.cursor.EntityID,
	}
}

func (w *entityWalk) tailStatement() (string, []any) {
	asked := int64(w.want - len(w.matches))
	if w.cursor.IsZero() {
		return entityTailFirstSQL, []any{execMode, w.pattern, asked}
	}
	return entityTailAfterSQL, []any{
		execMode, w.pattern, asked,
		w.cursor.RepoID, w.cursor.RelativePath, saturateCursorStartLine(w.cursor.StartLine), w.cursor.EntityID,
	}
}

// setTimeout sets the transaction-local statement timeout when it differs from
// the value already in force. The value persists across RELEASE and ROLLBACK
// TO, so the tracked value stays exact.
func (w *entityWalk) setTimeout(ctx context.Context, timeout time.Duration) error {
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
func (w *entityWalk) exec(ctx context.Context, query string, args ...any) error {
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
// edge key (the last row of the window, absent when the window was short). A
// match keeps its raw metadata beside the row; the query layer decodes it
// through its one metadata decode.
func (w *entityWalk) collect(ctx context.Context, query string, args []any) ([]querycontract.EntitySearchHit, *querycontract.SearchCursor, error) {
	rows, err := w.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	var (
		hits []querycontract.EntitySearchHit
		edge *querycontract.SearchCursor
	)
	for rows.Next() {
		var (
			kind string
			hit  querycontract.EntitySearchHit
		)
		if err := rows.Scan(&kind, &hit.Entity.EntityID, &hit.Entity.RepoID, &hit.Entity.RelativePath,
			&hit.Entity.EntityType, &hit.Entity.EntityName, &hit.Entity.StartLine, &hit.Entity.EndLine,
			&hit.Entity.Language, &hit.Entity.SourceCache, &hit.MetadataRaw); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("scan unscoped entity search row: %w", err)
		}
		if kind == kindEdge {
			edge = &querycontract.SearchCursor{
				RepoID:       hit.Entity.RepoID,
				RelativePath: hit.Entity.RelativePath,
				StartLine:    hit.Entity.StartLine,
				EntityID:     hit.Entity.EntityID,
			}
			continue
		}
		hits = append(hits, hit)
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
func (w *entityWalk) outcome() outcome {
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
func (w *entityWalk) page(offset, limit int) EntityPage {
	from := min(offset, len(w.matches))
	to := min(offset+limit, len(w.matches))
	hits := append([]querycontract.EntitySearchHit(nil), w.matches[from:to]...)
	if !w.finished {
		return EntityPage{Hits: hits, Partial: w.partial()}
	}
	return EntityPage{Hits: hits, More: len(w.matches) > offset+limit}
}

// progressed reports whether the call completed its answer or advanced the
// cursor. Only a partial that completed no window is false: resuming it
// repeats the identical request.
func (w *entityWalk) progressed() bool {
	return w.finished || w.cursor != w.requested
}

func (w *entityWalk) partial() *querycontract.SearchPartial {
	reason := querycontract.SearchPartialCandidateBudgetExceeded
	if w.overrun.Milliseconds() > querycontract.SearchLargeDocumentOverrunMS {
		reason = querycontract.SearchPartialBudgetExceededOnLargeDocument
	}
	progressed := w.progressed()
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
