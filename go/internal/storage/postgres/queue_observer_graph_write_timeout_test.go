package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestReducerGraphWriteTimeoutDepthFiltersFailureClass proves the
// graph-write-timeout depth read is scoped to retrying reducer rows whose
// durable failure_class is graph_write_timeout. Readiness-not-ready retrying
// rows (secrets_iam_endpoint_not_ready and other *_n classes) are excluded by
// the query, so the producer backpressure gate that consumes this count never
// false-throttles on a readiness backlog (#3560 P2).
func TestReducerGraphWriteTimeoutDepthFiltersFailureClass(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{int64(42)}}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depth, err := observer.ReducerGraphWriteTimeoutDepth(context.Background())
	if err != nil {
		t.Fatalf("ReducerGraphWriteTimeoutDepth() error = %v", err)
	}
	if depth != 42 {
		t.Fatalf("ReducerGraphWriteTimeoutDepth() = %d, want 42", depth)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(queryer.queries))
	}
	query := queryer.queries[0]
	for _, want := range []string{
		"stage = 'reducer'",
		"status = 'retrying'",
		"failure_class = 'graph_write_timeout'",
		"FROM active_fact_work_items",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("graph-write-timeout depth query missing %q:\n%s", want, query)
		}
	}
	// Readiness-not-ready classes must never be counted by this gate.
	for _, banned := range []string{
		"secrets_iam_endpoint_not_ready",
		"reducer_retryable",
		"_n'",
	} {
		if strings.Contains(query, banned) {
			t.Fatalf("graph-write-timeout depth query must not reference %q:\n%s", banned, query)
		}
	}
}

// TestReducerGraphWriteTimeoutDepthExcludesInactiveGenerations proves the
// graph-write-timeout depth read reuses the active-generation CTE so superseded
// stale-generation rows never inflate the backpressure signal.
func TestReducerGraphWriteTimeoutDepthExcludesInactiveGenerations(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"active_fact_work_items AS (",
		"FROM fact_work_items AS work",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = active_generation.generation_id",
	} {
		if !strings.Contains(reducerGraphWriteTimeoutDepthQuery, want) {
			t.Fatalf("graph-write-timeout depth query missing inactive-generation predicate %q:\n%s",
				want, reducerGraphWriteTimeoutDepthQuery)
		}
	}
}

// TestReducerGraphWriteTimeoutDepthEmpty proves an empty result reports zero
// depth, so a healthy backend never throttles the producer.
func TestReducerGraphWriteTimeoutDepthEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	observer := NewQueueObserverStore(queryer)
	depth, err := observer.ReducerGraphWriteTimeoutDepth(context.Background())
	if err != nil {
		t.Fatalf("ReducerGraphWriteTimeoutDepth() error = %v", err)
	}
	if depth != 0 {
		t.Fatalf("ReducerGraphWriteTimeoutDepth() = %d, want 0", depth)
	}
}

// TestReducerGraphWriteTimeoutDepthNilQueryer proves the nil-queryer guard.
func TestReducerGraphWriteTimeoutDepthNilQueryer(t *testing.T) {
	t.Parallel()

	observer := &QueueObserverStore{}
	if _, err := observer.ReducerGraphWriteTimeoutDepth(context.Background()); err == nil {
		t.Fatal("ReducerGraphWriteTimeoutDepth() error = nil, want non-nil for nil queryer")
	}
}

// TestReducerGraphWriteTimeoutDepthQueryError proves query failures propagate.
func TestReducerGraphWriteTimeoutDepthQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{err: errors.New("connection lost")}}}
	observer := NewQueueObserverStore(queryer)
	if _, err := observer.ReducerGraphWriteTimeoutDepth(context.Background()); err == nil {
		t.Fatal("ReducerGraphWriteTimeoutDepth() error = nil, want non-nil")
	}
}
