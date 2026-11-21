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
	// gaugeReducerLeaseOrphan is the `gauge` label of the snapshot that
	// feeds eshu_dp_reducer_lease_orphan_rows (#11081).
	gaugeReducerLeaseOrphan = "reducer_lease_orphan"

	leaseOrphanCountKey = "count"
)

// cachedReducerLeaseOrphanObserver serves the #11081 lease-orphan residue
// gauge from one snapshot. One refresh runs the orphan count statement, so
// the scrape never queries Postgres and the statement's cost is bounded by
// the refresh cadence, never by the claim rate.
type cachedReducerLeaseOrphanObserver struct{ source *snapshot.Source }

func (c cachedReducerLeaseOrphanObserver) ReducerLeaseOrphanRows(ctx context.Context) (int64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, err
	}
	if counts == nil {
		// A zero count means no orphan residue, so a pre-success zero
		// would lie.
		return 0, errPostgresGaugeSnapshotNotReady
	}
	return counts[leaseOrphanCountKey], nil
}

var _ telemetry.ReducerLeaseOrphanObserver = cachedReducerLeaseOrphanObserver{}

// registerReducerLeaseOrphanGauges registers the #11081 snapshot source fed
// by orphanObs and serves the lease-orphan residue gauge from it. A failed
// refresh publishes nothing, so the previous snapshot ages instead of a
// false empty residue appearing.
func registerReducerLeaseOrphanGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	orphanObs telemetry.ReducerLeaseOrphanObserver,
) error {
	source, err := refresher.Register(gaugeReducerLeaseOrphan, func(ctx context.Context) (map[string]int64, error) {
		count, err := orphanObs.ReducerLeaseOrphanRows(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]int64{
			leaseOrphanCountKey: count,
		}, nil
	})
	if err != nil {
		return fmt.Errorf("register reducer lease orphan snapshot source: %w", err)
	}
	if err := telemetry.RegisterReducerLeaseOrphanObservableGauges(instruments, meter, cachedReducerLeaseOrphanObserver{source: source}); err != nil {
		return fmt.Errorf("register reducer lease orphan observable gauges: %w", err)
	}
	return nil
}
