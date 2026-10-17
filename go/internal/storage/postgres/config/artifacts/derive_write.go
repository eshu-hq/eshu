// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// failureRecordTimeout bounds the failure-only state update. It runs on a
// context detached from the derive's, so a derive that timed out or whose
// caller was canceled still records its failure.
const failureRecordTimeout = 5 * time.Second

// The last_error_class values a failed derive records, besides
// sqlstate_<SQLSTATE> for a PostgreSQL error.
const (
	errorClassPanic    = "panic"
	errorClassTimeout  = "timeout"
	errorClassCanceled = "canceled"
	errorClassError    = "error"
)

// writeState is the part of the state row step 3 reads under the lock: the
// version and extractor version of the last successful derive, and its row
// count. contentVersion and rowCount are nil until a derive succeeds.
type writeState struct {
	contentVersion   *int64
	extractorVersion int
	rowCount         *int
}

// mayWrite is the write condition of protocol section 3, step 3. state is the
// repository's state row read under the lock, or nil; v0 is the version this
// derive read in step 1; extractorVersion is this binary's. A derive writes
// when one of these holds:
//
//   - No state row exists, or it records no successful derive (row_count or
//     content_version is NULL, the failure-only row). This clause is
//     Proposed: a failure-only row has a NULL content_version, and v0 > NULL
//     is unknown, so without it no derive could replace that row. A NULL
//     content_version with a non-NULL row_count counts as no success, as in
//     Classify.
//   - v0 > state.content_version and extractorVersion >= state's.
//   - v0 = state.content_version and extractorVersion > state's.
//
// Otherwise the derive is superseded: a newer version or extractor already
// owns the rows, so an older binary never replaces newer rows.
func mayWrite(state *writeState, v0 int64, extractorVersion int) bool {
	if state == nil || state.rowCount == nil || state.contentVersion == nil {
		return true
	}
	stored := *state.contentVersion
	switch {
	case v0 > stored:
		return extractorVersion >= state.extractorVersion
	case v0 == stored:
		return extractorVersion > state.extractorVersion
	default:
		return false
	}
}

// readContentVersion is step 1: v0, read on the pool outside any transaction.
func readContentVersion(ctx context.Context, queryer db.Queryer, repoID string) (int64, error) {
	rows, err := queryer.QueryContext(ctx, contentVersionSQL, repoID)
	if err != nil {
		return 0, fmt.Errorf("read config artifact content version of %q: %w", repoID, err)
	}
	defer func() { _ = rows.Close() }()
	var version int64
	if rows.Next() {
		if err := rows.Scan(&version); err != nil {
			return 0, fmt.Errorf("scan config artifact content version of %q: %w", repoID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read config artifact content version of %q: %w", repoID, err)
	}
	return version, nil
}

// readWriteState reads the state row inside step 3, after the lock.
func readWriteState(ctx context.Context, tx db.Transaction, repoID string) (*writeState, error) {
	rows, err := tx.QueryContext(ctx, readStateSQL, repoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var contentVersion, rowCount sql.NullInt64
	var state writeState
	if err := rows.Scan(&contentVersion, &state.extractorVersion, &rowCount); err != nil {
		return nil, err
	}
	if contentVersion.Valid {
		state.contentVersion = &contentVersion.Int64
	}
	if rowCount.Valid {
		count := int(rowCount.Int64)
		state.rowCount = &count
	}
	return &state, rows.Err()
}

// write is step 3: one short transaction under the repository lock. It
// returns OutcomeSuperseded, after a rollback, when mayWrite refuses.
func (d *Deriver) write(ctx context.Context, req Request, v0 int64, rows []Row, stats contentartifacts.ReadStats) (Outcome, error) {
	tx, err := d.database.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin config artifact write of %q: %w", req.RepoID, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, repoLockSQL, req.RepoID); err != nil {
		return "", fmt.Errorf("lock config artifacts of %q: %w", req.RepoID, err)
	}
	if d.afterLock != nil {
		d.afterLock(ctx)
	}
	state, err := readWriteState(ctx, tx, req.RepoID)
	if err != nil {
		return "", fmt.Errorf("read config artifact state of %q: %w", req.RepoID, err)
	}
	if !mayWrite(state, v0, d.extractorVersion) {
		return OutcomeSuperseded, nil
	}
	if _, err := tx.ExecContext(ctx, deleteRowsSQL, req.RepoID); err != nil {
		return "", fmt.Errorf("delete config artifact rows of %q: %w", req.RepoID, err)
	}
	if len(rows) > 0 {
		if _, err := tx.ExecContext(ctx, insertRowsSQL, insertRowsArgs(req.RepoID, rows)...); err != nil {
			return "", fmt.Errorf("insert config artifact rows of %q: %w", req.RepoID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, upsertStateSQL,
		req.RepoID, v0, d.extractorVersion, len(rows), stats.FilesListed,
		stats.CandidateCount, stats.CandidatesHydrated, stats.ListTruncated,
		req.ScopeID, req.GenerationID,
	); err != nil {
		return "", fmt.Errorf("upsert config artifact state of %q: %w", req.RepoID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit config artifacts of %q: %w", req.RepoID, err)
	}
	committed = true
	return OutcomeOK, nil
}

// insertRowsArgs builds the parallel arrays of insertRowsSQL.
func insertRowsArgs(repoID string, rows []Row) []any {
	rawPaths := make(array.StringArray, 0, len(rows))
	relativePaths := make(array.StringArray, 0, len(rows))
	kinds := make(array.StringArray, 0, len(rows))
	scoped := make(array.StringArray, 0, len(rows))
	for _, row := range rows {
		rawPaths = append(rawPaths, row.RawPath)
		relativePaths = append(relativePaths, row.RelativePath)
		kinds = append(kinds, row.EvidenceKind)
		scoped = append(scoped, strconv.FormatBool(row.NameScoped))
	}
	return []any{repoID, rawPaths, relativePaths, kinds, scoped}
}

// recordFailure is the failure-only state update of protocol section 3: in
// its own short transaction under the repository lock it counts the failure
// and records its class and time (recordFailureSQL). It never changes the
// rows, content_version, extractor_version or row_count of an existing state
// row. It runs on a context detached from the caller's cancellation and
// bounded by failureRecordTimeout.
func (d *Deriver) recordFailure(ctx context.Context, repoID, errorClass string) (err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureRecordTimeout)
	defer cancel()
	tx, err := d.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin config artifact failure record of %q: %w", repoID, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, repoLockSQL, repoID); err != nil {
		return fmt.Errorf("lock config artifacts of %q: %w", repoID, err)
	}
	if _, err = tx.ExecContext(ctx, recordFailureSQL, repoID, d.extractorVersion, errorClass); err != nil {
		return fmt.Errorf("record config artifact failure of %q: %w", repoID, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit config artifact failure of %q: %w", repoID, err)
	}
	return nil
}

// panicError is a panic recovered inside a derive.
type panicError struct {
	value any
	stack string
}

func newPanicError(value any) *panicError {
	return &panicError{value: value, stack: string(debug.Stack())}
}

func (e *panicError) Error() string {
	return fmt.Sprintf("config artifact derive panicked: %v", e.value)
}

// classifyFailure maps a failed derive to its outcome and last_error_class.
// deadline is the derive context's error when the failure was seen: a
// deadline means timeout, a cancellation of the caller means canceled.
func classifyFailure(err, deadline error) (Outcome, string) {
	var panicked *panicError
	if errors.As(err, &panicked) {
		return OutcomePanic, errorClassPanic
	}
	if errors.Is(deadline, context.DeadlineExceeded) {
		return OutcomeTimeout, errorClassTimeout
	}
	if errors.Is(deadline, context.Canceled) {
		return OutcomeError, errorClassCanceled
	}
	var coded interface{ SQLState() string }
	if errors.As(err, &coded) && coded.SQLState() != "" {
		return OutcomeError, "sqlstate_" + coded.SQLState()
	}
	return OutcomeError, errorClassError
}
