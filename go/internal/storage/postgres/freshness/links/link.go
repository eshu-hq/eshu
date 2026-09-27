// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	// DefaultSlots is the default number of concurrent full links (#7127
	// ruling 8.3). Raise it only with the 1/2/4-link cap evidence.
	DefaultSlots = 2
	// DefaultStatementTimeout bounds one link statement (#7127 ruling 8.3).
	DefaultStatementTimeout = 120 * time.Second
)

// LinkWriter links one activation per transaction. It is safe for
// concurrent use: the per-scope cursor row is the writer fence, and full
// links share Slots database-side advisory slots.
type LinkWriter struct {
	database db.ExecQueryer

	// Slots caps concurrent full links across every process sharing the
	// database. Values below 1 use DefaultSlots.
	Slots int
	// StatementTimeout bounds each link statement. Values at or below zero use
	// DefaultStatementTimeout.
	StatementTimeout time.Duration
	// Now supplies timestamps; nil uses time.Now.
	Now func() time.Time
}

// NewLinkWriter returns a LinkWriter over database, which must implement
// db.Beginner.
func NewLinkWriter(database db.ExecQueryer) *LinkWriter {
	return &LinkWriter{database: database}
}

func (w *LinkWriter) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w *LinkWriter) slots() int {
	if w.Slots < 1 {
		return DefaultSlots
	}
	return w.Slots
}

func (w *LinkWriter) statementTimeout() time.Duration {
	if w.StatementTimeout <= 0 {
		return DefaultStatementTimeout
	}
	return w.StatementTimeout
}

// cursorState is the locked cursor row of one scope.
type cursorState struct {
	stateGenerationID string
	activationSeq     int64
}

// activation is one journal row.
type activation struct {
	seq          int64
	generationID string
	priorID      string
	priorUnknown bool
}

// LinkNext links the scope's oldest activation above its cursor in one
// transaction and advances the cursor past it. It returns Idle when there is
// none. A lock miss, a busy slot or a statement timeout rolls back and
// returns a *RetryError; the cursor does not move, so the activation is
// retried. Any other error rolls back and is returned as is.
func (w *LinkWriter) LinkNext(ctx context.Context, scopeID string) (LinkResult, error) {
	if w == nil || w.database == nil {
		return LinkResult{}, errors.New("changed-since link database is required")
	}
	if scopeID == "" {
		return LinkResult{}, errors.New("changed-since link scope_id is required")
	}
	beginner, ok := w.database.(db.Beginner)
	if !ok {
		return LinkResult{}, errors.New("changed-since link database must support Begin")
	}
	start := time.Now()
	if _, err := w.database.ExecContext(ctx, ensureCursorQuery, scopeID, DigestVersion, w.now()); err != nil {
		return LinkResult{}, fmt.Errorf("changed-since link: ensure cursor: %w", err)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return LinkResult{}, fmt.Errorf("changed-since link: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := w.linkInTx(ctx, tx, scopeID)
	if err != nil {
		return LinkResult{}, classifyTimeout(err)
	}
	if err := tx.Commit(); err != nil {
		return LinkResult{}, classifyTimeout(fmt.Errorf("changed-since link: commit: %w", err))
	}
	committed = true
	result.Duration = time.Since(start)
	return result, nil
}

func (w *LinkWriter) linkInTx(ctx context.Context, tx db.Transaction, scopeID string) (LinkResult, error) {
	cursor, locked, err := lockCursor(ctx, tx, scopeID)
	if err != nil {
		return LinkResult{}, err
	}
	if !locked {
		return LinkResult{}, &RetryError{Reason: RetryCursorLocked}
	}
	act, found, err := nextActivation(ctx, tx, scopeID, cursor.activationSeq)
	if err != nil {
		return LinkResult{}, err
	}
	if !found {
		return LinkResult{Idle: true, ScopeID: scopeID}, nil
	}
	result := LinkResult{
		ScopeID:       scopeID,
		GenerationID:  act.generationID,
		ActivationSeq: act.seq,
		Kind:          LinkKindNone,
	}

	isDelta, exists, err := generationIsDelta(ctx, tx, scopeID, act.generationID)
	if err != nil {
		return LinkResult{}, err
	}
	if !exists {
		result.Break = BreakPrunedBeforeLink
		return result, w.advance(ctx, tx, scopeID, cursor.stateGenerationID, act.seq)
	}
	if err := lockGeneration(ctx, tx, scopeID, act.generationID); err != nil {
		return LinkResult{}, err
	}
	if isDelta {
		result.Break = deltaBreakReason(cursor.stateGenerationID, act)
		return result, w.advance(ctx, tx, scopeID, cursor.stateGenerationID, act.seq)
	}
	if cursor.stateGenerationID == act.generationID {
		return result, w.advance(ctx, tx, scopeID, cursor.stateGenerationID, act.seq)
	}
	if err := w.takeSlot(ctx, tx); err != nil {
		return LinkResult{}, err
	}
	if err := w.setLocals(ctx, tx); err != nil {
		return LinkResult{}, err
	}
	if cursor.stateGenerationID == "" {
		err = w.root(ctx, tx, scopeID, act.generationID, &result)
	} else {
		err = w.incremental(ctx, tx, scopeID, act.generationID, cursor.stateGenerationID, &result)
	}
	if err != nil {
		return LinkResult{}, err
	}
	return result, w.advance(ctx, tx, scopeID, act.generationID, act.seq)
}

// deltaBreakReason classifies a delta activation. The overlay link is not
// shipped (#7127 ruling 8.6), so every delta activation is a break; the reason
// says whether an overlay would have been possible.
func deltaBreakReason(stateGenerationID string, act activation) BreakReason {
	switch {
	case stateGenerationID == "":
		return BreakDeltaWithoutRoot
	case act.priorUnknown || act.priorID != stateGenerationID:
		return BreakPriorMismatch
	default:
		return BreakOverlayUnproven
	}
}

func lockCursor(ctx context.Context, tx db.Transaction, scopeID string) (cursorState, bool, error) {
	rows, err := tx.QueryContext(ctx, lockCursorQuery, scopeID)
	if err != nil {
		return cursorState{}, false, fmt.Errorf("changed-since link: lock cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return cursorState{}, false, fmt.Errorf("changed-since link: lock cursor: %w", err)
		}
		return cursorState{}, false, nil
	}
	var state cursorState
	var digestVersion int16
	if err := rows.Scan(&state.stateGenerationID, &state.activationSeq, &digestVersion); err != nil {
		return cursorState{}, false, fmt.Errorf("changed-since link: scan cursor: %w", err)
	}
	if digestVersion != DigestVersion {
		// A state built under another digest cannot be compared with this
		// one; the next full generation re-roots the scope.
		state.stateGenerationID = ""
	}
	return state, true, rows.Err()
}

func nextActivation(ctx context.Context, tx db.Transaction, scopeID string, afterSeq int64) (activation, bool, error) {
	rows, err := tx.QueryContext(ctx, nextActivationQuery, scopeID, afterSeq)
	if err != nil {
		return activation{}, false, fmt.Errorf("changed-since link: next activation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return activation{}, false, rows.Err()
	}
	var act activation
	if err := rows.Scan(&act.seq, &act.generationID, &act.priorID, &act.priorUnknown); err != nil {
		return activation{}, false, fmt.Errorf("changed-since link: scan activation: %w", err)
	}
	return act, true, rows.Err()
}

func generationIsDelta(ctx context.Context, tx db.Transaction, scopeID, generationID string) (bool, bool, error) {
	rows, err := tx.QueryContext(ctx, generationExistsQuery, generationID, scopeID)
	if err != nil {
		return false, false, fmt.Errorf("changed-since link: read generation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, false, rows.Err()
	}
	var isDelta bool
	if err := rows.Scan(&isDelta); err != nil {
		return false, false, fmt.Errorf("changed-since link: scan generation: %w", err)
	}
	return isDelta, true, rows.Err()
}

func lockGeneration(ctx context.Context, tx db.Transaction, scopeID, generationID string) error {
	rows, err := tx.QueryContext(ctx, lockGenerationQuery, generationID, scopeID)
	if err != nil {
		return fmt.Errorf("changed-since link: lock generation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("changed-since link: lock generation: %w", err)
		}
		return &RetryError{Reason: RetryGenerationLocked}
	}
	return rows.Err()
}

// takeSlot tries each full-link slot once. None free is slot_busy.
func (w *LinkWriter) takeSlot(ctx context.Context, tx db.Transaction) error {
	for slot := 1; slot <= w.slots(); slot++ {
		rows, err := tx.QueryContext(ctx, trySlotQuery, SlotLockClass, slot)
		if err != nil {
			return fmt.Errorf("changed-since link: try slot: %w", err)
		}
		var got bool
		if rows.Next() {
			if err := rows.Scan(&got); err != nil {
				_ = rows.Close()
				return fmt.Errorf("changed-since link: scan slot: %w", err)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return fmt.Errorf("changed-since link: try slot: %w", err)
		}
		if got {
			return nil
		}
	}
	return &RetryError{Reason: RetrySlotBusy}
}

func (w *LinkWriter) setLocals(ctx context.Context, tx db.Transaction) error {
	timeoutMS := w.statementTimeout().Milliseconds()
	for _, statement := range []string{
		setWorkMemStatement,
		setPlanCacheModeStatement,
		setStatementTimeoutStatementPrefix + strconv.FormatInt(timeoutMS, 10),
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("changed-since link: %s: %w", statement, err)
		}
	}
	return nil
}

func (w *LinkWriter) root(ctx context.Context, tx db.Transaction, scopeID, generationID string, result *LinkResult) error {
	if _, err := tx.ExecContext(ctx, clearStateQuery, scopeID); err != nil {
		return fmt.Errorf("changed-since link: clear state: %w", err)
	}
	var files, entities, facts int64
	if err := queryOne(ctx, tx, RootLinkSQL,
		[]any{scopeID, generationID, DigestVersion, w.now()},
		&result.DeltaRows, &files, &entities, &facts); err != nil {
		return fmt.Errorf("changed-since link: root: %w", err)
	}
	result.Kind = LinkKindRoot
	result.Keys = files + entities + facts
	return nil
}

func (w *LinkWriter) incremental(
	ctx context.Context, tx db.Transaction, scopeID, generationID, priorID string, result *LinkResult,
) error {
	var files, entities, facts, deleted, upserted, buckets int64
	if err := queryOne(ctx, tx, IncrementalLinkSQL,
		[]any{scopeID, generationID, priorID, DigestVersion, w.now()},
		&result.DeltaRows, &files, &entities, &facts, &deleted, &upserted, &buckets); err != nil {
		return fmt.Errorf("changed-since link: incremental: %w", err)
	}
	result.Kind = LinkKindIncremental
	result.PriorGenerationID = priorID
	result.Keys = files + entities + facts
	return nil
}

func (w *LinkWriter) advance(ctx context.Context, tx db.Transaction, scopeID, stateGenerationID string, seq int64) error {
	res, err := tx.ExecContext(ctx, advanceCursorQuery, scopeID, stateGenerationID, seq, DigestVersion, w.now())
	if err != nil {
		return fmt.Errorf("changed-since link: advance cursor: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n != 1 {
		return fmt.Errorf("changed-since link: advance cursor: %d rows updated, want 1", n)
	}
	return nil
}

// queryOne runs a statement that returns exactly one row.
func queryOne(ctx context.Context, q db.Queryer, query string, args []any, dest ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

// classifyTimeout turns SQLSTATE 57014 (statement_timeout) into a retry.
func classifyTimeout(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return &RetryError{Reason: RetryStatementTimeout, Err: err}
	}
	return err
}
