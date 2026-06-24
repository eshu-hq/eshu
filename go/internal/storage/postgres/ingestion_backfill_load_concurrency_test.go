package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// partitionLoadProbeQueryer records the peak number of simultaneously in-flight
// per-scope deferred fact-load queries so the load-path concurrency test can
// assert the fan-out stays within the worker bound and runs concurrently. It
// mirrors the write-path concurrencyProbeDB (ingestion_backfill_concurrency_test.go)
// but for the read fan-out in loadDeferredScopedFactsAcrossPartitions (#3710).
type partitionLoadProbeQueryer struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	calls    int
}

func (q *partitionLoadProbeQueryer) QueryContext(
	_ context.Context, query string, _ ...any,
) (Rows, error) {
	if query != listDeferredScopedRelationshipFactRecordsQuery {
		return &queueFakeRows{}, nil
	}

	q.mu.Lock()
	q.inFlight++
	q.calls++
	if q.inFlight > q.peak {
		q.peak = q.inFlight
	}
	q.mu.Unlock()

	// Hold the query open briefly so overlapping partitions actually overlap in
	// wall time, making the peak measurement meaningful.
	time.Sleep(time.Millisecond)

	q.mu.Lock()
	q.inFlight--
	q.mu.Unlock()

	return &queueFakeRows{}, nil
}

func makePartitions(n int) []scopeGenerationPartition {
	parts := make([]scopeGenerationPartition, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, scopeGenerationPartition{
			ScopeID:      "scope-" + itoa(i),
			GenerationID: "gen-" + itoa(i),
		})
	}
	return parts
}

// TestLoadDeferredScopedFactsFanOutRunsConcurrently is the #3710 load-path
// concurrency gate (mirrors TestWriteDeferredBackfillInBatchesRunsConcurrently for
// the write path). With several scope partitions and a worker count above one, the
// per-scope fact-load queries must run concurrently (peak in-flight > 1) yet stay
// bounded by the worker count, and every partition must be queried.
func TestLoadDeferredScopedFactsFanOutRunsConcurrently(t *testing.T) {
	t.Parallel()

	probe := &partitionLoadProbeQueryer{}
	store := NewIngestionStore(nil)
	store.maintenanceWorkers = 6

	partitions := makePartitions(32)
	_, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(),
		probe,
		deferredScopedFactQueryParams{},
		partitions,
		nil,
	)
	if err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions error = %v, want nil", err)
	}
	if probe.calls != len(partitions) {
		t.Fatalf("issued %d per-scope queries, want %d (every partition queried)", probe.calls, len(partitions))
	}
	if probe.peak <= 1 {
		t.Fatalf("peak concurrent per-scope queries = %d, want > 1 (the load must fan out)", probe.peak)
	}
	if probe.peak > store.maintenanceWorkers {
		t.Fatalf("peak concurrent per-scope queries = %d, exceeds worker bound %d", probe.peak, store.maintenanceWorkers)
	}
}

// TestLoadDeferredScopedFactsFanOutSerialWhenWorkerCountOne pins that a worker
// count of one keeps the per-scope load single-flight (peak == 1), the path a
// deployment pinned to ESHU_POSTGRES_MAX_OPEN_CONNS=1 takes.
func TestLoadDeferredScopedFactsFanOutSerialWhenWorkerCountOne(t *testing.T) {
	t.Parallel()

	probe := &partitionLoadProbeQueryer{}
	store := NewIngestionStore(nil)
	store.maintenanceWorkers = 1

	if _, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(),
		probe,
		deferredScopedFactQueryParams{},
		makePartitions(8),
		nil,
	); err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions error = %v, want nil", err)
	}
	if probe.peak != 1 {
		t.Fatalf("peak concurrent per-scope queries = %d, want 1 for worker count 1", probe.peak)
	}
}

// TestLoadDeferredScopedFactsFanOutFirstErrorAborts pins that a per-scope query
// error latches the first error, cancels the remaining work through the shared
// context, and surfaces the wrapped failure (never a partial fact set).
func TestLoadDeferredScopedFactsFanOutFirstErrorAborts(t *testing.T) {
	t.Parallel()

	probe := &errLoadProbeQueryer{failAfter: 1}
	store := NewIngestionStore(nil)
	store.maintenanceWorkers = 4

	loaded, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(),
		probe,
		deferredScopedFactQueryParams{},
		makePartitions(16),
		nil,
	)
	if err == nil {
		t.Fatal("loadDeferredScopedFactsAcrossPartitions returned nil error, want the latched first error")
	}
	if loaded != nil {
		t.Fatalf("loaded = %v, want nil on first-error abort", loaded)
	}
}

// errLoadProbeQueryer returns an error from one per-scope query after failAfter
// successful calls, exercising the first-error abort latch.
type errLoadProbeQueryer struct {
	mu        sync.Mutex
	calls     int
	failAfter int
}

func (q *errLoadProbeQueryer) QueryContext(
	_ context.Context, query string, _ ...any,
) (Rows, error) {
	if query != listDeferredScopedRelationshipFactRecordsQuery {
		return &queueFakeRows{}, nil
	}
	q.mu.Lock()
	q.calls++
	shouldFail := q.calls > q.failAfter
	q.mu.Unlock()
	if shouldFail {
		return nil, errors.New("synthetic per-scope load failure")
	}
	return &queueFakeRows{}, nil
}
