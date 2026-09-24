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

// neo4jLockClientStoppedError is the status Neo4j reports when a transaction
// is terminated while it waits on another transaction's lock. The cause can be
// a transaction timeout, an operator's TERMINATE TRANSACTIONS, or a database
// shutdown; the status is the same for all three. This message is the one an
// operator kill produced on neo4j:2026-community.
func neo4jLockClientStoppedError() *neo4jdriver.Neo4jError {
	return &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.LockClientStopped",
		Msg:  "The transaction has been terminated. Retry your operation in a new transaction, and you should see a successful result. The transaction has been terminated, so no more locks can be acquired. This can occur because the transaction ran longer than the configured transaction timeout, or because a human operator manually terminated the transaction, or because the database is shutting down. ForsetiClient[transactionId=45, clientId=2]",
	}
}

// TestRetryingExecutorRequeuesNeo4jLockClientStoppedWithoutLocalRetry pins
// the outcome for a stopped lock client in every group shape: one attempt (no
// in-place replay that would wait another full timeout under the caller's
// lease) and a durable-queue retry, because the terminated transaction was
// rolled back whatever terminated it. Before the timeout classifier matched
// this status it requeued after in-place retries; it must never dead-letter.
func TestRetryingExecutorRequeuesNeo4jLockClientStoppedWithoutLocalRetry(t *testing.T) {
	t.Parallel()

	unrelatedGroup := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	}}
	for name, run := range map[string]func(*RetryingExecutor) error{
		"single statement": func(executor *RetryingExecutor) error {
			return executor.Execute(context.Background(), unrelatedGroup[0])
		},
		"unrelated group": func(executor *RetryingExecutor) error {
			return executor.ExecuteGroup(context.Background(), unrelatedGroup)
		},
		"replay-safe RUNS_ON group": func(executor *RetryingExecutor) error {
			return executor.ExecuteGroup(context.Background(), crossRepoRunsOnReplayGroup())
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stopped := neo4jLockClientStoppedError()
			inner := &nornicDBTimeoutGroupExecutor{err: stopped}
			executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

			err := run(executor)
			require.Error(t, err)
			require.Equal(t, int32(1), inner.calls.Load(), "a stopped lock client must not retry in place")
			require.True(t, reducer.IsRetryable(err), "a stopped lock client must requeue, not dead-letter")
			require.ErrorIs(t, err, stopped)
			var classified interface{ FailureClass() string }
			require.True(t, errors.As(err, &classified))
			require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
			var requeued *neo4jRetryableError
			require.True(t, errors.As(err, &requeued))
			require.Equal(t, stopped.Code, requeued.code)
		})
	}
}

// TestRetryingExecutorKeepsCommitAmbiguousLockClientStoppedTerminal keeps a
// stopped lock client nested under a lost-during-commit connectivity error on
// the path it took before the timeout work: the commit outcome is unknown, so
// it is neither replayed in place nor requeued.
func TestRetryingExecutorKeepsCommitAmbiguousLockClientStoppedTerminal(t *testing.T) {
	t.Parallel()

	commitAmbiguousErr := &neo4jdriver.ConnectivityError{
		Inner: fmt.Errorf("Connection lost during commit: %w", neo4jLockClientStoppedError()),
	}
	inner := &nornicDBTimeoutGroupExecutor{err: commitAmbiguousErr}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

	err := executor.ExecuteGroup(context.Background(), crossRepoRunsOnReplayGroup())
	require.Same(t, commitAmbiguousErr, err)
	require.Equal(t, int32(1), inner.calls.Load())
	require.False(t, reducer.IsRetryable(err))
}
