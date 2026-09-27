// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// FailureClass names a counting link failure (#7127 ruling 8.10). Only a
// failure that happened once the link statement ran is counted: a failure of
// begin or of the lock reads is returned as a plain error and counts nothing.
type FailureClass string

const (
	// FailureStatementTimeout is SQLSTATE 57014 or the transaction deadline.
	FailureStatementTimeout FailureClass = "statement_timeout"
	// FailureConnectionLost is a terminated backend (57P01-57P03), a
	// connection-exception SQLSTATE (class 08) or a broken connection.
	FailureConnectionLost FailureClass = "connection_lost"
	// FailureSQLError is any other PostgreSQL error.
	FailureSQLError FailureClass = "sql_error"
	// FailureInternal is any other error.
	FailureInternal FailureClass = "internal"
)

const (
	// DefaultMaxAttempts is the default counting-failure limit before an
	// activation becomes a link_poisoned break.
	DefaultMaxAttempts = 5
	// backoffBase and backoffCap give next_attempt_at =
	// now + min(backoffCap, backoffBase * 2^(count-1)).
	backoffBase = 30 * time.Second
	backoffCap  = 30 * time.Minute
)

// FailureError is a counting link failure of one activation. The
// transaction rolled back; the caller records it with RecordFailure.
type FailureError struct {
	Class         FailureClass
	ScopeID       string
	GenerationID  string
	ActivationSeq int64
	Err           error
}

// Error describes the failure class, the activation and the cause.
func (e *FailureError) Error() string {
	return fmt.Sprintf("changed-since link failed (%s) at activation %d of %s: %v",
		e.Class, e.ActivationSeq, e.ScopeID, e.Err)
}

// Unwrap returns the cause.
func (e *FailureError) Unwrap() error { return e.Err }

// ClassifyFailure maps an error from the link statement onward to its
// counting class.
func ClassifyFailure(err error) FailureClass {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FailureStatementTimeout
	case errors.As(err, &pgErr):
		switch {
		case pgErr.Code == "57014":
			return FailureStatementTimeout
		case pgErr.Code == "57P01", pgErr.Code == "57P02", pgErr.Code == "57P03", strings.HasPrefix(pgErr.Code, "08"):
			return FailureConnectionLost
		default:
			return FailureSQLError
		}
	case connectionLost(err):
		return FailureConnectionLost
	default:
		return FailureInternal
	}
}

func connectionLost(err error) bool {
	var netErr net.Error
	var connectErr *pgconn.ConnectError
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &netErr) || errors.As(err, &connectErr) || pgconn.SafeToRetry(err) ||
		strings.Contains(err.Error(), "conn closed")
}

// Backoff returns the wait after the count-th consecutive counting failure:
// min(30 min, 30 s * 2^(count-1)).
func Backoff(count int) time.Duration {
	if count < 1 {
		count = 1
	}
	d := backoffBase
	for i := 1; i < count; i++ {
		d *= 2
		if d >= backoffCap {
			return backoffCap
		}
	}
	return d
}

// FailureRecord describes what RecordFailure wrote.
type FailureRecord struct {
	// Counted is false when the cursor was held by another writer or the
	// head activation moved on; nothing was written then.
	Counted bool
	// Attempts is the consecutive counting failures of the activation,
	// including this one.
	Attempts      int
	NextAttemptAt time.Time
	// Poisoned is true when this failure reached the limit: the activation
	// is now a link_poisoned break and the cursor is past it.
	Poisoned bool
}

// RecordFailure counts one counting failure of the scope's head activation
// in its own short transaction, under the cursor lock, after the failed link
// rolled back (#7127 ruling 8.10). Only the worker that ran the link statement
// calls it, so colliding replicas cannot inflate the count. At maxAttempts
// (DefaultMaxAttempts when below 1) it poisons the activation: the cursor
// advances past it, the state is kept, and the poison marker is set.
func (w *LinkWriter) RecordFailure(ctx context.Context, failure *FailureError, maxAttempts int) (FailureRecord, error) {
	if failure == nil {
		return FailureRecord{}, errors.New("changed-since record failure: nil failure")
	}
	if maxAttempts < 1 {
		maxAttempts = DefaultMaxAttempts
	}
	tx, err := w.begin(ctx)
	if err != nil {
		return FailureRecord{}, fmt.Errorf("changed-since record failure: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	cursor, locked, err := lockCursor(ctx, tx, failure.ScopeID)
	if err != nil || !locked {
		return FailureRecord{}, err
	}
	head, found, err := nextActivation(ctx, tx, failure.ScopeID, cursor.activationSeq)
	if err != nil || !found || head.seq != failure.ActivationSeq {
		return FailureRecord{}, err
	}
	record := FailureRecord{Counted: true, Attempts: 1}
	if cursor.attemptActivationSeq == head.seq {
		record.Attempts = cursor.attemptCount + 1
	}
	now := w.now()
	if record.Attempts >= maxAttempts {
		record.Poisoned = true
		_, err = tx.ExecContext(ctx, poisonActivationQuery, failure.ScopeID, head.seq, string(failure.Class), now)
	} else {
		record.NextAttemptAt = now.Add(Backoff(record.Attempts))
		_, err = tx.ExecContext(ctx, recordAttemptQuery, failure.ScopeID, head.seq, record.Attempts,
			record.NextAttemptAt, string(failure.Class), now)
	}
	if err != nil {
		return FailureRecord{}, fmt.Errorf("changed-since record failure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return FailureRecord{}, fmt.Errorf("changed-since record failure: commit: %w", err)
	}
	committed = true
	return record, nil
}
