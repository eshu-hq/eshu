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
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var gotDriverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &gotDriverErr)
	require.Same(t, driverErr, gotDriverErr)
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
			require.Same(t, tt.err, sourcecypher.WrapRetryableNeo4jError(tt.err))
			require.False(t, reducer.IsRetryable(tt.err))
		})
	}
}

// TestBackendRestartCommitPropertyKeyDictionaryClosedReachesDurableQueue pins
// the sixth commit-side spelling of a NornicDB restart: the property key
// dictionary persisted at commit after Close has nil'd its database. The
// commit is refused before the storage write and the transaction rolls back,
// so the GCP relationship edge upsert must requeue instead of dead-lettering
// as projection_bug.
