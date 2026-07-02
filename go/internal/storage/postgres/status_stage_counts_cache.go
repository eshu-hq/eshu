// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// statusStageCountsCacheTTL bounds how long a stage-counts read is served from
// the in-memory cache before the next call re-runs stageCountsQuery against
// Postgres. #4446 evidence: stageCountsQuery embeds activeFactWorkItemsCTE, a
// 4-way join over fact_work_items/ingestion_scopes/scope_generations that one
// ReadStatusSnapshotFiltered call and its sibling status/observer reads each
// re-plan and re-execute independently; on a large scope population this join
// is the dominant cost of a status read. 2s sits at the top of the issue's
// suggested 1-2s window: it is short enough that an operator polling
// /status/index never perceives materially stale stage counts, and it is the
// one activeFactWorkItemsCTE consumer whose result (StageStatusCount: stage,
// status, count) carries no asOf-relative field, so caching it introduces no
// staleness risk to any downstream consumer (contrast with domainBacklogQuery
// and queueSnapshotQuery, whose oldest-age/overdue-claim columns are computed
// relative to the caller-supplied asOf and must stay live).
const statusStageCountsCacheTTL = 2 * time.Second

// statusStageCountsCache is a short-TTL, thread-safe in-memory cache for the
// listStageCounts result. It follows the same pattern as
// internal/query/status_collector_readiness.go's collectorReadinessCache:
// a mutex-guarded single slot, zero value is a valid (always-cold) cache.
//
// It is held by pointer from StatusStore (see status.go) so that StatusStore,
// which is copied by value at call sites, never duplicates the mutex.
type statusStageCountsCache struct {
	mu      sync.Mutex
	warm    bool
	counts  []statuspkg.StageStatusCount
	expiry  time.Time
	nowFunc func() time.Time
}

// newStatusStageCountsCache constructs an empty, cold cache.
func newStatusStageCountsCache() *statusStageCountsCache {
	return &statusStageCountsCache{nowFunc: time.Now}
}

// get returns the cached stage counts and true when the cache is warm and
// still within its TTL. It returns (nil, false) on a cold or expired cache;
// the caller is expected to fall through to a fresh Postgres read.
func (c *statusStageCountsCache) get() ([]statuspkg.StageStatusCount, bool) {
	if c == nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.warm || c.nowFunc().After(c.expiry) {
		return nil, false
	}
	return c.counts, true
}

// set stores a freshly read stage-counts result and resets the TTL window.
// The caller must only call set after a successful read; a failed read must
// leave the cache untouched (see TestListStageCountsCachePropagatesQueryErrors)
// so a transient Postgres error never poisons subsequent reads with stale
// data for the remainder of the TTL.
func (c *statusStageCountsCache) set(counts []statuspkg.StageStatusCount) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts = counts
	c.warm = true
	c.expiry = c.nowFunc().Add(statusStageCountsCacheTTL)
}

// Bounded cache-outcome label values (closed metric label set) for
// eshu_dp_status_stage_counts_cache_total.
const (
	statusStageCountsCacheOutcomeHit   = "hit"
	statusStageCountsCacheOutcomeMiss  = "miss"
	statusStageCountsCacheOutcomeError = "error"
)

// recordStatusStageCountsCacheOutcome increments the
// StatusStageCountsCacheTotal counter registered in
// go/internal/telemetry/instruments.go. outcome must be one of the
// statusStageCountsCacheOutcome* constants. instruments may be nil (e.g. a
// StatusStore constructed without a telemetry.Instruments handle, or a
// registration error at startup), in which case recording is a no-op so a
// telemetry pipeline fault never fails a status read.
func recordStatusStageCountsCacheOutcome(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	if instruments == nil || instruments.StatusStageCountsCacheTotal == nil {
		return
	}
	instruments.StatusStageCountsCacheTotal.Add(
		ctx, 1,
		metric.WithAttributes(attribute.String("outcome", outcome)),
	)
}

// listStageCounts returns the reducer/projector stage x status counts,
// serving a cached copy when cache is non-nil and still within its TTL.
// stageCountsQuery embeds activeFactWorkItemsCTE, the #4446 4-way join the
// issue calls out as expensive at repo scale; StageStatusCount carries no
// asOf-relative fields, so short-TTL caching this specific read introduces no
// staleness risk to any consumer.
func listStageCounts(
	ctx context.Context,
	queryer Queryer,
	cache *statusStageCountsCache,
	instruments *telemetry.Instruments,
) ([]statuspkg.StageStatusCount, error) {
	if cache != nil {
		if counts, hit := cache.get(); hit {
			recordStatusStageCountsCacheOutcome(ctx, instruments, statusStageCountsCacheOutcomeHit)
			return counts, nil
		}
	}

	rows, err := queryer.QueryContext(ctx, stageCountsQuery)
	if err != nil {
		recordStatusStageCountsCacheOutcome(ctx, instruments, statusStageCountsCacheOutcomeError)
		return nil, fmt.Errorf("list stage counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := []statuspkg.StageStatusCount{}
	for rows.Next() {
		var stage string
		var state string
		var count int64
		if scanErr := rows.Scan(&stage, &state, &count); scanErr != nil {
			recordStatusStageCountsCacheOutcome(ctx, instruments, statusStageCountsCacheOutcomeError)
			return nil, fmt.Errorf("list stage counts: %w", scanErr)
		}
		counts = append(counts, statuspkg.StageStatusCount{
			Stage:  stage,
			Status: state,
			Count:  int(count),
		})
	}
	if err := rows.Err(); err != nil {
		recordStatusStageCountsCacheOutcome(ctx, instruments, statusStageCountsCacheOutcomeError)
		return nil, fmt.Errorf("list stage counts: %w", err)
	}

	if cache != nil {
		cache.set(counts)
	}
	recordStatusStageCountsCacheOutcome(ctx, instruments, statusStageCountsCacheOutcomeMiss)

	return counts, nil
}
