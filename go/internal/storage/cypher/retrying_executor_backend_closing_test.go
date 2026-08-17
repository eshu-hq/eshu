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
// The relationship one is the consequential half. GCPCloudResourceEdgeWriter
// emits one statement per relationship type in sort.Strings(cypherTypes) order,
// so a failure partway through leaves the alphabetical PREFIX of that scope's
// types written and the SUFFIX missing. Nothing repairs that except a replay:
// the handler's shouldSkipRetract stops skipping the prior-generation retract
// once AttemptCount > 1, so attempt 2 sweeps the partial write and rewrites the
// scope whole. Classified terminal, there is no attempt 2, and the scope keeps
// a permanently truncated edge set while the drain reports the rest of the
// graph healthy.

// TestBackendClosedDuringMergeChainRemainsQueueRetryable covers the
// unambiguous half: the store answered "DB Closed". No statement-shape guard
// is needed for it — a closed store wrote nothing.
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

	t.Run("missing start node in a non-MERGE group stays terminal", func(t *testing.T) {
		t.Parallel()
		err := newNeo4jError(nornicDBStatementSyntaxErrorCode,
			"UNWIND MERGE chain relationship create failed: start node nornic:abc does not exist")
		require.Empty(t, classifyRetryableGraphWriteGroupError(err, []Statement{{
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (source:CloudResource)-[rel]->() WHERE rel.scope_id IN $scope_ids DELETE rel",
		}}))
	})
}
