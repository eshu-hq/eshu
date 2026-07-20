// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// barrierReleaseTimeout bounds how long a faked writer waits for peer writers to
// arrive. A serialized regression never reaches the barrier count, so the wait
// elapses and the parallelism assertion fails instead of hanging the suite.
const barrierReleaseTimeout = 2 * time.Second

// TestCodeReachabilityProjectionRunnerPartitionsDisjointRepositoriesConcurrently
// proves the runner processes distinct (scope, generation, repository) conflict
// partitions concurrently while never overlapping two writers on the same
// partition.
func TestCodeReachabilityProjectionRunnerPartitionsDisjointRepositoriesConcurrently(t *testing.T) {
	t.Parallel()

	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("need at least 2 CPUs to observe concurrent partition projection")
	}

	const partitions = 4
	// The runner clamps fan-out to the usable (cgroup-aware) CPU count, so
	// the barrier must release at the effective concurrency, not the
	// partition count, or it would stall waiting for arrivals that can never
	// run at once.
	effective := partitions
	if n := runtime.GOMAXPROCS(0); effective > n {
		effective = n
	}
	inputs := make([]CodeReachabilityProjectionInput, 0, partitions)
	for i := 0; i < partitions; i++ {
		inputs = append(inputs, CodeReachabilityProjectionInput{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: fmt.Sprintf("repo-%d", i),
			Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
			Edges: []CodeReachabilityEdge{{
				SourceEntityID:   "entity:root",
				TargetEntityID:   "entity:leaf",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			}},
			UpdatedAt: time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC),
		})
	}

	writer := newBarrierCodeReachabilityRowWriter(effective)
	runner := CodeReachabilityProjectionRunner{
		InputLoader: &fakeCodeReachabilityInputLoader{inputs: inputs},
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10, Concurrency: partitions},
	}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if got, want := result.InputsProcessed, partitions; got != want {
		t.Fatalf("InputsProcessed = %d, want %d", got, want)
	}
	if got, want := result.RowsWritten, partitions*2; got != want {
		t.Fatalf("RowsWritten = %d, want %d", got, want)
	}
	if v := writer.overlaps(); v > 0 {
		t.Fatalf("same-partition writer overlaps = %d, want 0", v)
	}
	if got := writer.maxConcurrent(); got < 2 {
		t.Fatalf("max concurrent writers = %d, want >= 2 (disjoint partitions must parallelize)", got)
	}
}

// TestCodeReachabilityProjectionRunnerSerializesSamePartitionInputs proves that
// two inputs sharing one (scope, generation, repository) conflict key (e.g. two
// source runs) never run concurrently, so the per-repository DELETE+INSERT
// replacement cannot race or lose updates, and the newest watermark survives.
func TestCodeReachabilityProjectionRunnerSerializesSamePartitionInputs(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	input := func(watermark time.Time) CodeReachabilityProjectionInput {
		return CodeReachabilityProjectionInput{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: "repo-shared",
			Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
			Edges:        []CodeReachabilityEdge{{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"}},
			UpdatedAt:    watermark,
		}
	}

	writer := newBarrierCodeReachabilityRowWriter(1)
	runner := CodeReachabilityProjectionRunner{
		InputLoader: &fakeCodeReachabilityInputLoader{inputs: []CodeReachabilityProjectionInput{input(earlier), input(later)}},
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10, Concurrency: 8},
	}

	if _, err := runner.ProcessOnce(context.Background(), later); err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if v := writer.overlaps(); v > 0 {
		t.Fatalf("same-partition writer overlaps = %d, want 0", v)
	}
	if got := writer.lastWatermark("scope-1\x00generation-1\x00repo-shared"); !got.Equal(later) {
		t.Fatalf("final watermark = %v, want newest %v", got, later)
	}
}

// barrierCodeReachabilityRowWriter is a concurrency-safe fake writer that
// records per-partition overlap, peak concurrency, and the last watermark per
// partition. The barrier releases once `release` writers are in flight so a
// disjoint-partition test can prove the writes actually parallelized.
type barrierCodeReachabilityRowWriter struct {
	release int
	gate    chan struct{}
	once    sync.Once

	mu           sync.Mutex
	arrived      int
	inFlight     int
	maxInFlight  int
	overlapCount int
	activeByKey  map[string]int
	lastByKey    map[string]time.Time
}

func newBarrierCodeReachabilityRowWriter(release int) *barrierCodeReachabilityRowWriter {
	return &barrierCodeReachabilityRowWriter{
		release:     release,
		gate:        make(chan struct{}),
		activeByKey: make(map[string]int),
		lastByKey:   make(map[string]time.Time),
	}
}

func (w *barrierCodeReachabilityRowWriter) ReplaceRepositoryRows(
	_ context.Context,
	scopeID string,
	generationID string,
	repositoryID string,
	_ []CodeReachabilityRow,
	_ []CodeRootVerdictRow,
	watermark time.Time,
	_ bool,
) error {
	key := scopeID + "\x00" + generationID + "\x00" + repositoryID
	w.mu.Lock()
	w.activeByKey[key]++
	if w.activeByKey[key] > 1 {
		w.overlapCount++
	}
	w.inFlight++
	if w.inFlight > w.maxInFlight {
		w.maxInFlight = w.inFlight
	}
	w.arrived++
	if w.arrived >= w.release {
		w.once.Do(func() { close(w.gate) })
	}
	w.mu.Unlock()

	select {
	case <-w.gate:
	case <-time.After(barrierReleaseTimeout):
	}

	w.mu.Lock()
	w.lastByKey[key] = watermark
	w.activeByKey[key]--
	w.inFlight--
	w.mu.Unlock()
	return nil
}

func (w *barrierCodeReachabilityRowWriter) overlaps() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overlapCount
}

func (w *barrierCodeReachabilityRowWriter) maxConcurrent() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.maxInFlight
}

func (w *barrierCodeReachabilityRowWriter) lastWatermark(key string) time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastByKey[key]
}
