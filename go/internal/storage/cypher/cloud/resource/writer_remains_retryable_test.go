// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resource

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

// This file holds the writer-dispatch halves of the backend-restart
// retryability proofs: each pins that a restart-shaped failure surfacing
// through the CloudResource node writer reaches the durable queue as
// retryable instead of dead-lettering. The executor and classification
// halves of the same restart spellings live in the root cypher package
// retrying-executor tests, which own the unexported retry internals these
// proofs must not duplicate (#6694).

func TestBackendRestartTransactionStartFailureRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartGroupExecutor{}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writerErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "restart-recovery-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writerErr)

	require.True(t, reducer.IsRetryable(handlerErr))
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionStartFailed", driverErr.Code)
	require.Equal(t, "failed to write WAL tx begin: wal: closed", driverErr.Msg)
}

// A graph-backend restart interrupts a canonical write at one of four points,
// and until this file every point but one was already classified:
//
//  1. store still healthy                     -- no failure
//  2. store closing, commit refused           -- THIS FILE (was terminal)
//  3. WAL closed before a tx could begin      -- isNornicDBRestartTransactionStartFailure
//  4. process gone, socket closed             -- *neo4jdriver.ConnectivityError
//
// Point 2 was the hole. Observed live on 2026-08-17 while reproducing the
// ifa-fault-injection gate's restart-backend-between-phase-groups cell: the
// gcp_resource_materialization intent for gcp:project:supply-chain-demo-project
// dead-lettered on attempt 1 as failure_class=projection_bug with
//
//	write canonical cloud resource nodes: Neo4jError:
//	Neo.ClientError.Transaction.TransactionCommitFailed
//	(commit failed: badger commit failed: Writes are blocked, possibly due to
//	DropAll or Close)
//
// and the dependent gcp_relationship_materialization intent then sat pending
// forever behind a canonical-nodes readiness phase that would never publish, so
// the drain never reached residual=0. A backend restart is the exact condition
// this gate exists to prove recovery from, so classifying it as a projection
// bug is wrong: it is a transient backend-unavailable failure and the durable
// queue must replay it.
func TestBackendRestartCommitBlockedWritesRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartTerminalGroupExecutor{
		err: newNeo4jError(
			"Neo.ClientError.Transaction.TransactionCommitFailed",
			"Writes are blocked, possibly due to DropAll or Close",
		),
	}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writerErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "restart-commit-recovery-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writerErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a commit refused because the graph store is closing is a backend restart, not a projection bug")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionCommitFailed", driverErr.Code)
}

// TestBackendRestartEngineClosedTransactionStartRemainsQueueRetryable is the
// end-to-end twin of the predicate test above, and it is the one that asserts
// the change's actual user-visible claim: an engine-closed begin failure no
// longer reaches the queue as failure_class=projection_bug.
//
// The predicate test alone cannot say that. It proves
// isNornicDBRestartTransactionStartFailure returns true, not that the value
// reducer.IsRetryable sees on a real writer error is retryable -- the WAL
// spelling has had this walk (TestBackendRestartTransactionStartFailureRemains-
// QueueRetryable, retrying_executor_backend_restart_test.go:130) since #6142 and
// the second spelling shipped without it.
func TestBackendRestartEngineClosedTransactionStartRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartEngineClosedGroupExecutor{}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writerErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "engine-closed-recovery-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writerErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"engine-closed begin failure must reach the queue as retryable, not as a projection bug")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionStartFailed", driverErr.Code)
	require.Equal(t, "failed to start transaction: engine is closed", driverErr.Msg)
}

// TestBackendRestartCommitStoreClosedRemainsQueueRetryable is the end-to-end
// twin: it asserts the change's user-visible claim through the real
// CloudResourceNodeWriter dispatch, which is the value reducer.IsRetryable
// actually sees. The predicate test above cannot say that on its own.
func TestBackendRestartCommitStoreClosedRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartTerminalGroupExecutor{
		err: &neo4jdriver.Neo4jError{
			Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
			Msg:  "commit failed: materializing mvcc commit state: DB Closed",
		},
	}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writerErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "store-closed-commit-recovery-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writerErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a commit refused because the graph store is closed must reach the queue as retryable, not as a projection bug")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionCommitFailed", driverErr.Code)
}

// TestBackendClosedDuringMergeChainRemainsQueueRetryable covers the
// unambiguous half: the store answered "DB Closed". No statement-shape guard is
// needed for it -- not because a closed store wrote nothing (it does not: this
// error was observed from a write transaction 4,597 statements in) but because
// the interrupted transaction was measured to roll back whole, survived=0 of
// 20,000, leaving nothing half-applied for a replay to double up.
func TestBackendClosedDuringMergeChainRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &backendRestartTerminalGroupExecutor{
		err: newNeo4jError(
			"Neo.ClientError.Statement.SyntaxError",
			"UNWIND MERGE chain create failed: checking node existence: reading node: "+"DB Closed",
		),
	}
	writer := NewCloudResourceNodeWriter(inner, 0)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w",
		writer.WriteCloudResourceNodes(
			context.Background(),
			[]map[string]any{{"uid": "backend-closed-resource"}},
			"reducer/gcp-resources",
		))

	require.True(t, reducer.IsRetryable(handlerErr),
		"a write that failed because the store reported DB Closed is a backend restart, not a projection bug")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
}
