// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const observedNornicDBRelationshipSnapshotConflict = "UNWIND MERGE chain relationship update failed: not found"

func TestClassifyTransientNeo4jErrorPrioritizesNornicDBWriteConflict(t *testing.T) {
	t.Parallel()

	err := &neo4jdriver.Neo4jError{
		Code: nornicDBTransactionOutdatedCode,
		Msg:  "UNWIND MERGE chain relationship update failed: conflict: edge nornic:123 changed after transaction start",
	}
	if got := classifyTransientNeo4jError(err); got != graphWriteRetryReasonWriteConflict {
		t.Fatalf("retry reason = %q, want %q", got, graphWriteRetryReasonWriteConflict)
	}
}

type relationshipSnapshotConflictExecutor struct {
	executeCalls    atomic.Int32
	executeFailures int32
	groupCalls      atomic.Int32
	groupFailures   int32
	err             error
}

func (e *relationshipSnapshotConflictExecutor) Execute(_ context.Context, _ Statement) error {
	call := e.executeCalls.Add(1)
	if call <= e.executeFailures {
		return e.err
	}
	return nil
}

func (e *relationshipSnapshotConflictExecutor) ExecuteGroup(_ context.Context, _ []Statement) error {
	call := e.groupCalls.Add(1)
	if call <= e.groupFailures {
		return e.err
	}
	return nil
}

func TestRetryingExecutorRetriesNornicDBRelationshipSnapshotConflict(t *testing.T) {
	t.Parallel()

	inner := &relationshipSnapshotConflictExecutor{
		executeFailures: 1,
		err:             typedRelationshipSnapshotConflict(),
	}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.Execute(context.Background(), relationshipMergeStatement("worker-a"))
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := inner.executeCalls.Load(), int32(2); got != want {
		t.Fatalf("Execute() calls = %d, want %d", got, want)
	}
}

func TestRetryingExecutorExecuteGroupRetriesNornicDBRelationshipSnapshotConflict(t *testing.T) {
	t.Parallel()

	inner := &relationshipSnapshotConflictExecutor{
		groupFailures: 1,
		err:           typedRelationshipSnapshotConflict(),
	}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.ExecuteGroup(
		context.Background(),
		[]Statement{relationshipMergeStatement("worker-a")},
	)
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after retry", err)
	}
	if got, want := inner.groupCalls.Load(), int32(2); got != want {
		t.Fatalf("ExecuteGroup() calls = %d, want %d", got, want)
	}
}

func TestRetryingExecutorDoesNotBroadenRelationshipSnapshotRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		statements []Statement
	}{
		{
			name: "non merge statement",
			err:  typedRelationshipSnapshotConflict(),
			statements: []Statement{{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MATCH (n:ContainerImage {digest: $digest}) SET n.scope_id = $scope_id",
			}},
		},
		{
			name: "mixed merge group",
			err:  typedRelationshipSnapshotConflict(),
			statements: []Statement{
				relationshipMergeStatement("worker-a"),
				{
					Operation: OperationCanonicalUpsert,
					Cypher:    "CREATE (n:UnsafeReplay {id: $id})",
				},
			},
		},
		{
			name: "wrong neo4j code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Statement.EntityNotFound",
				Msg:  observedNornicDBRelationshipSnapshotConflict,
			},
			statements: []Statement{relationshipMergeStatement("worker-a")},
		},
		{
			name: "near miss syntax error",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Statement.SyntaxError",
				Msg:  "UNWIND MERGE chain relationship create failed: not found",
			},
			statements: []Statement{relationshipMergeStatement("worker-a")},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inner := &relationshipSnapshotConflictExecutor{
				groupFailures: 10,
				err:           tc.err,
			}
			executor := &RetryingExecutor{
				Inner:      inner,
				MaxRetries: 2,
				BaseDelay:  time.Nanosecond,
			}

			err := executor.ExecuteGroup(context.Background(), tc.statements)
			if err == nil {
				t.Fatal("ExecuteGroup() error = nil, want terminal error")
			}
			if got, want := inner.groupCalls.Load(), int32(1); got != want {
				t.Fatalf("ExecuteGroup() calls = %d, want %d", got, want)
			}
		})
	}
}

func TestRetryingExecutorExecuteDoesNotBroadenRelationshipSnapshotRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		statement Statement
	}{
		{
			name: "non merge statement",
			err:  typedRelationshipSnapshotConflict(),
			statement: Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    "MATCH (n:ContainerImage {digest: $digest}) SET n.scope_id = $scope_id",
			},
		},
		{
			name: "wrong neo4j code",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Statement.EntityNotFound",
				Msg:  observedNornicDBRelationshipSnapshotConflict,
			},
			statement: relationshipMergeStatement("worker-a"),
		},
		{
			name: "near miss syntax error",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Statement.SyntaxError",
				Msg:  "UNWIND MERGE chain relationship create failed: not found",
			},
			statement: relationshipMergeStatement("worker-a"),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inner := &relationshipSnapshotConflictExecutor{
				executeFailures: 10,
				err:             tc.err,
			}
			executor := &RetryingExecutor{
				Inner:      inner,
				MaxRetries: 2,
				BaseDelay:  time.Nanosecond,
			}

			err := executor.Execute(context.Background(), tc.statement)
			if err == nil {
				t.Fatal("Execute() error = nil, want terminal error")
			}
			if got, want := inner.executeCalls.Load(), int32(1); got != want {
				t.Fatalf("Execute() calls = %d, want %d", got, want)
			}
		})
	}
}

func TestRetryingExecutorRelationshipSnapshotConflictExhaustionRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	inner := &relationshipSnapshotConflictExecutor{
		groupFailures: 10,
		err:           typedRelationshipSnapshotConflict(),
	}
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  time.Nanosecond,
	}

	err := executor.ExecuteGroup(
		context.Background(),
		[]Statement{relationshipMergeStatement("worker-a")},
	)
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want exhausted retry error")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("ExecuteGroup() error = %T %v, want queue-retryable error", err, err)
	}
	if got, want := inner.groupCalls.Load(), int32(3); got != want {
		t.Fatalf("ExecuteGroup() calls = %d, want %d", got, want)
	}
	if !strings.Contains(err.Error(), observedNornicDBRelationshipSnapshotConflict) {
		t.Fatalf("ExecuteGroup() error = %q, want original conflict context", err)
	}
}

type concurrentRelationshipSnapshotExecutor struct {
	firstAttempts *proofBarrier
	winnerChosen  atomic.Bool
	calls         atomic.Int32

	mu            sync.Mutex
	contributors  map[string]struct{}
	conflictCount int
	attempts      map[string]int
}

func newConcurrentRelationshipSnapshotExecutor() *concurrentRelationshipSnapshotExecutor {
	return &concurrentRelationshipSnapshotExecutor{
		firstAttempts: newProofBarrier(2),
		contributors:  make(map[string]struct{}),
		attempts:      make(map[string]int),
	}
}

func (e *concurrentRelationshipSnapshotExecutor) Execute(
	_ context.Context,
	_ Statement,
) error {
	return nil
}

func (e *concurrentRelationshipSnapshotExecutor) ExecuteGroup(
	_ context.Context,
	statements []Statement,
) error {
	e.calls.Add(1)
	writer, _ := statements[0].Parameters["writer"].(string)

	e.mu.Lock()
	e.attempts[writer]++
	attempt := e.attempts[writer]
	e.mu.Unlock()
	if attempt == 1 {
		e.firstAttempts.wait()
		if e.winnerChosen.Swap(true) {
			e.mu.Lock()
			e.conflictCount++
			e.mu.Unlock()
			return typedRelationshipSnapshotConflict()
		}
	}

	e.mu.Lock()
	e.contributors[writer] = struct{}{}
	e.mu.Unlock()
	return nil
}

func TestRetryingExecutorConvergesConcurrentRelationshipSnapshotConflict(t *testing.T) {
	t.Parallel()

	inner := newConcurrentRelationshipSnapshotExecutor()
	executor := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 2,
		BaseDelay:  time.Nanosecond,
	}

	writers := []string{"scope-a", "scope-b"}
	errs := make([]error, len(writers))
	var group sync.WaitGroup
	for i, writer := range writers {
		group.Add(1)
		go func(index int, name string) {
			defer group.Done()
			errs[index] = executor.ExecuteGroup(
				context.Background(),
				[]Statement{relationshipMergeStatement(name)},
			)
		}(i, writer)
	}
	group.Wait()

	for i, writer := range writers {
		if errs[i] != nil {
			t.Fatalf("writer %q error = %v, want nil", writer, errs[i])
		}
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if got, want := len(inner.contributors), len(writers); got != want {
		t.Fatalf("contributors = %d, want %d", got, want)
	}
	if got, want := inner.conflictCount, 1; got != want {
		t.Fatalf("conflictCount = %d, want %d", got, want)
	}
	if got, want := inner.calls.Load(), int32(3); got != want {
		t.Fatalf("ExecuteGroup() calls = %d, want %d", got, want)
	}
}

func BenchmarkRetryingExecutorExecuteGroupSuccess5767(b *testing.B) {
	rows := make([]map[string]any, DefaultBatchSize)
	for i := range rows {
		rows[i] = map[string]any{
			"digest":        "sha256:placeholder",
			"repository_id": "repo-synthetic",
		}
	}
	statement := relationshipMergeStatement("scope-a")
	statement.Parameters["rows"] = rows

	inner := &relationshipSnapshotConflictExecutor{}
	executor := &RetryingExecutor{Inner: inner}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := executor.ExecuteGroup(ctx, []Statement{statement}); err != nil {
			b.Fatal(err)
		}
	}
}

func typedRelationshipSnapshotConflict() error {
	return &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Statement.SyntaxError",
		Msg:  observedNornicDBRelationshipSnapshotConflict,
	}
}

func relationshipMergeStatement(writer string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher: `UNWIND $rows AS row
MATCH (img:ContainerImage {digest: row.digest})
MATCH (repo:Repository {id: row.repository_id})
MERGE (img)-[rel:BUILT_FROM]->(repo)
SET rel.scope_id = row.scope_id`,
		Parameters: map[string]any{
			"writer": writer,
			"rows": []map[string]any{{
				"digest":        "sha256:placeholder",
				"repository_id": "repo-synthetic",
			}},
		},
	}
}
