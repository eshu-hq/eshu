// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"errors"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const malformedNeo4jConnectivityErrorMessage = "neo4j connectivity error is missing its underlying cause"

const (
	nornicDBRestartTransactionStartCode = "Neo.ClientError.Transaction.TransactionStartFailed"
	nornicDBRestartTransactionStartMsg  = "failed to write WAL tx begin: wal: closed"
	// nornicDBStoreClosingCommitMsg is the body NornicDB reports under
	// nornicDBTransactionCommitFailedCode when its embedded Badger store has
	// already blocked writes for shutdown, so the commit is refused rather than
	// attempted. "Writes are blocked, possibly due to DropAll or Close" is
	// Badger's own exported ErrBlockedWrites sentence, which is why matching it
	// is stable across NornicDB builds; the code is checked alongside it so an
	// unrelated commit failure stays terminal.
	nornicDBStoreClosingCommitMsg = "Writes are blocked, possibly due to DropAll or Close"
	// nornicDBStoreClosedMsg is what NornicDB's UNWIND MERGE chain fast path
	// reports once the store is closed rather than merely closing, e.g.
	// "UNWIND MERGE chain create failed: checking node existence: reading node:
	// DB Closed". It arrives under nornicDBStatementSyntaxErrorCode -- the same
	// code a genuinely malformed query uses -- so the message, not the code, is
	// what separates a backend teardown from a Cypher bug here.
	nornicDBStoreClosedMsg = "DB Closed"
)

var errMalformedNeo4jConnectivity = errors.New(malformedNeo4jConnectivityErrorMessage)

// retryableNeo4jCodes lists Neo4j error codes that are safe to retry in
// reducer materialization paths. Scoped narrowly to codes evidenced as
// transient under concurrent projector/reducer graph access.
//
// See docs/public/reference/service-workflows.md and
// docs/public/deployment/service-runtimes.md for the current shared-write and
// reduction-flow contract behind these retry classifications.
var retryableNeo4jCodes = map[string]bool{
	"Neo.ClientError.Statement.EntityNotFound":        true,
	"Neo.TransientError.Transaction.DeadlockDetected": true,
}

// neo4jRetryableError wraps a Neo4j error and implements
// reducer.RetryableError for codes evidenced as transient in concurrent
// projector/reducer access patterns.
type neo4jRetryableError struct {
	inner error
	code  string
}

func (e *neo4jRetryableError) Error() string   { return e.inner.Error() }
func (e *neo4jRetryableError) Unwrap() error   { return e.inner }
func (e *neo4jRetryableError) Retryable() bool { return true }

// FailureClass reports the durable graph-write-timeout failure class so a
// transient driver-retry graph write (deadlock budget exhausted, connectivity
// loss) is recorded on the retrying row under the same class as a bounded
// graph-write deadline. Producer write-timeout backpressure (#3560) scopes its
// pressure signal to this class, which keeps a graph-write retry distinguishable
// from a reducer readiness backlog that also persists as a retrying row.
func (e *neo4jRetryableError) FailureClass() string { return GraphWriteTimeoutFailureClass }

// schemaFenceError marks a canonical write refused because the applied graph
// schema no longer admits this writer (see
// CanonicalNodeWriter.WithSchemaWriteFence).
//
// It is retryable rather than terminal on purpose. The refusal says this
// process must not write, not that the work is bad: the queue should hold it
// for a writer the schema does admit, which is the pod that replaces this one.
// Dead-lettering instead would turn a rolling upgrade into a backlog an
// operator has to redrive by hand.
//
// It carries no FailureClass. The graph-write-timeout class exists for
// transient driver failures and feeds producer write-timeout backpressure;
// a schema refusal is neither, and slowing producers would not clear it.
type schemaFenceError struct {
	inner error
}

func (e *schemaFenceError) Error() string   { return e.inner.Error() }
func (e *schemaFenceError) Unwrap() error   { return e.inner }
func (e *schemaFenceError) Retryable() bool { return true }

// WrapRetryableNeo4jError inspects err for graph-write failures that are safe
// to retry from the durable reducer queue. It wraps known retryable Neo4j error
// codes, driver retry-budget exhaustion, connectivity failures, and the three
// NornicDB failures a backend restart emits: the transaction-start failure
// raised once the WAL is closed (matched on an exact message), the commit
// failure raised once the store has blocked writes for shutdown, and the
// statement failure raised once the store is closed outright (both matched on a
// distinguishing substring, since their bodies carry variable context). Every
// one requires its error code as well. Malformed connectivity errors remain
// terminal, and all other errors are returned unchanged.
func WrapRetryableNeo4jError(err error) error {
	if err == nil {
		return nil
	}
	if isMalformedNeo4jConnectivityError(err) {
		return errMalformedNeo4jConnectivity
	}
	// TransactionExecutionLimit means session.ExecuteWrite exhausted its
	// internal retry budget (typically 30s for deadlocks). The queue should
	// retry later when contention subsides.
	var txLimit *neo4jdriver.TransactionExecutionLimit
	if errors.As(err, &txLimit) {
		return &neo4jRetryableError{inner: err, code: "TransactionExecutionLimit"}
	}
	var connectivityErr *neo4jdriver.ConnectivityError
	if errors.As(err, &connectivityErr) {
		return &neo4jRetryableError{inner: err, code: "ConnectivityError"}
	}
	if isNornicDBRestartTransactionStartFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBRestartTransactionStartCode}
	}
	if isNornicDBStoreClosingCommitFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBTransactionCommitFailedCode}
	}
	if isNornicDBStoreClosedStatementFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBStatementSyntaxErrorCode}
	}
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) {
		return err
	}
	if retryableNeo4jCodes[neo4jErr.Code] {
		return &neo4jRetryableError{inner: err, code: neo4jErr.Code}
	}
	return err
}

func isMalformedNeo4jConnectivityError(err error) bool {
	var connectivityErr *neo4jdriver.ConnectivityError
	return errors.As(err, &connectivityErr) && connectivityErr.Inner == nil
}

// isNornicDBRestartTransactionStartFailure recognizes the exact error emitted
// when a backend restart closes the WAL before a new transaction can begin.
// No transaction body has run, so both immediate and durable queue replay are
// safe. Keep the message guard alongside the code because NornicDB currently
// reports this backend-unavailable condition under a ClientError prefix.
func isNornicDBRestartTransactionStartFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBRestartTransactionStartCode &&
		neo4jErr.Msg == nornicDBRestartTransactionStartMsg
}

// isNornicDBStoreClosingCommitFailure recognizes the commit-side twin of
// isNornicDBRestartTransactionStartFailure: a backend restart that reaches the
// store between the transaction body and its commit, so Badger refuses the
// commit instead of the WAL refusing the begin. A restart interrupts a
// canonical write at one of four points -- store healthy, store closing
// (here), WAL closed (the start failure above), process gone (a
// *neo4jdriver.ConnectivityError) -- and this one was the only point still
// classified as terminal, which dead-lettered a restart as failure_class
// projection_bug and stranded every intent gated on the phase it would have
// published.
//
// Retry here means DURABLE QUEUE replay only; this shape is deliberately kept
// out of classifyTransientNeo4jError, which replays a transaction body in
// place. A commit failure leaves an outcome this process cannot observe, and
// that file already excludes the driver's own CommitFailedDeadError for
// exactly that reason. Queue replay needs no such observation: it re-runs the
// whole handler, and the canonical writers it drives are MERGE-shaped upserts
// that converge on the same node or edge however many times they run. Measured
// against the pinned image, a restart under a long grouped transaction rolls
// every executed statement back rather than tearing, so a replay finds nothing
// half-applied to begin with; and the relationship handlers keep a second
// guarantee anyway -- shouldSkipRetract stops skipping the prior-generation
// retract once AttemptCount exceeds 1, which would sweep a partial from any
// other source before the replay rewrites the scope.
//
// Both the code and the message are required, matching the start-failure guard
// beside it: NornicDB reports this backend-unavailable condition under a
// ClientError prefix that a real constraint violation also uses, so the code
// alone would swallow genuine terminal commit failures.
func isNornicDBStoreClosingCommitFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBTransactionCommitFailedCode &&
		strings.Contains(neo4jErr.Msg, nornicDBStoreClosingCommitMsg)
}

// isNornicDBStoreClosedStatementFailure recognizes a statement that failed
// because the store was already closed under it. NornicDB's UNWIND MERGE chain
// fast path reports this as nornicDBStatementSyntaxErrorCode, so the code alone
// cannot be trusted -- a genuinely malformed query carries the same one. The
// "DB Closed" body is what makes it unambiguous.
//
// It needs no statement-shape guard, but NOT because nothing was written -- an
// earlier version of this comment claimed that and it is false. This error was
// raised live from a WRITE transaction that had already executed 4,597 of
// 20,000 statements. Replay is safe on the measured outcome instead: that same
// probe found the interrupted transaction rolled back whole (survived=0 of
// 20,000), so there is nothing half-applied for a replay to double up. See
// evidence-6142-backend-restart-transient-classification.md.
func isNornicDBStoreClosedStatementFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBStatementSyntaxErrorCode &&
		strings.Contains(neo4jErr.Msg, nornicDBStoreClosedMsg)
}
