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

// This file holds the commit-side restart-classification tests that assert
// root retry internals (classifyTransientNeo4jError,
// isNornicDBStoreClosingCommitFailure) with raw executor drives. They stay in
// the root package because those predicates are unexported: a leaf copy
// could not call them. The writer-driving halves of the same restart
// spellings live with their writers in the cloud leaves (#6694).

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

// closedPropertyKeyDictionaryCommitMsg is the RAW body from Ifa cell
// restart-backend-between-phase-groups (runs 36349511251, 36292315197,
// 36210727831), not built from the constant under test: a typo in the
// production constant must turn this red.
const closedPropertyKeyDictionaryCommitMsg = "commit failed: persisting property key dictionary: property key dictionary persistence requires an open database"

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

// closedStoreCommitReadMsg is the RAW body from Ifa run 35819550601 (shard
// 4/4, cell restart-backend-between-phase-groups, work-items.csv
// failure_message), not built from the constants under test: a typo in the
// production constant must turn this red, not stay green alongside it. The
// %q-encoded key bytes are part of the wire text.
const closedStoreCommitReadMsg = `commit failed: DB::Get key: "\x0e\x00\x00\x00\x00\x00\x00\x00\xb4" err: DB Closed`

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

// The closed-store-read widening rests on the Badger wrap's shape, not on
// the words "DB Closed": the body must open a quoted Get key and end with
// Badger's ` err: DB Closed` tail. Every near miss keeps the predicate
// false. The writer-driving half of these cases lives in the
// cloud/resource leaf, which cannot call this unexported predicate.
func TestBackendRestartCommitClosedStoreReadPredicateNearMissesStayTerminal(t *testing.T) {
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
		})
	}
}
