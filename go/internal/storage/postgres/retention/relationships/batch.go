// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Statement stages, as the fault hook and the tests name them.
const (
	stageSetLockTimeout    = "set_lock_timeout"
	stageTryLock           = "try_lock"
	stageClassify          = "classify"
	stageDeleteEvidence    = "delete_evidence"
	stageDeleteCandidates  = "delete_candidates"
	stageDeleteGenerations = "delete_generations"
	stageSettle            = "settle"
)

// batchOutcome is what one batch transaction did.
type batchOutcome struct {
	// committed reports that the batch committed. Only then do rows and swept
	// count: a batch that deleted nothing and finished no generation ends
	// without a commit, and one that fails rolls back whole.
	committed bool
	// swept is the claimed generations the batch left with nothing behind: no
	// evidence, candidate or generation row, counting the batch's own deletes. It
	// includes a claimed generation whose rows another session or replica had
	// already removed after the census listed it, which the batch found empty.
	// rows is the batch's deletes' affected row counts by table, which can be
	// non-zero while swept is zero (a purge held the rest of a generation).
	swept int
	rows  map[string]int64
	// busy, ineligible and timedOut count generations left for a later pass.
	busy       int
	ineligible int
	timedOut   int
	// continued is the claimed generations that still had rows after a batch
	// whose evidence or candidate delete stopped at its row limit. They are not
	// busy: the batch only ran out of row budget, so the pass runs them again.
	continued []string
	duration  time.Duration
}

// total is the rows the batch's deletes affected, across the three tables.
func (o batchOutcome) total() int64 {
	var n int64
	for _, rows := range o.rows {
		n += rows
	}
	return n
}

// runBatch deletes the relationship rows of up to BatchGenerations orphan
// generations in one transaction. It never waits for a row lock: every row lock
// it takes, in classification and in each of the three deletes, is FOR UPDATE SKIP
// LOCKED. That is what keeps it out of a lock cycle with the #10820 purge (the
// purge deletes evidence and candidates first and the generation row last; this
// batch locks the generation row first):
//
//  1. lock_timeout 500 ms, transaction local and never zero. It bounds the one
//     wait the batch cannot skip, a relation-level lock such as DDL; it is a
//     backstop, not a cycle breaker;
//  2. a per-generation advisory try lock, so two sweepers never take the same
//     generation, and an id another sweeper holds is skipped;
//  3. classification, which re-checks Rule A in this snapshot and locks the
//     generation rows FOR UPDATE SKIP LOCKED, so an activation or refinalize in
//     flight is skipped and a committed one is re-checked under the lock;
//  4. evidence, then candidates: each selects up to BatchRows rows of the claimed
//     generations FOR UPDATE SKIP LOCKED, re-asserting every Rule A predicate,
//     and deletes exactly those rows, so a row another session holds stays;
//  5. the generation row last, picked FOR UPDATE SKIP LOCKED like the rows before
//     it and only for a generation with no evidence and no candidate row left.
//     Classification locked the generation rows that existed then; a generation
//     with no generation row was claimed without a lock, so a row a writer has
//     created since is skipped here, not waited for, and the settle read counts
//     the generation lock_busy;
//  6. a settle read, which names the claimed generations that still have a row.
//     If neither picking delete stopped at its row limit, another session holds
//     what is left: the batch counts those generations as lock_busy and leaves
//     the rest of the batch to commit. If one did, the generation may only be
//     larger than the batch, and the pass runs it again (outcome.continued).
//
// The batch commits when it deleted a row or finished a generation, and
// otherwise ends without a commit. A 55P03 from any statement aborts the
// transaction and counts every generation of the batch as lock_timeout; any
// other error, including a deadlock (40P01), is returned so the pass fails
// visibly.
func (s *sweeper) runBatch(ctx context.Context, batch []string) (batchOutcome, error) {
	started := time.Now()
	outcome := batchOutcome{rows: map[string]int64{}}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return outcome, fmt.Errorf("relationship retention: begin batch: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	fail := func(stage string, err error) (batchOutcome, error) {
		if isLockTimeout(err) {
			return batchOutcome{rows: map[string]int64{}, timedOut: len(batch), duration: time.Since(started)}, nil
		}
		return batchOutcome{rows: map[string]int64{}, duration: time.Since(started)},
			fmt.Errorf("relationship retention: %s: %w", stage, err)
	}

	if _, err := tx.ExecContext(ctx, setLockTimeoutQuery, lockTimeoutSetting); err != nil {
		return fail("set lock_timeout", err)
	}
	if err := s.hook(stageSetLockTimeout); err != nil {
		return fail("set lock_timeout", err)
	}

	locked, busy, err := tryLock(ctx, tx, batch)
	if err != nil {
		return fail("try lock", err)
	}
	outcome.busy += busy
	if err := s.hook(stageTryLock); err != nil {
		return fail("try lock", err)
	}
	if len(locked) == 0 {
		outcome.duration = time.Since(started)
		return outcome, nil
	}

	claimed, held, ineligible, err := classify(ctx, tx, locked)
	if err != nil {
		return fail("classify", err)
	}
	outcome.busy += held
	outcome.ineligible += ineligible
	if err := s.hook(stageClassify); err != nil {
		return fail("classify", err)
	}
	if len(claimed) == 0 {
		outcome.duration = time.Since(started)
		return outcome, nil
	}

	cut := false // a picking delete stopped at its row limit
	for _, step := range []struct {
		stage string
		query string
		table string
		picks bool // takes the row limit as its second argument
	}{
		{stageDeleteEvidence, deleteEvidenceQuery, EvidenceTable, true},
		{stageDeleteCandidates, deleteCandidatesQuery, CandidatesTable, true},
		{stageDeleteGenerations, deleteGenerationsQuery, GenerationsTable, false},
	} {
		args := []any{claimed}
		if step.picks {
			args = append(args, s.batchRows)
		}
		result, err := tx.ExecContext(ctx, step.query, args...)
		if err != nil {
			return fail(step.stage, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return fail(step.stage, err)
		}
		outcome.rows[step.table] = n
		if step.picks && n >= int64(s.batchRows) {
			cut = true
		}
		if err := s.hook(step.stage); err != nil {
			return fail(step.stage, err)
		}
	}
	left, err := settle(ctx, tx, claimed)
	if err != nil {
		return fail("settle", err)
	}
	if err := s.hook(stageSettle); err != nil {
		return fail("settle", err)
	}
	outcome.swept = len(claimed) - len(left)
	if cut {
		outcome.continued = left
	} else {
		outcome.busy += len(left)
	}
	if outcome.swept == 0 && outcome.total() == 0 {
		// Another session held everything the batch claimed: nothing was deleted
		// and no generation finished, so there is nothing to commit.
		outcome.rows = map[string]int64{}
		outcome.duration = time.Since(started)
		return outcome, nil
	}
	if err := tx.Commit(); err != nil {
		return fail("commit", err)
	}
	committed = true
	outcome.committed = true
	outcome.duration = time.Since(started)
	return outcome, nil
}

// tryLock takes the per-generation advisory lock for every id in batch and
// returns the ids it got and how many it did not.
func tryLock(ctx context.Context, q db.Queryer, batch []string) (locked []string, busy int, err error) {
	rows, err := q.QueryContext(ctx, tryLockQuery, batch)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var generationID string
		var got bool
		if err := rows.Scan(&generationID, &got); err != nil {
			return nil, 0, err
		}
		if got {
			locked = append(locked, generationID)
		} else {
			busy++
		}
	}
	return locked, busy, rows.Err()
}

// classify returns the ids the batch may delete (claimed), how many the
// predicates allowed but another session holds (held), and how many no longer
// satisfy Rule A (ineligible).
func classify(ctx context.Context, q db.Queryer, locked []string) (claimed []string, held, ineligible int, err error) {
	rows, err := q.QueryContext(ctx, classifyQuery, locked)
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var generationID string
		var predicatesHold, isClaimed bool
		if err := rows.Scan(&generationID, &predicatesHold, &isClaimed); err != nil {
			return nil, 0, 0, err
		}
		switch {
		case isClaimed:
			claimed = append(claimed, generationID)
		case predicatesHold:
			held++
		default:
			ineligible++
		}
	}
	return claimed, held, ineligible, rows.Err()
}

// settle returns the claimed generations that still have a row after the batch's
// deletes, in the order the batch claimed them: ones another session held, or
// ones larger than the batch's row limit. It returns each claimed id once and
// ignores anything else the statement returns, so the batch's swept count can
// never go below zero.
func settle(ctx context.Context, q db.Queryer, claimed []string) (left []string, err error) {
	rows, err := q.QueryContext(ctx, settleQuery, claimed)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	pending := make(map[string]struct{}, len(claimed))
	for _, id := range claimed {
		pending[id] = struct{}{}
	}
	got := make(map[string]struct{})
	for rows.Next() {
		var generationID string
		if err := rows.Scan(&generationID); err != nil {
			return nil, err
		}
		if _, ok := pending[generationID]; ok {
			delete(pending, generationID)
			got[generationID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range claimed {
		if _, ok := got[id]; ok {
			left = append(left, id)
		}
	}
	return left, nil
}

// hook runs the test-only fault hook after a statement; production never sets it.
func (s *sweeper) hook(stage string) error {
	if s.afterStatement == nil {
		return nil
	}
	return s.afterStatement(stage)
}

// isLockTimeout reports SQLSTATE 55P03, lock_timeout expiry: another session
// holds a relation-level lock the batch needs (the batch never waits for a row
// lock), and the batch rolled back with nothing applied. A deadlock (40P01) is
// deliberately not a timeout: it must surface.
func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}
