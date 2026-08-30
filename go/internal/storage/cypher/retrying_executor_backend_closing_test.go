// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/stretchr/testify/require"
)

// NornicDB does not confine a shutting-down store to transaction-lifecycle
// error codes. Its UNWIND MERGE chain fast path reports the same condition as
// Neo.ClientError.Statement.SyntaxError, which is the code a genuinely
// malformed query uses, so both shapes below were terminal and dead-lettered a
// backend restart as failure_class=projection_bug at attempt 1.
//
// Both were observed live on 2026-08-17 on an idle machine, from a single
// restart of the fault-injection gate's restart-backend-between-phase-groups
// cell, which produced three dead letters at once:
//
//	gcp_resource_materialization     acme-demo-gcp-01  projection_bug
//	  write canonical cloud resource nodes: … Neo.ClientError.Statement.SyntaxError
//	  (UNWIND MERGE chain create failed: checking node existence: reading node: DB Closed)
//	gcp_resource_materialization     acme-demo-gcp-03  projection_bug  (same)
//	gcp_relationship_materialization acme-demo-gcp-02  projection_bug
//	  write canonical gcp relationship edges: … Neo.ClientError.Statement.SyntaxError
//	  (UNWIND MERGE chain relationship create failed: start node nornic:565f703f-… does not exist)
//
// Classified terminal, each of these dead-lettered at attempt 1, and because
// gcp_resource_materialization publishes the canonical-nodes-committed phase,
// the relationship intents behind it then waited on a readiness gate that could
// never open. The drain never reached residual=0.
//
// Replay is safe here for a measured reason, not an assumed one. An earlier
// version of this comment argued that these statements go out one per
// relationship type in sort.Strings(cypherTypes) order, so an interrupted write
// leaves an alphabetical prefix that only a replay can sweep. Probed against
// the pinned image, that is not what happens: restarting the backend under a
// 20,000-statement transaction rolled every one of the 4,597 already-executed
// statements back (survived=0) and failed loudly. The group is atomic on this
// path, so a replay has nothing partial to double-apply — a weaker premise than
// the original, and the correct one. See
// evidence-6142-backend-restart-transient-classification.md.

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
			nornicDBStatementSyntaxErrorCode,
			"UNWIND MERGE chain create failed: checking node existence: reading node: "+nornicDBStoreClosedMsg,
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
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
}

// TestMergeChainRelationshipCreateMissingStartNodeIsRetryable covers the
// consequential half, and pins that it rides the SAME MERGE-shaped guard the
// sibling "update failed: not found" conflict already uses. Replay is safe on
// its own terms even if the start node were genuinely absent: the statement is
// MERGE-shaped, so re-execution converges, and a start node that really does
// not exist simply fails again and dead-letters once the retry budget is spent
// — the same terminal outcome as today, reached only after recovery was
// actually attempted.
func TestMergeChainRelationshipCreateMissingStartNodeIsRetryable(t *testing.T) {
	t.Parallel()

	err := newNeo4jError(
		nornicDBStatementSyntaxErrorCode,
		"UNWIND MERGE chain relationship create failed: start node nornic:565f703f-871f-4a20-8bf9-109df868f90f does not exist",
	)

	require.NotEmpty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MATCH (source:CloudResource {uid: row.source_uid}) MERGE (source)-[rel:GCP_route_in_network]->(target)",
	}}))

	inner := &backendRestartTerminalGroupExecutor{err: err}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 2, BaseDelay: time.Nanosecond}
	groupErr := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MATCH (source:CloudResource {uid: row.source_uid}) MERGE (source)-[rel:GCP_route_in_network]->(target)",
	}})
	require.Error(t, groupErr)
	require.Equal(t, int32(3), inner.calls.Load(), "the group must be replayed in place before the budget is spent")
	require.True(t, reducer.IsRetryable(groupErr),
		"an exhausted MERGE-chain replay must still reach the durable queue rather than dead-letter")
}

// TestBackendClosingMergeChainClassificationFailsClosedForNearMisses keeps both
// additions narrow. A real Cypher syntax error carries the same code, so the
// message is what separates a backend teardown from a malformed query, and a
// non-MERGE group must not be replayed on the missing-start-node shape.
func TestBackendClosingMergeChainClassificationFailsClosedForNearMisses(t *testing.T) {
	t.Parallel()

	t.Run("a real syntax error stays terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			"Invalid input 'MERG': expected 'MERGE' (line 1, column 1)")
		require.Empty(t, classifyTransientNeo4jError(err))
		require.Empty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
			Cypher: "UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})",
		}}))
		require.Same(t, err, WrapRetryableNeo4jError(err))
		require.False(t, reducer.IsRetryable(WrapRetryableNeo4jError(err)))
	})

	t.Run("DB Closed body under an unrelated code stays terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError("Neo.ClientError.Security.Unauthorized",
			"UNWIND MERGE chain create failed: checking node existence: reading node: "+nornicDBStoreClosedMsg)
		require.Same(t, err, WrapRetryableNeo4jError(err))
		require.False(t, reducer.IsRetryable(WrapRetryableNeo4jError(err)))
	})

	// The neighbouring create-side body. An earlier draft of this change
	// matched the "create failed" prefix alone and silently made this
	// retryable; TestRetryingExecutorDoesNotBroadenRelationshipSnapshotRetry
	// caught it. Pinned here too so the boundary is visible from the change
	// that introduced it, not only from the guard that happened to catch it.
	t.Run("create failed: not found stays terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			"UNWIND MERGE chain relationship create failed: not found")
		require.Empty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
			Cypher: "UNWIND $rows AS row MERGE (source)-[rel:GCP_route_in_network]->(target)",
		}}))
		require.Same(t, err, WrapRetryableNeo4jError(err))
	})

	// The guard claims the fragments BRACKET the node id. Reversed order must
	// therefore stay terminal; two unordered Contains calls would accept it.
	t.Run("reversed fragments stay terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			" does not exist UNWIND MERGE chain relationship create failed: start node ")
		require.Empty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
			Cypher: "UNWIND $rows AS row MERGE (source)-[rel:GCP_route_in_network]->(target)",
		}}))
		require.Same(t, err, WrapRetryableNeo4jError(err))
	})

	// #6176 moved this boundary from "contains MERGE" to "converges on
	// replay", so the group that must stay terminal is one holding a statement
	// a second execution would double-apply. The predicate-scoped retract this
	// subtest used to carry is now replay-safe and is asserted retryable in the
	// following subtest; keeping it here would have pinned the old gate rather
	// than the safety property behind it.
	t.Run("missing start node in a non-idempotent group stays terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			"UNWIND MERGE chain relationship create failed: start node nornic:abc does not exist")
		require.Empty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
			Operation: OperationCanonicalUpsert,
			Cypher:    "MATCH (source:CloudResource {uid: $uid}) CREATE (a:Audit {uid: $audit_uid})",
		}}))
	})

	t.Run("missing start node in an idempotent retract group is replayed", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			"UNWIND MERGE chain relationship create failed: start node nornic:abc does not exist")
		require.Equal(t, graphWriteRetryReasonWriteConflict,
			classifyRetryableGraphWriteGroupError(err, []Statement{{
				Operation: OperationCanonicalRetract,
				Cypher:    "MATCH (source:CloudResource)-[rel]->() WHERE rel.scope_id IN $scope_ids DELETE rel",
			}}),
			"deleting the same predicate-bound edges twice removes the same set, so the replay is safe")
	})
}
