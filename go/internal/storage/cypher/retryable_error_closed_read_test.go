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

// closedStoreCommitReadMsg is the RAW body from Ifa run 35819550601 (shard
// 4/4, cell restart-backend-between-phase-groups, work-items.csv
// failure_message), not built from the constants under test: a typo in the
// production constant must turn this red, not stay green alongside it. The
// %q-encoded key bytes are part of the wire text.
const closedStoreCommitReadMsg = `commit failed: DB::Get key: "\x0e\x00\x00\x00\x00\x00\x00\x00\xb4" err: DB Closed`

// TestBackendRestartCommitClosedStoreReadReachesDurableQueue pins the fourth
// commit-side spelling of a NornicDB restart: a Badger point read inside
// commit validation (constraint or snapshot-isolation check) on a store that
// Close has already torn down. Badger wraps it as `DB::Get key: %q` + ` err:
// DB Closed`; NornicDB's Commit rolls the transaction back before any durable
// write; the executor prefixes `commit failed:`. Nothing is half-applied, so
// the queue must retry it after the restart instead of dead-lettering it as a
// projection bug (which is what run 35819550601 did).
func TestBackendRestartCommitClosedStoreReadReachesDurableQueue(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  closedStoreCommitReadMsg,
	}
	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writeErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "closed-store-read-retry-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writeErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a closed-store read during commit validation must requeue the resource instead of dead-lettering it")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var gotDriverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &gotDriverErr)
	require.Same(t, driverErr, gotDriverErr)
}

// The commit failed, so the transaction body must not be replayed in place by
// the RetryingExecutor; the durable queue owns the retry after the restart.
func TestBackendRestartCommitClosedStoreReadWaitsForDurableQueue(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  closedStoreCommitReadMsg,
	}
	require.Empty(t, classifyTransientNeo4jError(driverErr),
		"a commit failure must not replay the transaction body in place")

	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}
	err := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})",
	}})
	require.Same(t, driverErr, err)
	require.EqualValues(t, 1, inner.calls.Load())
}

// The widening rests on the Badger wrap's shape, not on the words "DB Closed":
// the body must open a quoted Get key and end with Badger's ` err: DB Closed`
// tail. Every near miss stays terminal.
func TestBackendRestartCommitClosedStoreReadNearMissesStayTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "same body under another code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Schema.ConstraintValidationFailed",
				Msg:  closedStoreCommitReadMsg,
			},
		},
		{
			name: "constraint identity contains the whole body",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  `commit failed: constraint violation: Node with uid="repos/acme/` + closedStoreCommitReadMsg + `" already exists`,
			},
		},
		{
			name: "Get key wrap without the closed-store tail",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  `commit failed: DB::Get key: "\x0e\x00" err: Key not found`,
			},
		},
		{
			name: "closed-store tail without the Get key wrap",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  `commit failed: something else err: DB Closed`,
			},
		},
		{
			name: "trailing text after the tail",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  closedStoreCommitReadMsg + `: unrelated error`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, isNornicDBStoreClosingCommitFailure(tt.err), "predicate must stay false")
			inner := &backendRestartTerminalGroupExecutor{err: tt.err}
			writer := NewCloudResourceNodeWriter(inner, 0)
			writeErr := writer.WriteCloudResourceNodes(
				context.Background(),
				[]map[string]any{{"uid": "closed-store-read-near-miss"}},
				"reducer/gcp-resources",
			)
			require.False(t, reducer.IsRetryable(fmt.Errorf("write canonical cloud resource nodes: %w", writeErr)),
				"near miss must stay terminal")
		})
	}
}
