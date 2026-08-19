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
// construction, because the canonical writers are MERGE-shaped, and safe
// besides because a restart under a grouped transaction was measured to roll
// every executed statement back rather than tear (see
// evidence-6142-backend-restart-transient-classification.md). shouldSkipRetract
// forcing a full prior-edge retract once AttemptCount > 1 is a second guarantee
// on top of that, not the load-bearing one. Retrying
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

// Point 3 above was only PARTLY classified. isNornicDBRestartTransactionStartFailure
// matched the begin-side teardown on an exact message, "failed to write WAL tx
// begin: wal: closed" -- but NornicDB reports the same condition a second way
// when the engine is already closed rather than only its WAL:
//
//	write canonical gcp relationship edges: Neo4jError:
//	Neo.ClientError.Transaction.TransactionStartFailed
//	(failed to start transaction: engine is closed)
//
// Observed live on 2026-08-18 in the same restart-backend-between-phase-groups
// cell, dead-lettering a gcp_relationship_materialization write as
// failure_class=projection_bug and failing the drain at residual=1. Both
// spellings mean the transaction never began, so replay is equally safe; only
// the wording differs. This test pins the second spelling so narrowing the
// match back to one message fails here rather than intermittently in a
// twenty-minute Docker cell, which is how both halves of this were found.
func TestBackendRestartEngineClosedTransactionStartIsRetryable(t *testing.T) {
	t.Parallel()

	// Code and Msg are RAW LITERALS, copied from the observed NornicDB errors
	// above, never the constants under test. Building the input out of
	// nornicDBEngineClosedTransactionStartMsg would make this assert only that
	// the classifier references the same constant it is compared against -- it
	// would stay green through a typo or a truncation in that constant, while
	// the real backend message went back to dead-lettering as
	// failure_class=projection_bug. That is the co-derivation ban the shell
	// side of this change states in capitals for its own pins
	// (scripts/lib/test-ifa-fault-injection-shard-cases.sh), applied here.
	engineClosed := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionStartFailed",
		Msg:  "failed to start transaction: engine is closed",
	}
	require.True(t, isNornicDBRestartTransactionStartFailure(engineClosed),
		"engine-closed begin failure must classify as a backend restart, not a projection bug")

	walClosed := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionStartFailed",
		Msg:  "failed to write WAL tx begin: wal: closed",
	}
	require.True(t, isNornicDBRestartTransactionStartFailure(walClosed),
		"the original WAL-closed spelling must keep classifying")

	// Same code, unrelated body: must stay terminal. Without this the fix would
	// be a widening that swallows genuine begin-time faults.
	unrelated := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionStartFailed",
		Msg:  "failed to start transaction: constraint violation",
	}
	require.False(t, isNornicDBRestartTransactionStartFailure(unrelated),
		"an unrelated TransactionStartFailed body must remain terminal")
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
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var driverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &driverErr)
	require.Equal(t, "Neo.ClientError.Transaction.TransactionStartFailed", driverErr.Code)
	require.Equal(t, "failed to start transaction: engine is closed", driverErr.Msg)
}

// backendRestartEngineClosedGroupExecutor fails its first ExecuteGroup with the
// engine-closed spelling and succeeds afterwards, mirroring
// backendRestartGroupExecutor's WAL-spelling shape.
type backendRestartEngineClosedGroupExecutor struct {
	calls atomic.Int32
}

func (e *backendRestartEngineClosedGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *backendRestartEngineClosedGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	if e.calls.Add(1) == 1 {
		return newNeo4jError(
			"Neo.ClientError.Transaction.TransactionStartFailed",
			"failed to start transaction: engine is closed",
		)
	}
	return nil
}
