// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"errors"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const malformedNeo4jConnectivityErrorMessage = "neo4j connectivity error is missing its underlying cause"

const (
	nornicDBRestartTransactionStartCode = "Neo.ClientError.Transaction.TransactionStartFailed"
	nornicDBRestartTransactionStartMsg  = "failed to write WAL tx begin: wal: closed"
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
// codes, driver retry-budget exhaustion, connectivity failures, and the exact
// NornicDB transaction-start failure emitted during backend restart. Malformed
// connectivity errors remain terminal, and all other errors are returned
// unchanged.
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
