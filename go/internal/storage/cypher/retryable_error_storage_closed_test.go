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

// closedPropertyKeyDictionaryCommitMsg is the RAW body from Ifa cell
// restart-backend-between-phase-groups (runs 36349511251, 36292315197,
// 36210727831), not built from the constant under test: a typo in the
// production constant must turn this red.
const closedPropertyKeyDictionaryCommitMsg = "commit failed: persisting property key dictionary: property key dictionary persistence requires an open database"

// TestBackendRestartCommitPropertyKeyDictionaryClosedReachesDurableQueue pins
// the sixth commit-side spelling of a NornicDB restart: the property key
// dictionary persisted at commit after Close has nil'd its database. The
// commit is refused before the storage write and the transaction rolls back,
// so the GCP relationship edge upsert must requeue instead of dead-lettering
// as projection_bug.
func TestBackendRestartCommitPropertyKeyDictionaryClosedReachesDurableQueue(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  closedPropertyKeyDictionaryCommitMsg,
	}
	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	writer := NewGCPCloudResourceEdgeWriter(inner, 0)
	writeErr := writer.WriteCloudResourceEdges(
		context.Background(), gcpCloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/gcp-relationships",
	)
	handlerErr := fmt.Errorf("write canonical gcp relationship edges: %w", writeErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a closed property key dictionary at commit must requeue the edge write instead of dead-lettering it")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, GraphWriteTimeoutFailureClass, classified.FailureClass())
	var gotDriverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &gotDriverErr)
	require.Same(t, driverErr, gotDriverErr)
}

// The commit failed, so the RetryingExecutor must not replay the body in
// place; the durable queue owns the retry after the restart.
func TestBackendRestartCommitPropertyKeyDictionaryClosedIsNotReplayedInPlace(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  closedPropertyKeyDictionaryCommitMsg,
	}
	require.Empty(t, classifyTransientNeo4jError(driverErr),
		"a commit failure must not replay the transaction body in place")

	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	executor := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Nanosecond}
	err := executor.ExecuteGroup(context.Background(), []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MATCH (s:CloudResource {uid: row.source_uid}) MATCH (t:CloudResource {uid: row.target_uid}) MERGE (s)-[:USES]->(t)",
	}})
	require.Same(t, driverErr, err)
	require.EqualValues(t, 1, inner.calls.Load())
}

// The match is exact equality: every near miss stays terminal.
func TestBackendRestartCommitPropertyKeyDictionaryClosedNearMissesStayTerminal(t *testing.T) {
	t.Parallel()

	const commitCode = "Neo.ClientError.Transaction.TransactionCommitFailed"
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "same body under another code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Schema.ConstraintValidationFailed",
				Msg:  closedPropertyKeyDictionaryCommitMsg,
			},
		},
		{
			name: "extra prefix",
			err:  &neo4jdriver.Neo4jError{Code: commitCode, Msg: "wrapped: " + closedPropertyKeyDictionaryCommitMsg},
		},
		{
			name: "extra suffix",
			err:  &neo4jdriver.Neo4jError{Code: commitCode, Msg: closedPropertyKeyDictionaryCommitMsg + ": unrelated error"},
		},
		{
			name: "constraint identity contains the body",
			err: &neo4jdriver.Neo4jError{
				Code: commitCode,
				Msg:  `commit failed: constraint violation: Node with uid="repos/acme/` + closedPropertyKeyDictionaryCommitMsg + `" already exists`,
			},
		},
		{
			name: "plain error carrying the body",
			err:  fmt.Errorf("%s", closedPropertyKeyDictionaryCommitMsg),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, isNornicDBStoreClosingCommitFailure(tt.err), "predicate must stay false")
			require.Same(t, tt.err, WrapRetryableNeo4jError(tt.err))
			require.False(t, reducer.IsRetryable(tt.err))
		})
	}
}
