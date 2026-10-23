// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Database is the Postgres surface the sweeper needs: a transaction per batch
// and a plain query for the catalog size read.
type Database interface {
	db.Beginner
	db.Queryer
}

// Sweeper deletes retention-eligible workflow_work_items rows in bounded
// batch transactions. It keeps the primary-key cursor of its walk between
// passes, so a pass that stops at MaxBatches resumes where it left off. The
// cursor lives in memory: a restart walks from the table head again, which
// is safe because every batch re-checks eligibility.
//
// A Sweeper is safe for concurrent use; concurrent passes serialize on the
// cursor. Separate processes may run their own Sweeper against one database:
// SKIP LOCKED keeps their batches from waiting on each other, and a row one
// process deleted is simply absent for the other.
type Sweeper struct {
	database Database

	mu     sync.Mutex
	cursor string
}

// NewSweeper returns a Sweeper over database.
func NewSweeper(database Database) *Sweeper {
	return &Sweeper{database: database}
}

// PruneWorkItems runs one retention pass as of asOf: up to policy.MaxBatches
// batch transactions of policy.BatchSize rows each, stopping early when the
// walk reaches the table end. A failed batch rolls back, keeps the cursor
// before its window so the next pass retries it, and returns the batches
// committed so far alongside the error.
func (s *Sweeper) PruneWorkItems(
	ctx context.Context,
	asOf time.Time,
	policy workflow.WorkItemRetentionPolicy,
) (workflow.WorkItemRetentionResult, error) {
	var result workflow.WorkItemRetentionResult
	if s == nil || s.database == nil {
		return result, errors.New("workflow work item retention database is required")
	}
	if err := policy.Validate(); err != nil {
		return result, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := policy.Cutoff(asOf)
	lockTimeout := lockTimeoutSetting(policy.LockTimeout)
	for result.Batches < policy.MaxBatches {
		started := time.Now()
		batch, err := s.runBatch(ctx, s.cursor, policy.BatchSize, cutoff, lockTimeout)
		if err != nil {
			return result, fmt.Errorf("workflow work item retention batch after %q: %w", s.cursor, err)
		}
		result.Batches++
		result.BatchDurations = append(result.BatchDurations, time.Since(started))
		result.ScannedRows += int(batch.scanned)
		result.DeletedCompleted += int(batch.deletedCompleted)
		result.DeletedFailedTerminal += int(batch.deletedFailedTerminal)
		if batch.scanned < int64(policy.BatchSize) || !batch.nextCursor.Valid {
			s.cursor = ""
			result.Wrapped = true
			return result, nil
		}
		s.cursor = batch.nextCursor.String
	}
	return result, nil
}

// batchOutcome is the one row pruneBatchQuery returns.
type batchOutcome struct {
	nextCursor            sql.NullString
	scanned               int64
	deletedCompleted      int64
	deletedFailedTerminal int64
}

// runBatch runs one window in its own transaction under local lock and
// statement timeouts, and commits it.
func (s *Sweeper) runBatch(
	ctx context.Context,
	cursor string,
	batchSize int,
	cutoff time.Time,
	lockTimeout string,
) (batchOutcome, error) {
	var outcome batchOutcome
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return outcome, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, setBatchTimeoutsQuery, lockTimeout, statementTimeoutSetting); err != nil {
		return outcome, fmt.Errorf("set timeouts: %w", err)
	}
	rows, err := tx.QueryContext(ctx, pruneBatchQuery, cursor, batchSize, cutoff)
	if err != nil {
		return outcome, fmt.Errorf("delete window: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return outcome, fmt.Errorf("delete window: %w", err)
		}
		return outcome, errors.New("delete window returned no summary row")
	}
	if err := rows.Scan(
		&outcome.nextCursor,
		&outcome.scanned,
		&outcome.deletedCompleted,
		&outcome.deletedFailedTerminal,
	); err != nil {
		_ = rows.Close()
		return outcome, fmt.Errorf("scan window summary: %w", err)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return outcome, fmt.Errorf("delete window: %w", err)
	}
	if err := rows.Close(); err != nil {
		return outcome, fmt.Errorf("close window summary: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return outcome, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return outcome, nil
}

// WorkItemTableSize reads workflow_work_items' total relation bytes and the
// planner's live-row estimate from the catalog.
func (s *Sweeper) WorkItemTableSize(ctx context.Context) (workflow.WorkItemTableSize, error) {
	var size workflow.WorkItemTableSize
	if s == nil || s.database == nil {
		return size, errors.New("workflow work item retention database is required")
	}
	rows, err := s.database.QueryContext(ctx, tableSizeQuery)
	if err != nil {
		return size, fmt.Errorf("read workflow work item table size: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return size, fmt.Errorf("read workflow work item table size: %w", err)
		}
		return size, errors.New("read workflow work item table size: workflow_work_items not in pg_class")
	}
	if err := rows.Scan(&size.Bytes, &size.EstimatedRows); err != nil {
		return size, fmt.Errorf("scan workflow work item table size: %w", err)
	}
	return size, rows.Err()
}

// lockTimeoutSetting renders a lock timeout as a Postgres duration string in
// whole milliseconds.
func lockTimeoutSetting(timeout time.Duration) string {
	return strconv.FormatInt(timeout.Milliseconds(), 10) + "ms"
}
