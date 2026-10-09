// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"context"
	"time"
)

// PrefetchKind identifies which shared-projection prefetch recorded one
// observation. The values are the closed "kind" label set on the
// eshu_dp_shared_projection_prefetch_* instruments (#7724).
type PrefetchKind string

const (
	// PrefetchKindAcceptance is the accepted-generation prefetch over
	// shared_projection_acceptance.
	PrefetchKindAcceptance PrefetchKind = "acceptance"
	// PrefetchKindReadiness is the graph-projection-phase readiness
	// prefetch over graph_projection_phase_state, including the #7121
	// drain re-read, which is a readiness read served fresh.
	PrefetchKindReadiness PrefetchKind = "readiness"
)

// PrefetchKindStats accumulates one prefetch kind's query behavior over a
// single SelectPartitionBatch call: every widen round, plus the #7121
// drain re-read for readiness.
type PrefetchKindStats struct {
	// Keys is the distinct normalized keys submitted to the store. For
	// readiness this counts only keys actually queried; keys served from
	// the cross-round cache land in CacheHits instead.
	Keys int
	// Queries is the SQL queries issued. Batched prefetches issue at most
	// ceil(Keys/1000).
	Queries int
	// Rows is the rows the store returned (found keys).
	Rows int
	// CacheHits is the answers served from the cross-round readiness
	// cache instead of the store. Always zero for acceptance, which is
	// re-queried fresh every round (#7724: a stale cached acceptance can
	// complete a live row as stale, losing its edge permanently).
	CacheHits int
	// Duration is the total store time across the recorded calls.
	Duration time.Duration
}

// PrefetchStats accumulates one SelectPartitionBatch call's prefetch
// behavior for operator telemetry (#7724).
//
// Single-goroutine ownership: the selection call that creates the instance
// is its only writer. Prefetch closures run synchronously inside that
// selection, and concurrent runner workers each own a separate selection,
// so no mutex guards these counters. Do not share one instance across
// goroutines.
type PrefetchStats struct {
	Acceptance PrefetchKindStats
	Readiness  PrefetchKindStats
}

// Add records one prefetch call's observation into the matching kind's
// totals. An unknown kind is dropped so a mistyped caller cannot corrupt
// the closed kind set.
func (s *PrefetchStats) Add(kind PrefetchKind, keys, queries, rows, hits int, d time.Duration) {
	if s == nil {
		return
	}
	var slot *PrefetchKindStats
	switch kind {
	case PrefetchKindAcceptance:
		slot = &s.Acceptance
	case PrefetchKindReadiness:
		slot = &s.Readiness
	default:
		return
	}
	slot.Keys += keys
	slot.Queries += queries
	slot.Rows += rows
	slot.CacheHits += hits
	slot.Duration += d
}

// prefetchStatsContextKey carries a *PrefetchStats through prefetch calls
// whose signatures are frozen (#7724 1A: no prefetch signature changes).
type prefetchStatsContextKey struct{}

// ContextWithPrefetchStats returns a context that carries stats for
// RecordPrefetch. A nil stats is stored as-is and records as a no-op.
func ContextWithPrefetchStats(ctx context.Context, stats *PrefetchStats) context.Context {
	return context.WithValue(ctx, prefetchStatsContextKey{}, stats)
}

// PrefetchStatsFrom returns the *PrefetchStats carried by ctx, or nil when
// the caller attached none (for example direct prefetch users outside
// SelectPartitionBatch).
func PrefetchStatsFrom(ctx context.Context) *PrefetchStats {
	stats, _ := ctx.Value(prefetchStatsContextKey{}).(*PrefetchStats)
	return stats
}

// RecordPrefetch adds one prefetch call's observation to the stats carried
// by ctx. It is a no-op when ctx carries none, so storage prefetches keep
// their behavior for direct callers that attach no recorder.
func RecordPrefetch(ctx context.Context, kind PrefetchKind, keys, queries, rows, hits int, d time.Duration) {
	PrefetchStatsFrom(ctx).Add(kind, keys, queries, rows, hits, d)
}
