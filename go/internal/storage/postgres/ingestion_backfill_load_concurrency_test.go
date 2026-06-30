// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

// chunkProbeQueryer records the repo_id arm width for each deferred fact-load
// query. It returns the same fact from every chunk so the caller must de-duplicate
// merged rows by fact_id before discovery builds its content index.
type chunkProbeQueryer struct {
	mu                sync.Mutex
	calls             int
	maxRepoIDArgs     int
	nonRepoIDArgCalls int
}

func (q *chunkProbeQueryer) QueryContext(
	_ context.Context, query string, args ...any,
) (Rows, error) {
	if query != listDeferredScopedRelationshipFactRecordsQuery {
		return &queueFakeRows{}, nil
	}
	q.mu.Lock()
	q.calls++
	call := q.calls
	if len(args) >= 1 {
		if nonRepoID, ok := args[0].(pq.StringArray); ok && len(nonRepoID) > 0 {
			q.nonRepoIDArgCalls++
		}
	}
	if len(args) >= 2 {
		if repoIDs, ok := args[1].(pq.StringArray); ok && len(repoIDs) > q.maxRepoIDArgs {
			q.maxRepoIDArgs = len(repoIDs)
		}
	}
	q.mu.Unlock()

	return &queueFakeRows{rows: [][]any{
		contentFactRow(
			"fact-duplicate",
			"scope-large",
			"gen-large",
			"content",
			`{"repo_id":"repo-large","artifact_type":"terraform","relative_path":"main.tf","content":"target = repo-000001"}`,
		),
		contentFactRow(
			"fact-unique-"+itoa(call),
			"scope-large",
			"gen-large",
			"content",
			`{"repo_id":"repo-large","artifact_type":"terraform","relative_path":"unique.tf","content":"target = repo-000001"}`,
		),
	}}, nil
}

// TestLoadDeferredScopedFactsChunksRepoIDArm is the #4257 perf-shape regression:
// one huge scope must not issue one giant self-exclusion query with the whole
// catalog repo_id arm. The repo_id arm is split into bounded chunks that share
// the existing worker pool, and duplicate fact rows from multiple chunks are
// merged by fact_id so discovery sees the same fact set as the old union query.
func TestLoadDeferredScopedFactsChunksRepoIDArm(t *testing.T) {
	t.Parallel()

	const maxRepoIDsPerQuery = 128
	repoIDs := make([]string, 0, maxRepoIDsPerQuery*2+1)
	for i := 0; i < maxRepoIDsPerQuery*2+1; i++ {
		repoIDs = append(repoIDs, "repo-"+itoa(i))
	}
	params := deferredScopedFactQueryParams{
		nonRepoIDLike: pq.StringArray{"%external-config%"},
		repoIDValues:  pq.StringArray(repoIDs),
	}
	probe := &chunkProbeQueryer{}
	store := NewIngestionStore(nil)
	store.maintenanceWorkers = 4

	loaded, err := store.loadDeferredScopedFactsAcrossPartitions(
		context.Background(),
		probe,
		params,
		[]scopeGenerationPartition{{ScopeID: "scope-large", GenerationID: "gen-large"}},
		nil,
	)
	if err != nil {
		t.Fatalf("loadDeferredScopedFactsAcrossPartitions error = %v, want nil", err)
	}
	if probe.calls != 3 {
		t.Fatalf("issued %d fact-load queries, want 3 bounded chunks", probe.calls)
	}
	if probe.maxRepoIDArgs > maxRepoIDsPerQuery {
		t.Fatalf("max repo_id args per query = %d, want <= %d", probe.maxRepoIDArgs, maxRepoIDsPerQuery)
	}
	if probe.nonRepoIDArgCalls != 1 {
		t.Fatalf("non-repo_id LIKE arm ran in %d chunks, want 1", probe.nonRepoIDArgCalls)
	}
	if len(loaded) != 4 {
		t.Fatalf("loaded %d facts, want 3 unique chunk facts plus one de-duplicated shared fact", len(loaded))
	}
	factIDs := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		factIDs = append(factIDs, envelope.FactID)
	}
	sort.Strings(factIDs)
	wantFactIDs := []string{"fact-duplicate", "fact-unique-1", "fact-unique-2", "fact-unique-3"}
	for i, want := range wantFactIDs {
		if factIDs[i] != want {
			t.Fatalf("factIDs = %v, want %v", factIDs, wantFactIDs)
		}
	}
}

// TestLoadDeferredScopedFactsCanceledContextReturnsError pins that cancellation
// cannot produce a partial fact set with a nil error. Deferred backfill publishes
// readiness after discovery, so a canceled fact-load pass must fail closed before
// any partially loaded chunks can reach DiscoverEvidence.
func TestLoadDeferredScopedFactsCanceledContextReturnsError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := &chunkProbeQueryer{}
	store := NewIngestionStore(nil)
	store.maintenanceWorkers = 1

	loaded, err := store.loadDeferredScopedFactsAcrossPartitions(
		ctx,
		probe,
		deferredScopedFactQueryParams{
			nonRepoIDLike: pq.StringArray{"%external-config%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b"},
		},
		[]scopeGenerationPartition{{ScopeID: "scope-large", GenerationID: "gen-large"}},
		nil,
	)
	if err == nil {
		t.Fatal("loadDeferredScopedFactsAcrossPartitions error = nil, want cancellation error")
	}
	if loaded != nil {
		t.Fatalf("loaded = %v, want nil when context is canceled", loaded)
	}
	if probe.calls != 0 {
		t.Fatalf("issued %d queries after canceled context, want 0", probe.calls)
	}
}

func BenchmarkMergeDeferredScopedTaskFactsManyTasks(b *testing.B) {
	const (
		taskCount     = 256
		factsPerTask  = 64
		duplicateID   = "fact-duplicate"
		expectedFacts = taskCount*factsPerTask + 1
	)
	perTask := make([][]facts.Envelope, 0, taskCount)
	for taskIndex := 0; taskIndex < taskCount; taskIndex++ {
		taskFacts := make([]facts.Envelope, 0, factsPerTask+1)
		taskFacts = append(taskFacts, facts.Envelope{FactID: duplicateID})
		for factIndex := 0; factIndex < factsPerTask; factIndex++ {
			taskFacts = append(taskFacts, facts.Envelope{
				FactID: "fact-" + itoa(taskIndex) + "-" + itoa(factIndex),
			})
		}
		perTask = append(perTask, taskFacts)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		merged := mergeDeferredScopedTaskFacts(perTask)
		if len(merged) != expectedFacts {
			b.Fatalf("merged %d facts, want %d", len(merged), expectedFacts)
		}
	}
}
