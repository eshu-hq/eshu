// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWithRequestStagesAccumulates proves each stage sums its own
// observations and the stages never cross-talk: the Server-Timing trailer
// reads these totals, so a pg observation must not move the graph stage.
func TestWithRequestStagesAccumulates(t *testing.T) {
	t.Parallel()
	ctx, stages := WithRequestStages(context.Background())
	ObservePG(ctx, 150*time.Millisecond)
	ObservePG(ctx, 50*time.Millisecond)
	ObserveGraph(ctx, 25*time.Millisecond)
	if got := stages.PG(); got != 200*time.Millisecond {
		t.Fatalf("PG() = %v, want 200ms", got)
	}
	if got := stages.Graph(); got != 25*time.Millisecond {
		t.Fatalf("Graph() = %v, want 25ms", got)
	}
}

// TestObserveWithoutClockIsNoOp proves recording on a context without a
// stage clock is safe: store seams stay callable on paths the middleware
// never wraps.
func TestObserveWithoutClockIsNoOp(t *testing.T) {
	t.Parallel()
	ObservePG(context.Background(), time.Second)
	ObserveGraph(context.Background(), time.Second)
	ObservePG(context.TODO(), time.Second)
	var stages *RequestStages
	if got := stages.PG(); got != 0 {
		t.Fatalf("nil PG() = %v, want 0", got)
	}
	if got := stages.Graph(); got != 0 {
		t.Fatalf("nil Graph() = %v, want 0", got)
	}
}

// TestRequestStagesConcurrentSum proves the accumulation is race-safe and
// sums under concurrency: parallel store calls add, which is the defined
// overlap accounting behind the trailer's pg+graph-may-exceed-total rule.
func TestRequestStagesConcurrentSum(t *testing.T) {
	t.Parallel()
	ctx, stages := WithRequestStages(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				ObservePG(ctx, time.Millisecond)
			}
		}()
	}
	wg.Wait()
	if got := stages.PG(); got != 800*time.Millisecond {
		t.Fatalf("PG() = %v, want 800ms (8 goroutines x 100 x 1ms)", got)
	}
}
