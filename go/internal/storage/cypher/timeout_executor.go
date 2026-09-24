// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TimeoutExecutor bounds individual graph write statements with a child
// context. A zero timeout preserves the caller's context unchanged.
type TimeoutExecutor struct {
	Inner       Executor
	Timeout     time.Duration
	TimeoutHint string
}

// GraphWriteTimeoutFailureClass is the durable failure_class persisted on a
// reducer/projector work item that failed (or is retrying) because a bounded
// graph write exceeded its deadline. It is the single source of truth shared by
// GraphWriteTimeoutError.FailureClass and the producer write-backpressure gate,
// which counts only retrying rows in this class so readiness backlogs cannot
// false-throttle reducer admission (#3560).
const GraphWriteTimeoutFailureClass = "graph_write_timeout"

// Typed statuses a graph backend reports for a transaction it terminated at
// its timeout; see isTransactionTimedOut.
const (
	// transactionTimedOutClientConfigurationCode is the status both backends
	// report after rolling back a transaction that exceeded the timeout the
	// client sent with it (neo4j.WithTxTimeout, ESHU_CANONICAL_WRITE_TIMEOUT).
	// NornicDB reuses Neo4j's status verbatim.
	transactionTimedOutClientConfigurationCode = "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration"
	// transactionTimedOutServerConfigurationCode is the status Neo4j reports
	// when the server-wide db.transaction.timeout terminates the transaction
	// instead. KernelImpl.beginTransaction (Neo4j 2026.06) builds both
	// statuses from the same TransactionTimeout, so the rollback guarantee is
	// identical and the classifier treats them the same way.
	transactionTimedOutServerConfigurationCode = "Neo.ClientError.Transaction.TransactionTimedOut"
	// transactionLockClientStoppedCode is what Neo4j reports when a
	// transaction is terminated while it waits on another transaction's lock:
	// termination stops the lock client
	// (KernelTransactionImplementation.markForTerminationIfPossible), so the
	// pending acquisition fails with this status. The cause can be a
	// transaction timeout, an operator's TERMINATE TRANSACTIONS, or a database
	// shutdown, and the status does not say which. It is therefore not a
	// timeout status; see lockClientStoppedRequeue for how it is handled.
	transactionLockClientStoppedCode = "Neo.ClientError.Transaction.LockClientStopped"
)

// isTransactionTimedOut identifies the exact typed error a graph backend
// emits after rolling back a transaction that reached its timeout: NornicDB
// and Neo4j both report the client-configured status, and Neo4j reports the
// server-configured status for db.transaction.timeout. In Neo4j both come from
// the kernel's termination path, which rolls the transaction back rather than
// committing it (KernelTransactionImplementation.closeTransaction checks
// canCommit before commitTransaction). It is durable-queue retryable only
// when its outer error chain preserves the known rollback outcome.
func isTransactionTimedOut(err error) bool {
	return transactionTimeoutCode(err) != ""
}

// transactionTimeoutCode returns the typed timeout status carried by err, or
// an empty string when err is not a transaction timeout.
func transactionTimeoutCode(err error) string {
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) {
		return ""
	}
	switch neo4jErr.Code {
	case transactionTimedOutClientConfigurationCode,
		transactionTimedOutServerConfigurationCode:
		return neo4jErr.Code
	default:
		return ""
	}
}

// lockClientStoppedRequeue returns a durable-queue retryable error when err
// is Neo4j's stopped-lock-client status with a known rollback outcome, and nil
// otherwise. Every termination reason rolls the transaction back, so a queue
// replay is safe in every group. It is not retried in place: after a timeout,
// each in-place replay would wait another full timeout while the caller holds
// its lease. A status nested in a connectivity error or a driver execution
// limit has an unknown outer outcome and is left to the ordinary classifier.
func lockClientStoppedRequeue(operation string, err error) error {
	if hasUnknownTransactionOutcome(err) {
		return nil
	}
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) || neo4jErr.Code != transactionLockClientStoppedCode {
		return nil
	}
	slog.Warn(
		"neo4j transaction terminated during lock wait, requeueing without local retry",
		"operation", operation,
		"code", neo4jErr.Code,
		"error", err.Error(),
	)
	return &neo4jRetryableError{inner: err, code: transactionLockClientStoppedCode}
}

// GraphWriteTimeoutError marks a graph write deadline/cancellation with enough
// context for queue failure classifiers and operator status surfaces.
type GraphWriteTimeoutError struct {
	Operation   string
	Timeout     time.Duration
	TimeoutHint string
	Summary     string
	Cause       error
}

func (e GraphWriteTimeoutError) Error() string {
	prefix := e.Operation
	if e.Timeout > 0 {
		prefix = fmt.Sprintf("%s after %s", prefix, e.Timeout)
	}
	if e.TimeoutHint != "" {
		prefix = fmt.Sprintf("%s; adjust %s to tune the graph write budget", prefix, e.TimeoutHint)
	}
	if e.Summary != "" {
		return fmt.Sprintf("%s (%s): %v", prefix, e.Summary, e.Cause)
	}
	return fmt.Sprintf("%s: %v", prefix, e.Cause)
}

func (e GraphWriteTimeoutError) Unwrap() error {
	return e.Cause
}

func (e GraphWriteTimeoutError) FailureClass() string {
	return GraphWriteTimeoutFailureClass
}

func (e GraphWriteTimeoutError) FailureDetails() string {
	if e.Summary == "" {
		return e.Error()
	}
	return e.Summary
}

// Retryable marks graph write deadlines as bounded-retry candidates. A timeout
// can be caused by transient backend pressure, while syntax/schema failures
// still remain terminal because they do not implement the retry contract.
func (e GraphWriteTimeoutError) Retryable() bool {
	return true
}

// Execute forwards the statement with an optional deadline.
func (e TimeoutExecutor) Execute(ctx context.Context, statement Statement) error {
	if e.Inner == nil {
		return fmt.Errorf("inner executor is required")
	}
	if e.Timeout <= 0 {
		return e.Inner.Execute(ctx, statement)
	}

	execCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	err := e.Inner.Execute(execCtx, statement)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutError(
			"neo4j execute timed out",
			e.Timeout,
			e.TimeoutHint,
			statementSummary(statement),
			context.DeadlineExceeded,
		)
	}
	if errors.Is(execCtx.Err(), context.Canceled) {
		return timeoutError(
			"neo4j execute canceled before completion",
			0,
			"",
			statementSummary(statement),
			context.Canceled,
		)
	}
	return err
}

// ExecuteGroup forwards grouped statements with an optional deadline when the
// wrapped executor supports atomic grouped writes.
func (e TimeoutExecutor) ExecuteGroup(ctx context.Context, statements []Statement) error {
	if e.Inner == nil {
		return fmt.Errorf("inner executor is required")
	}
	ge, ok := e.Inner.(GroupExecutor)
	if !ok {
		return fmt.Errorf("inner executor does not support ExecuteGroup")
	}
	if e.Timeout <= 0 {
		return ge.ExecuteGroup(ctx, statements)
	}

	execCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	err := ge.ExecuteGroup(execCtx, statements)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutError(
			"neo4j execute group timed out",
			e.Timeout,
			e.TimeoutHint,
			statementGroupSummary(statements),
			context.DeadlineExceeded,
		)
	}
	if errors.Is(execCtx.Err(), context.Canceled) {
		return timeoutError(
			"neo4j execute group canceled before completion",
			0,
			"",
			statementGroupSummary(statements),
			context.Canceled,
		)
	}
	return err
}

// ExecuteProbe forwards a read-only probe with an optional deadline when the
// wrapped executor supports probing. It returns an error without attempting
// the probe when Inner does not implement ProbeExecutor.
func (e TimeoutExecutor) ExecuteProbe(ctx context.Context, stmt Statement) (bool, error) {
	if e.Inner == nil {
		return false, fmt.Errorf("inner executor is required")
	}
	pe, ok := e.Inner.(ProbeExecutor)
	if !ok {
		return false, fmt.Errorf("inner executor does not support ExecuteProbe")
	}
	if e.Timeout <= 0 {
		return pe.ExecuteProbe(ctx, stmt)
	}

	execCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	found, err := pe.ExecuteProbe(execCtx, stmt)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return false, timeoutError(
			"neo4j execute probe timed out",
			e.Timeout,
			e.TimeoutHint,
			statementSummary(stmt),
			context.DeadlineExceeded,
		)
	}
	if errors.Is(execCtx.Err(), context.Canceled) {
		return false, timeoutError(
			"neo4j execute probe canceled before completion",
			0,
			"",
			statementSummary(stmt),
			context.Canceled,
		)
	}
	return found, err
}

func timeoutError(prefix string, timeout time.Duration, timeoutHint string, summary string, cause error) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		return GraphWriteTimeoutError{
			Operation:   prefix,
			Timeout:     timeout,
			TimeoutHint: timeoutHint,
			Summary:     summary,
			Cause:       cause,
		}
	}
	if timeout > 0 {
		prefix = fmt.Sprintf("%s after %s", prefix, timeout)
	}
	if timeoutHint != "" {
		prefix = fmt.Sprintf("%s; adjust %s to tune the graph write budget", prefix, timeoutHint)
	}
	if summary != "" {
		return fmt.Errorf("%s (%s): %w", prefix, summary, cause)
	}
	return fmt.Errorf("%s: %w", prefix, cause)
}

func statementGroupSummary(statements []Statement) string {
	if len(statements) == 0 {
		return ""
	}
	return statementSummary(statements[0])
}

func statementSummary(statement Statement) string {
	if statement.Parameters == nil {
		return ""
	}
	summary, _ := statement.Parameters[StatementMetadataSummaryKey].(string)
	return summary
}
