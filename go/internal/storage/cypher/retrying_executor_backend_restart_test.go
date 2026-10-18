// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

type backendRestartGroupExecutor struct {
	calls atomic.Int32
}

type backendRestartSequentialExecutor struct {
	calls      atomic.Int32
	alwaysFail bool
}

type backendRestartTerminalGroupExecutor struct {
	err   error
	calls atomic.Int32
}

func (e *backendRestartSequentialExecutor) Execute(context.Context, Statement) error {
	if e.calls.Add(1) == 1 || e.alwaysFail {
		return newNeo4jError(
			"Neo.ClientError.Transaction.TransactionStartFailed",
			"failed to write WAL tx begin: wal: closed",
		)
	}
	return nil
}

func (e *backendRestartGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *backendRestartGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	if e.calls.Add(1) == 1 {
		return newNeo4jError(
			"Neo.ClientError.Transaction.TransactionStartFailed",
			"failed to write WAL tx begin: wal: closed",
		)
	}
	return nil
}

func (e *backendRestartTerminalGroupExecutor) Execute(context.Context, Statement) error {
	e.calls.Add(1)
	return e.err
}

func (e *backendRestartTerminalGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	e.calls.Add(1)
	return e.err
}

func TestRetryingExecutorRetriesBackendRestartTransactionStartFailure(t *testing.T) {
	t.Parallel()

	inner := &backendRestartGroupExecutor{}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})",
	}})
	require.NoError(t, err)
	require.Equal(t, int32(2), inner.calls.Load())
}

func TestRetryingExecutorExecuteRetriesBackendRestartTransactionStartFailure(t *testing.T) {
	t.Parallel()

	inner := &backendRestartSequentialExecutor{}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), inner.calls.Load())
}

func TestRetryingExecutorExecuteBackendRestartExhaustionRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartSequentialExecutor{alwaysFail: true}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    "MERGE (r:CloudResource {uid: $uid})",
	})
	require.Error(t, err)
	require.Equal(t, int32(2), inner.calls.Load())

	wrapped := WrapRetryableNeo4jError(err)
	require.True(t, reducer.IsRetryable(wrapped))
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, wrapped, &classified)
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, wrapped, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionStartFailed", driverErr.Code)
	require.Equal(t, "failed to write WAL tx begin: wal: closed", driverErr.Msg)
}

func TestBackendRestartTransactionStartFailureClassificationFailsClosedForNearMisses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "same code with different message",
			err: newNeo4jError(
				"Neo.ClientError.Transaction.TransactionStartFailed",
				"failed to write WAL tx begin: permission denied",
			),
		},
		{
			name: "same message with different code",
			err: newNeo4jError(
				"Neo.ClientError.Transaction.TransactionCommitFailed",
				"failed to write WAL tx begin: wal: closed",
			),
		},
		{
			name: "plain error with same message",
			err:  fmt.Errorf("%s", "failed to write WAL tx begin: wal: closed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Empty(t, classifyTransientNeo4jError(tt.err))
			wrapped := WrapRetryableNeo4jError(tt.err)
			require.Same(t, tt.err, wrapped)
			require.False(t, reducer.IsRetryable(wrapped))

			inner := &backendRestartTerminalGroupExecutor{err: tt.err}
			executor := &RetryingExecutor{
				Inner:      inner,
				MaxRetries: 3,
				BaseDelay:  time.Nanosecond,
			}
			err := executor.ExecuteGroup(context.Background(), []Statement{{
				Operation: OperationCanonicalUpsert,
				Cypher:    "UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})",
			}})
			require.Same(t, tt.err, err)
			require.Equal(t, int32(1), inner.calls.Load())

			sequentialInner := &backendRestartTerminalGroupExecutor{err: tt.err}
			sequentialExecutor := &RetryingExecutor{
				Inner:      sequentialInner,
				MaxRetries: 3,
				BaseDelay:  time.Nanosecond,
			}
			err = sequentialExecutor.Execute(context.Background(), Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MERGE (r:CloudResource {uid: $uid})",
			})
			require.Same(t, tt.err, err)
			require.Equal(t, int32(1), sequentialInner.calls.Load())
		})
	}
}
