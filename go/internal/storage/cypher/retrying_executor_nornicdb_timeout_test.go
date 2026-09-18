// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

type nornicDBTimeoutExecutor struct {
	calls   atomic.Int32
	err     error
	failFor int32
}

type nornicDBTimeoutGroupExecutor struct {
	calls atomic.Int32
	err   error
}

func (e *nornicDBTimeoutExecutor) Execute(context.Context, Statement) error {
	if e.calls.Add(1) <= e.failFor {
		return e.err
	}
	return nil
}

func (e *nornicDBTimeoutGroupExecutor) Execute(context.Context, Statement) error {
	e.calls.Add(1)
	return e.err
}

func (e *nornicDBTimeoutGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	e.calls.Add(1)
	return e.err
}

func TestRetryingExecutorDoesNotDeferNornicDBTransactionTimedOutClientConfigurationForSingleStatement(t *testing.T) {
	t.Parallel()

	inner := &nornicDBTimeoutExecutor{
		err: &neo4jdriver.Neo4jError{
			Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
			Msg:  "lock acquisition timed out",
		},
		failFor: 1,
	}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	})
	require.Same(t, inner.err, err)
	require.Equal(t, int32(1), inner.calls.Load())
	require.False(t, reducer.IsRetryable(err))
}

func TestNornicDBTransactionTimedOutClientConfigurationClassificationFailsClosedForLookalikes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "same code prefix",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfigurationExtra",
				Msg:  "transaction timed out",
			},
		},
		{
			name: "same message with different code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  "transaction timed out",
			},
		},
		{
			name: "plain error with exact code",
			err:  errors.New("Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Empty(t, classifyTransientNeo4jError(tt.err))
			inner := &nornicDBTimeoutExecutor{err: tt.err, failFor: 1}
			executor := &RetryingExecutor{
				Inner:      inner,
				MaxRetries: 1,
				BaseDelay:  time.Nanosecond,
			}

			err := executor.Execute(context.Background(), Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MERGE (r:CloudResource {uid: $uid})",
			})
			require.Same(t, tt.err, err)
			require.Equal(t, int32(1), inner.calls.Load())
			require.False(t, reducer.IsRetryable(err))
		})
	}
}

func TestRetryingExecutorDoesNotDeferNornicDBTransactionTimeoutForNonReplaySafeStatement(t *testing.T) {
	t.Parallel()

	timeoutErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
		Msg:  "transaction timed out",
	}
	inner := &nornicDBTimeoutExecutor{err: timeoutErr, failFor: 1}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

	err := executor.Execute(context.Background(), Statement{
		Operation: OperationUpsertNode,
		Cypher:    "CREATE (r:CloudResource {uid: $uid})",
	})
	require.Same(t, timeoutErr, err)
	require.Equal(t, int32(1), inner.calls.Load())
	require.False(t, reducer.IsRetryable(err))
}

func TestRetryingExecutorDoesNotDeferNornicDBTransactionTimeoutForGroup(t *testing.T) {
	t.Parallel()

	timeoutErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
		Msg:  "transaction timed out",
	}
	inner := &nornicDBTimeoutGroupExecutor{err: timeoutErr}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

	err := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	}})
	require.Same(t, timeoutErr, err)
	require.Equal(t, int32(1), inner.calls.Load())
	require.False(t, reducer.IsRetryable(err))
}

func TestRetryingExecutorDoesNotDeferNornicDBTransactionTimeoutNestedInCommitAmbiguousConnectivityError(t *testing.T) {
	t.Parallel()

	timeoutErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
		Msg:  "transaction timed out",
	}
	commitAmbiguousErr := &neo4jdriver.ConnectivityError{
		Inner: fmt.Errorf("Connection lost during commit: %w", timeoutErr),
	}
	inner := &nornicDBTimeoutExecutor{err: commitAmbiguousErr, failFor: 1}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}

	err := executor.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	})
	require.Same(t, commitAmbiguousErr, err)
	require.Equal(t, int32(1), inner.calls.Load())
	require.False(t, reducer.IsRetryable(err))
}
