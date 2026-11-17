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
)

// batchOutcome is what one batch transaction did.
type batchOutcome struct {
	// swept is the generations the batch deleted, and rows its deletes'
	// affected row counts by table. Both are zero unless the batch committed.
	swept int
	rows  map[string]int64
	// busy, ineligible and timedOut count generations left for a later pass.
	busy       int
	ineligible int
	timedOut   int
	duration   time.Duration
}

// runBatch deletes the relationship rows of up to BatchGenerations orphan
// generations in one transaction, in the fixed order the arbiter ruled:
//
//  1. lock_timeout 500 ms, transaction local (never zero);
//  2. a per-generation advisory try lock, so two sweepers never take the same
//     generation, and an id another sweeper holds is skipped;
//  3. classification, which re-checks Rule A in this snapshot and locks the
//     generation rows FOR UPDATE SKIP LOCKED, so an activation or refinalize in
//     flight is skipped and a committed one is re-checked under the lock;
//  4. evidence, then candidates, then the generation row last, each delete
//     taking only the ids classification claimed and re-asserting every Rule A
//     predicate in its own WHERE.
//
// The whole batch commits or none of it does, so a crash leaves nothing
// half-deleted. A 55P03 from any statement aborts the transaction and counts
// every generation of the batch as lock_timeout; any other error, including a
// deadlock (40P01), is returned so the pass fails visibly.
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

	for _, step := range []struct {
		stage string
		query string
		table string
	}{
		{stageDeleteEvidence, deleteEvidenceQuery, EvidenceTable},
		{stageDeleteCandidates, deleteCandidatesQuery, CandidatesTable},
		{stageDeleteGenerations, deleteGenerationsQuery, GenerationsTable},
	} {
		result, err := tx.ExecContext(ctx, step.query, claimed)
		if err != nil {
			return fail(step.stage, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return fail(step.stage, err)
		}
		outcome.rows[step.table] = n
		if err := s.hook(step.stage); err != nil {
			return fail(step.stage, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail("commit", err)
	}
	committed = true
	outcome.swept = len(claimed)
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

// hook runs the test-only fault hook after a statement; production never sets it.
func (s *sweeper) hook(stage string) error {
	if s.afterStatement == nil {
		return nil
	}
	return s.afterStatement(stage)
}

// isLockTimeout reports SQLSTATE 55P03, lock_timeout expiry: another session
// holds rows the batch needs, and the batch rolled back with nothing applied.
// A deadlock (40P01) is deliberately not a timeout: it must surface.
func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}
