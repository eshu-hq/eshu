// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
)

const (
	// gaugeReducerClaimedExpired is the `gauge` label of the snapshot that
	// feeds eshu_dp_reducer_claimed_expired and
	// eshu_dp_reducer_claimed_expired_oldest_age_seconds (#10865).
	gaugeReducerClaimedExpired = "reducer_claimed_expired"

	claimedExpiredCountKey           = "count"
	claimedExpiredOldestAgeMillisKey = "oldest_age_millis"
)

// cachedReducerClaimedExpiredObserver serves the #10865 reclaim-backlog
// gauges from one snapshot. One refresh runs the single count-and-age
// statement, so the scrape never queries Postgres and the statement's cost
// is bounded by the refresh cadence, never by the claim rate. The age is
// stored as millis like the queue-age snapshots.
type cachedReducerClaimedExpiredObserver struct{ source *snapshot.Source }

func (c cachedReducerClaimedExpiredObserver) ReducerClaimedExpiredCounts(ctx context.Context) (int64, float64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, 0, err
	}
	if counts == nil {
		// A zero count means no reclaim backlog, so a pre-success zero
		// would lie.
		return 0, 0, errPostgresGaugeSnapshotNotReady
	}
	return counts[claimedExpiredCountKey], millisToSeconds(counts[claimedExpiredOldestAgeMillisKey]), nil
}

var _ telemetry.ReducerClaimedExpiredObserver = cachedReducerClaimedExpiredObserver{}

// registerReducerClaimedExpiredGauges registers the #10865 snapshot source
// fed by expiredObs and serves the reclaim-backlog gauges from it. A failed
// refresh publishes nothing, so the previous snapshot ages instead of a
// false empty backlog appearing.
func registerReducerClaimedExpiredGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	expiredObs telemetry.ReducerClaimedExpiredObserver,
) error {
	source, err := refresher.Register(gaugeReducerClaimedExpired, func(ctx context.Context) (map[string]int64, error) {
		count, age, err := expiredObs.ReducerClaimedExpiredCounts(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]int64{
			claimedExpiredCountKey:           count,
			claimedExpiredOldestAgeMillisKey: secondsToMillis(age),
		}, nil
	})
	if err != nil {
		return fmt.Errorf("register reducer claimed expired snapshot source: %w", err)
	}
	if err := telemetry.RegisterReducerClaimedExpiredObservableGauges(instruments, meter, cachedReducerClaimedExpiredObserver{source: source}); err != nil {
		return fmt.Errorf("register reducer claimed expired observable gauges: %w", err)
	}
	return nil
}
