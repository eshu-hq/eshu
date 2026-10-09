// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"context"
	"testing"
	"time"
)

func TestPrefetchStatsAddAccumulatesPerKind(t *testing.T) {
	t.Parallel()

	var stats PrefetchStats
	stats.Add(PrefetchKindAcceptance, 10, 1, 8, 0, 2*time.Millisecond)
	stats.Add(PrefetchKindAcceptance, 5, 1, 5, 0, time.Millisecond)
	stats.Add(PrefetchKindReadiness, 7, 1, 0, 3, 500*time.Microsecond)

	if stats.Acceptance.Keys != 15 || stats.Acceptance.Queries != 2 || stats.Acceptance.Rows != 13 {
		t.Fatalf("acceptance = %+v, want keys=15 queries=2 rows=13", stats.Acceptance)
	}
	if stats.Acceptance.Duration != 3*time.Millisecond {
		t.Fatalf("acceptance duration = %v, want 3ms", stats.Acceptance.Duration)
	}
	if stats.Readiness.Keys != 7 || stats.Readiness.CacheHits != 3 {
		t.Fatalf("readiness = %+v, want keys=7 cacheHits=3", stats.Readiness)
	}
}

func TestPrefetchStatsAddDropsUnknownKind(t *testing.T) {
	t.Parallel()

	var stats PrefetchStats
	stats.Add(PrefetchKind("drain"), 10, 1, 8, 0, time.Millisecond)

	if stats.Acceptance != (PrefetchKindStats{}) || stats.Readiness != (PrefetchKindStats{}) {
		t.Fatalf("unknown kind must not record, got %+v", stats)
	}
}

func TestRecordPrefetchNoOpWithoutStats(t *testing.T) {
	t.Parallel()

	// Must not panic when the caller attached no recorder (direct
	// prefetch users outside SelectPartitionBatch).
	RecordPrefetch(context.Background(), PrefetchKindAcceptance, 1, 1, 1, 0, time.Millisecond)
	if got := PrefetchStatsFrom(context.Background()); got != nil {
		t.Fatalf("PrefetchStatsFrom without stats = %v, want nil", got)
	}
}

func TestRecordPrefetchWritesAttachedStats(t *testing.T) {
	t.Parallel()

	var stats PrefetchStats
	ctx := ContextWithPrefetchStats(context.Background(), &stats)
	RecordPrefetch(ctx, PrefetchKindReadiness, 4, 1, 2, 1, time.Millisecond)

	if stats.Readiness.Keys != 4 || stats.Readiness.Queries != 1 || stats.Readiness.Rows != 2 || stats.Readiness.CacheHits != 1 {
		t.Fatalf("readiness = %+v, want keys=4 queries=1 rows=2 hits=1", stats.Readiness)
	}
	if PrefetchStatsFrom(ctx) != &stats {
		t.Fatal("PrefetchStatsFrom did not return the attached stats")
	}
}
