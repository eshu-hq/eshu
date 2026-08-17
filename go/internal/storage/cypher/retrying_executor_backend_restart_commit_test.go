// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

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
			nornicDBTransactionCommitFailedCode,
			nornicDBStoreClosingCommitMsg,
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
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, nornicDBTransactionCommitFailedCode, driverErr.Code)
}

// TestBackendRestartCommitBlockedWritesIsNotReplayedInPlace pins the
// deliberate half of the fix. Unlike the transaction-START failure, a COMMIT
// failure has an outcome this process cannot observe, and this file's sibling
// classifyTransientNeo4jError already excludes the driver's own
// CommitFailedDeadError for exactly that reason. Recovery therefore belongs to
// the durable queue, which replays the whole handler -- idempotent by
// construction, because the canonical writers are MERGE-shaped and
// shouldSkipRetract forces a full prior-edge retract once AttemptCount > 1, so
// a partially-applied prior attempt is cleaned up rather than doubled. Retrying
// the transaction body in place carries no such guarantee, and during a real
// restart it would burn the whole in-place budget inside the first second of a
// multi-second outage anyway.
func TestBackendRestartCommitBlockedWritesIsNotReplayedInPlace(t *testing.T) {
	t.Parallel()

	blocked := newNeo4jError(nornicDBTransactionCommitFailedCode, nornicDBStoreClosingCommitMsg)
	require.Empty(t, classifyTransientNeo4jError(blocked))

	inner := &backendRestartTerminalGroupExecutor{err: blocked}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}
	err := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})",
	}})
	require.Same(t, blocked, err)
	require.Equal(t, int32(1), inner.calls.Load())
}

// TestBackendRestartCommitBlockedWritesClassificationFailsClosedForNearMisses
// keeps the new classification as narrow as the transaction-start one it sits
// beside: it takes BOTH the commit-failed code AND the blocked-writes body, so
// an unrelated commit failure (a real constraint violation) and an unrelated
// carrier of the same sentence both stay terminal.
func TestBackendRestartCommitBlockedWritesClassificationFailsClosedForNearMisses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "commit-failed code with an unrelated body",
			err: newNeo4jError(
				nornicDBTransactionCommitFailedCode,
				"commit failed: constraint violation: UNIQUE on :CloudResource(uid) already exists",
			),
		},
		{
			name: "blocked-writes body under an unrelated code",
			err: newNeo4jError(
				nornicDBStatementSyntaxErrorCode,
				nornicDBStoreClosingCommitMsg,
			),
		},
		{
			name: "plain error carrying the blocked-writes body",
			err:  fmt.Errorf("%s", nornicDBStoreClosingCommitMsg),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := WrapRetryableNeo4jError(tt.err)
			require.Same(t, tt.err, wrapped)
			require.False(t, reducer.IsRetryable(wrapped))
		})
	}
}
