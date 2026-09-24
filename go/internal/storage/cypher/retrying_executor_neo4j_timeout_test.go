// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

// neo4jTransactionTimeoutErrors are the two statuses Neo4j reports when its
// transaction monitor terminates a transaction for exceeding a timeout
// (KernelImpl.beginTransaction, Neo4j 2026.06): the client-configured timeout
// sent with neo4j.WithTxTimeout and the server-wide db.transaction.timeout.
// Messages follow the Neo4j status-code catalogue descriptions.
func neo4jTransactionTimeoutErrors() map[string]*neo4jdriver.Neo4jError {
	return map[string]*neo4jdriver.Neo4jError{
		"client configuration": {
			Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
			Msg:  "The transaction has not completed within the timeout specified at its start by the client. You may want to retry with a longer timeout.",
		},
		"server db.transaction.timeout": {
			Code: "Neo.ClientError.Transaction.TransactionTimedOut",
			Msg:  "The transaction has not completed within the specified timeout (db.transaction.timeout). You may want to retry with a longer timeout.",
		},
	}
}

func TestRetryingExecutorDefersNeo4jTransactionTimeoutForReplaySafeGroup(t *testing.T) {
	t.Parallel()

	for name, timeoutErr := range neo4jTransactionTimeoutErrors() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inner := &nornicDBTimeoutGroupExecutor{err: timeoutErr}
			executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

			err := executor.ExecuteGroup(context.Background(), crossRepoRunsOnReplayGroup())
			require.Error(t, err)
			require.Equal(t, int32(1), inner.calls.Load(), "timeout must bypass local retry")
			require.True(t, reducer.IsRetryable(err))
			var classified interface{ FailureClass() string }
			require.True(t, errors.As(err, &classified))
			require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
			require.ErrorIs(t, err, timeoutErr)
			var deferred *neo4jRetryableError
			require.True(t, errors.As(err, &deferred))
			require.Equal(t, timeoutErr.Code, deferred.code, "deferral must record the status the backend sent")
		})
	}
}

func TestRetryingExecutorKeepsNeo4jTransactionTimeoutTerminalOutsideReplaySafeGroup(t *testing.T) {
	t.Parallel()

	for name, timeoutErr := range neo4jTransactionTimeoutErrors() {
		t.Run(name+" single statement", func(t *testing.T) {
			t.Parallel()

			inner := &nornicDBTimeoutExecutor{err: timeoutErr, failFor: 1}
			executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

			err := executor.Execute(context.Background(), Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MERGE (r:CloudResource {uid: $uid})",
			})
			require.Same(t, timeoutErr, err)
			require.Equal(t, int32(1), inner.calls.Load())
			require.False(t, reducer.IsRetryable(err))
		})
		t.Run(name+" unrelated group", func(t *testing.T) {
			t.Parallel()

			inner := &nornicDBTimeoutGroupExecutor{err: timeoutErr}
			executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

			err := executor.ExecuteGroup(context.Background(), []Statement{{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MERGE (r:CloudResource {uid: $uid})",
			}})
			require.Same(t, timeoutErr, err)
			require.Equal(t, int32(1), inner.calls.Load())
			require.False(t, reducer.IsRetryable(err))
		})
		t.Run(name+" nested in commit-ambiguous connectivity", func(t *testing.T) {
			t.Parallel()

			commitAmbiguousErr := &neo4jdriver.ConnectivityError{
				Inner: fmt.Errorf("Connection lost during commit: %w", timeoutErr),
			}
			inner := &nornicDBTimeoutGroupExecutor{err: commitAmbiguousErr}
			executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

			err := executor.ExecuteGroup(context.Background(), crossRepoRunsOnReplayGroup())
			require.Same(t, commitAmbiguousErr, err)
			require.Equal(t, int32(1), inner.calls.Load(), "nested timeout must not retry locally")
			require.False(t, reducer.IsRetryable(err))
		})
	}
}

func TestNeo4jTransactionTimeoutClassificationFailsClosedForLookalikes(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		&neo4jdriver.Neo4jError{Code: "Neo.ClientError.Transaction.TransactionTimedOutExtra", Msg: "timed out"},
		&neo4jdriver.Neo4jError{Code: "Neo.TransientError.Transaction.TransactionTimedOut", Msg: "timed out"},
		&neo4jdriver.Neo4jError{Code: "Neo.TransientError.Transaction.LockClientStopped", Msg: "stopped"},
		errors.New("Neo.ClientError.Transaction.TransactionTimedOut"),
	} {
		require.False(t, isTransactionTimedOut(err), "%v", err)
		require.False(t, hasTransactionTimeoutInUnknownOutcome(err), "%v", err)
	}
}
