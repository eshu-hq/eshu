// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
)

const (
	// gaugeCorpusFence is the `gauge` label of the snapshot that feeds
	// eshu_dp_corpus_fence_holders, eshu_dp_corpus_fence_deferred_consumers,
	// and eshu_dp_corpus_fence_oldest_deferred_age_seconds (#8247).
	gaugeCorpusFence = "reducer_corpus_fence"

	corpusFenceHoldersKey            = "holders"
	corpusFenceDeferredPrefix        = "deferred"
	corpusFenceOldestAgeMillisPrefix = "oldest_age_millis"
)

// cachedCorpusFenceObserver serves the #8247 fence gauges from one snapshot.
// One refresh runs the single fence statement, so a scrape never queries
// Postgres and the statement's cost is bounded by the refresh cadence, never
// by the claim rate. Ages are stored as millis like the queue-age snapshots.
type cachedCorpusFenceObserver struct{ source *snapshot.Source }

func (c cachedCorpusFenceObserver) CorpusFenceCounts(ctx context.Context) (int64, map[string]int64, map[string]float64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, nil, nil, err
	}
	if counts == nil {
		// A zero holder count means the fence is open, so a pre-success
		// zero would lie.
		return 0, nil, nil, errPostgresGaugeSnapshotNotReady
	}
	deferred := make(map[string]int64)
	oldest := make(map[string]float64)
	for key, value := range counts {
		kind, domain, ok := strings.Cut(key, snapshotKeySep)
		if !ok {
			continue
		}
		switch kind {
		case corpusFenceDeferredPrefix:
			deferred[domain] = value
		case corpusFenceOldestAgeMillisPrefix:
			oldest[domain] = millisToSeconds(value)
		}
	}
	return counts[corpusFenceHoldersKey], deferred, oldest, nil
}

var _ telemetry.CorpusFenceObserver = cachedCorpusFenceObserver{}

// registerCorpusFenceGauges registers the #8247 snapshot source fed by
// fenceObs and serves the fence gauges from it. A failed refresh publishes
// nothing, so the previous snapshot ages instead of a false open fence
// appearing.
func registerCorpusFenceGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	fenceObs telemetry.CorpusFenceObserver,
) error {
	source, err := refresher.Register(gaugeCorpusFence, func(ctx context.Context) (map[string]int64, error) {
		holders, deferred, oldest, err := fenceObs.CorpusFenceCounts(ctx)
		if err != nil {
			return nil, err
		}
		flat := make(map[string]int64, 1+len(deferred)+len(oldest))
		flat[corpusFenceHoldersKey] = holders
		for domain, count := range deferred {
			flat[corpusFenceDeferredPrefix+snapshotKeySep+domain] = count
		}
		for domain, age := range oldest {
			flat[corpusFenceOldestAgeMillisPrefix+snapshotKeySep+domain] = secondsToMillis(age)
		}
		return flat, nil
	})
	if err != nil {
		return fmt.Errorf("register corpus fence snapshot source: %w", err)
	}
	if err := telemetry.RegisterCorpusFenceObservableGauges(instruments, meter, cachedCorpusFenceObserver{source: source}); err != nil {
		return fmt.Errorf("register corpus fence observable gauges: %w", err)
	}
	return nil
}
