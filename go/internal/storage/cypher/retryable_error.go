package cypher

import (
	"errors"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// retryableNeo4jCodes lists Neo4j error codes that are safe to retry in
// reducer materialization paths. Scoped narrowly to codes evidenced as
// transient under concurrent projector/reducer graph access.
//
// See docs/docs/reference/service-workflows.md and
// docs/docs/deployment/service-runtimes.md for the current shared-write and
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

// WrapRetryableNeo4jError inspects err for known retryable Neo4j error codes
// or driver-level retry exhaustion. If the error (or any wrapped error in the
// chain) is a *neo4j.Neo4jError with a retryable code, or a
// *neo4j.TransactionExecutionLimit (driver exhausted its internal retry
// budget), the error is wrapped in a type implementing reducer.RetryableError.
// Otherwise the original error is returned unchanged.
func WrapRetryableNeo4jError(err error) error {
	if err == nil {
		return nil
	}
	// TransactionExecutionLimit means session.ExecuteWrite exhausted its
	// internal retry budget (typically 30s for deadlocks). The queue should
	// retry later when contention subsides.
	var txLimit *neo4jdriver.TransactionExecutionLimit
	if errors.As(err, &txLimit) {
		return &neo4jRetryableError{inner: err, code: "TransactionExecutionLimit"}
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
