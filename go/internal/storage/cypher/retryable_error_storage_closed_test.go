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

func TestBackendRestartCommitStorageClosedReachesDurableQueue(t *testing.T) {
	t.Parallel()

	// The code and body are raw literals from Ifa run 35259033020, shard 4.
	// Constructing the input from production constants would hide a typo.
	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  "commit failed: storage closed",
	}
	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	writer := NewCloudResourceNodeWriter(inner, 0)
	writeErr := writer.WriteCloudResourceNodes(
		context.Background(),
		[]map[string]any{{"uid": "storage-closed-retry-resource"}},
		"reducer/gcp-resources",
	)
	handlerErr := fmt.Errorf("write canonical cloud resource nodes: %w", writeErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a closed store during commit validation must requeue the resource instead of dead-lettering it")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var gotDriverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &gotDriverErr)
	require.Same(t, driverErr, gotDriverErr)
}

func TestBackendRestartCommitStorageClosedWaitsForDurableQueue(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  "commit failed: storage closed",
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

func TestBackendRestartCommitStorageClosedNearMissesStayTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "same body under another code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Schema.ConstraintValidationFailed",
				Msg:  "commit failed: storage closed",
			},
		},
		{
			name: "constraint identity contains the body",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  `commit failed: constraint violation: Node with uid="repos/acme/commit failed: storage closed" already exists`,
			},
		},
		{
			name: "same code with a longer body",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  "commit failed: storage closed: unrelated error",
			},
		},
		{
			name: "same code with only the tail",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg:  "storage closed",
			},
		},
		{
			name: "plain error carrying the body",
			err:  fmt.Errorf("commit failed: storage closed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Same(t, tt.err, WrapRetryableNeo4jError(tt.err))
			require.False(t, reducer.IsRetryable(tt.err))
		})
	}
}
