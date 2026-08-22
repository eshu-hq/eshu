// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/sdk/metric"
)

const nornicDBPlatformCommitUniqueConflictMessage = "commit failed: constraint violation: " +
	"Constraint violation (UNIQUE on Platform.[id]): " +
	"Node with id=platform:kubernetes:none:prod:prod:none already exists"

type typedPlatformConflictExecutor struct {
	calls atomic.Int32
	err   error
}

type concurrentPlatformConflictExecutor struct {
	mu           sync.Mutex
	version      int
	created      int
	conflicts    int
	contributors map[string]struct{}
	seen         map[string]struct{}
	readBarrier  *proofBarrier
}

func newConcurrentPlatformConflictExecutor(writers int) *concurrentPlatformConflictExecutor {
	return &concurrentPlatformConflictExecutor{
		contributors: make(map[string]struct{}, writers),
		seen:         make(map[string]struct{}, writers),
		readBarrier:  newProofBarrier(writers),
	}
}

func (e *concurrentPlatformConflictExecutor) Execute(_ context.Context, stmt Statement) error {
	writer, _ := stmt.Parameters["writer"].(string)

	e.mu.Lock()
	_, retried := e.seen[writer]
	e.seen[writer] = struct{}{}
	snapshot := e.version
	e.mu.Unlock()

	if !retried {
		e.readBarrier.wait()
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if snapshot != e.version {
		e.conflicts++
		return &neo4jdriver.Neo4jError{
			Code: nornicDBStatementSyntaxErrorCode,
			Msg:  nornicDBPlatformCommitUniqueConflictMessage,
		}
	}
	if e.version == 0 {
		e.created++
	}
	e.contributors[writer] = struct{}{}
	e.version++
	return nil
}

func (e *typedPlatformConflictExecutor) Execute(context.Context, Statement) error {
	if e.calls.Add(1) == 1 {
		return e.err
	}
	return nil
}

func TestRetryingExecutorRetriesTypedNornicDBPlatformCommitUniqueConflict(t *testing.T) {
	t.Parallel()

	inner := &typedPlatformConflictExecutor{err: &neo4jdriver.Neo4jError{
		Code: nornicDBStatementSyntaxErrorCode,
		Msg:  nornicDBPlatformCommitUniqueConflictMessage,
	}}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}

	err := retrying.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher: `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
SET p.name = row.platform_name`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := inner.calls.Load(), int32(2); got != want {
		t.Fatalf("Execute() calls = %d, want %d", got, want)
	}
}

func TestNornicDBPlatformCommitUniqueConflictRetryStaysNarrow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		cypher string
	}{
		{
			name: "ordinary syntax error",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg:  "Invalid input 'BROKEN': expected a graph pattern",
			},
			cypher: "MERGE (p:Platform {id: $id}) BROKEN",
		},
		{
			name: "unique conflict without merge",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg:  nornicDBPlatformCommitUniqueConflictMessage,
			},
			cypher: "CREATE (p:Platform {id: $id})",
		},
		{
			name: "syntax error unique body without commit failure",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg: "constraint violation: Constraint violation (UNIQUE on Platform.[id]): " +
					"Node with id=platform:kubernetes:none:prod:prod:none already exists",
			},
			cypher: "MERGE (p:Platform {id: $id})",
		},
		{
			name: "commit failure without unique constraint",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg:  "commit failed: constraint violation: Platform node already exists",
			},
			cypher: "MERGE (p:Platform {id: $id})",
		},
		{
			name: "commit unique constraint without existing node",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg:  "commit failed: constraint violation: UNIQUE on Platform.[id]",
			},
			cypher: "MERGE (p:Platform {id: $id})",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRetryableGraphWriteError(tt.err, Statement{Cypher: tt.cypher}); got != "" {
				t.Fatalf("retry reason = %q, want terminal error", got)
			}
		})
	}
}

func TestLiveNornicDBOnCreateCommitUniqueConflictShapeRequiresSyntaxError(t *testing.T) {
	t.Parallel()

	const uid = "nornicdb-retry-contract"
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "exact syntax compatibility shape",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg: "commit failed: constraint violation: " +
					"Constraint violation (UNIQUE on NornicDBRetryContract.[uid]): " +
					"Node with uid=" + uid + " already exists",
			},
			want: true,
		},
		{
			name: "already supported transaction code",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBTransactionCommitFailedCode,
				Msg: "commit failed: constraint violation: " +
					"Constraint violation (UNIQUE on NornicDBRetryContract.[uid]): " +
					"Node with uid=" + uid + " already exists",
			},
			want: false,
		},
		{
			name: "wrong constrained label",
			err: &neo4jdriver.Neo4jError{
				Code: nornicDBStatementSyntaxErrorCode,
				Msg: "commit failed: constraint violation: " +
					"Constraint violation (UNIQUE on Other.[uid]): " +
					"Node with uid=" + uid + " already exists",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isLiveNornicDBOnCreateCommitUniqueConflict(tt.err, uid); got != tt.want {
				t.Fatalf("live compatibility shape = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRetryingExecutorConvergesConcurrentTypedPlatformCommitUniqueConflict(t *testing.T) {
	t.Parallel()

	graph := newConcurrentPlatformConflictExecutor(2)
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	retrying := &RetryingExecutor{
		Inner:       graph,
		MaxRetries:  1,
		BaseDelay:   time.Millisecond,
		Instruments: instruments,
	}

	writers := []string{"workload-target", "workload-multi-target"}
	errs := make([]error, len(writers))
	var wg sync.WaitGroup
	for i, writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = retrying.Execute(context.Background(), Statement{
				Operation: OperationCanonicalUpsert,
				Cypher: `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.name = row.platform_name`,
				Parameters: map[string]any{"writer": writer},
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %q error = %v, want nil", writers[i], err)
		}
	}
	graph.mu.Lock()
	created := graph.created
	contributorCount := len(graph.contributors)
	conflicts := graph.conflicts
	graph.mu.Unlock()
	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}
	if got, want := contributorCount, len(writers); got != want {
		t.Fatalf("contributors = %d, want %d", got, want)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1", conflicts)
	}
	if got := collectRetryCounter(t, reader); got != 1 {
		t.Fatalf("Neo4jDeadlockRetries counter = %d, want 1", got)
	}
	attrs := retryCounterAttributes(t, reader)
	if got, want := attrs[telemetry.MetricDimensionReason], graphWriteRetryReasonUniqueConflict; got != want {
		t.Fatalf("retry reason = %q, want %q", got, want)
	}
	if got, want := attrs[telemetry.MetricDimensionWritePhase], string(OperationCanonicalUpsert); got != want {
		t.Fatalf("write phase = %q, want %q", got, want)
	}
	for key, value := range attrs {
		if strings.Contains(value, "platform:kubernetes:none:prod:prod:none") ||
			strings.Contains(value, "workload-target") {
			t.Fatalf("metric attribute %q leaked conflict data %q", key, value)
		}
	}
}
