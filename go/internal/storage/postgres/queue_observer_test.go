// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQueueObserverStoreQueueDepths(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "pending", int64(5)},
					{"projector", "claimed", int64(2)},
					{"projector", "running", int64(1)},
					{"projector", "retrying", int64(3)},
					{"reducer", "pending", int64(10)},
					{"reducer", "claimed", int64(4)},
				},
			},
			{
				rows: [][]any{
					{"semantic_extraction", "pending", int64(2)},
					{"semantic_extraction", "claimed", int64(1)},
					{"semantic_extraction", "retrying", int64(1)},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}

	// projector: pending=5, in_flight=3 (claimed+running merged), retrying=3
	if depths["projector"]["pending"] != 5 {
		t.Fatalf("projector pending = %d, want 5", depths["projector"]["pending"])
	}
	if depths["projector"]["in_flight"] != 3 {
		t.Fatalf("projector in_flight = %d, want 3 (claimed 2 + running 1)", depths["projector"]["in_flight"])
	}
	if depths["projector"]["retrying"] != 3 {
		t.Fatalf("projector retrying = %d, want 3", depths["projector"]["retrying"])
	}

	// reducer: pending=10, in_flight=4
	if depths["reducer"]["pending"] != 10 {
		t.Fatalf("reducer pending = %d, want 10", depths["reducer"]["pending"])
	}
	if depths["reducer"]["in_flight"] != 4 {
		t.Fatalf("reducer in_flight = %d, want 4", depths["reducer"]["in_flight"])
	}
	if got, want := depths["semantic_extraction"]["pending"], int64(2); got != want {
		t.Fatalf("semantic_extraction pending = %d, want %d", got, want)
	}
	if got, want := depths["semantic_extraction"]["in_flight"], int64(1); got != want {
		t.Fatalf("semantic_extraction in_flight = %d, want %d", got, want)
	}
	if got, want := depths["semantic_extraction"]["retrying"], int64(1); got != want {
		t.Fatalf("semantic_extraction retrying = %d, want %d", got, want)
	}
}

func TestQueueObserverStoreQueueDepthsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}
	if len(depths) != 0 {
		t.Fatalf("QueueDepths() = %v, want empty map", depths)
	}
}

func TestQueueObserverStoreQueueDepthsMergesClaimedAndRunning(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "claimed", int64(7)},
					{"projector", "running", int64(3)},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}

	if depths["projector"]["in_flight"] != 10 {
		t.Fatalf("in_flight = %d, want 10 (claimed 7 + running 3)", depths["projector"]["in_flight"])
	}
	if _, has := depths["projector"]["claimed"]; has {
		t.Fatal("claimed status should be merged into in_flight, not present separately")
	}
	if _, has := depths["projector"]["running"]; has {
		t.Fatal("running status should be merged into in_flight, not present separately")
	}
}

func TestQueueObserverStoreSourceQueueDepths(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "git", "pending", int64(5)},
					{"projector", "git", "claimed", int64(2)},
					{"projector", "git", "running", int64(1)},
					{"reducer", "aws", "retrying", int64(3)},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.SourceQueueDepths(context.Background())
	if err != nil {
		t.Fatalf("SourceQueueDepths() error = %v", err)
	}

	if got, want := depths["projector"]["git"]["pending"], int64(5); got != want {
		t.Fatalf("projector git pending = %d, want %d", got, want)
	}
	if got, want := depths["projector"]["git"]["in_flight"], int64(3); got != want {
		t.Fatalf("projector git in_flight = %d, want %d", got, want)
	}
	if _, has := depths["projector"]["git"]["claimed"]; has {
		t.Fatal("claimed status should be merged into source in_flight")
	}
	if _, has := depths["projector"]["git"]["running"]; has {
		t.Fatal("running status should be merged into source in_flight")
	}
	if got, want := depths["reducer"]["aws"]["retrying"], int64(3); got != want {
		t.Fatalf("reducer aws retrying = %d, want %d", got, want)
	}
}

func TestQueueObserverStoreSourceQueueOldestAge(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "git", 120.5},
					{"reducer", "aws", 45.0},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	ages, err := observer.SourceQueueOldestAge(context.Background())
	if err != nil {
		t.Fatalf("SourceQueueOldestAge() error = %v", err)
	}

	if got, want := ages["projector"]["git"], 120.5; got != want {
		t.Fatalf("projector git age = %f, want %f", got, want)
	}
	if got, want := ages["reducer"]["aws"], 45.0; got != want {
		t.Fatalf("reducer aws age = %f, want %f", got, want)
	}
}

func TestQueueObserverQueriesExcludeInactiveReducerGenerations(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"queueDepthQuery":     queueDepthQuery,
		"queueOldestAgeQuery": queueOldestAgeQuery,
	} {
		for _, want := range []string{
			"active_fact_work_items AS (",
			"FROM fact_work_items AS work",
			"JOIN ingestion_scopes AS scope",
			"scope.active_generation_id = active_generation.generation_id",
			"work.stage = 'reducer'",
			"work.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
			"stale_generation.ingested_at < active_generation.ingested_at",
			"stale_generation.generation_id < active_generation.generation_id",
			"FROM active_fact_work_items",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s missing inactive-generation observer predicate %q:\n%s", name, want, query)
			}
		}
	}
}

func TestSourceQueueObserverQueriesUseBoundedSourceSystem(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"sourceQueueDepthQuery":     sourceQueueDepthQuery,
		"sourceQueueOldestAgeQuery": sourceQueueOldestAgeQuery,
	} {
		for _, want := range []string{
			"active_fact_work_items AS (",
			"JOIN ingestion_scopes AS scope",
			"scope.source_system",
			"work.payload->>'source_system'",
			"GROUP BY work.stage, source_system",
			"ORDER BY work.stage, source_system",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s missing source observer predicate %q:\n%s", name, want, query)
			}
		}
	}
}

func TestQueueObserverStoreQueueOldestAge(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", 120.5},
					{"reducer", 45.0},
				},
			},
			{
				rows: [][]any{
					{"semantic_extraction", 30.0},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	ages, err := observer.QueueOldestAge(context.Background())
	if err != nil {
		t.Fatalf("QueueOldestAge() error = %v", err)
	}

	if ages["projector"] != 120.5 {
		t.Fatalf("projector age = %f, want 120.5", ages["projector"])
	}
	if ages["reducer"] != 45.0 {
		t.Fatalf("reducer age = %f, want 45.0", ages["reducer"])
	}
	if ages["semantic_extraction"] != 30.0 {
		t.Fatalf("semantic_extraction age = %f, want 30.0", ages["semantic_extraction"])
	}
}

func TestQueueObserverStoreQueueOldestAgeEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	ages, err := observer.QueueOldestAge(context.Background())
	if err != nil {
		t.Fatalf("QueueOldestAge() error = %v", err)
	}
	if len(ages) != 0 {
		t.Fatalf("QueueOldestAge() = %v, want empty map", ages)
	}
}

func TestQueueObserverStoreNilQueryer(t *testing.T) {
	t.Parallel()

	observer := &QueueObserverStore{}

	_, err := observer.QueueDepths(context.Background())
	if err == nil {
		t.Fatal("QueueDepths() error = nil, want non-nil for nil queryer")
	}

	_, err = observer.QueueOldestAge(context.Background())
	if err == nil {
		t.Fatal("QueueOldestAge() error = nil, want non-nil for nil queryer")
	}

	_, err = observer.SourceQueueDepths(context.Background())
	if err == nil {
		t.Fatal("SourceQueueDepths() error = nil, want non-nil for nil queryer")
	}

	_, err = observer.SourceQueueOldestAge(context.Background())
	if err == nil {
		t.Fatal("SourceQueueOldestAge() error = nil, want non-nil for nil queryer")
	}
}

func TestQueueObserverStoreQueueDepthsQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("connection lost")},
		},
	}

	observer := NewQueueObserverStore(queryer)
	_, err := observer.QueueDepths(context.Background())
	if err == nil {
		t.Fatal("QueueDepths() error = nil, want non-nil")
	}
}

func TestQueueObserverStoreQueueOldestAgeQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("connection lost")},
		},
	}

	observer := NewQueueObserverStore(queryer)
	_, err := observer.QueueOldestAge(context.Background())
	if err == nil {
		t.Fatal("QueueOldestAge() error = nil, want non-nil")
	}
}
