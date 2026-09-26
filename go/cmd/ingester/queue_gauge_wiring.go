// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
)

const (
	// postgresQueueGaugeRefreshIntervalEnv is the delay between background
	// refreshes of the ingester's Postgres-backed queue gauges (#7064). It
	// shares its default with the reducer's Postgres gauge pair; the two
	// binaries read the same queue tables, so no per-binary cadence is
	// needed.
	postgresQueueGaugeRefreshIntervalEnv = "ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL"
	// postgresQueueGaugeRefreshTimeoutEnv bounds each background Postgres
	// queue read that feeds those gauges (#7064).
	postgresQueueGaugeRefreshTimeoutEnv = "ESHU_POSTGRES_GAUGE_REFRESH_TIMEOUT"

	gaugeIngesterQueueDepth       = "ingester_queue_depth"
	gaugeIngesterQueueOldestAge   = "ingester_queue_oldest_age"
	gaugeIngesterSourceQueueDepth = "ingester_source_queue_depth"
	gaugeIngesterSourceQueueAge   = "ingester_source_queue_oldest_age"
)

// queueSnapshotKeySep joins nested observation dimensions into one snapshot
// key. Postgres text cannot hold a NUL byte, so a key the database returned
// can never contain the separator. Keys never leave the process.
const queueSnapshotKeySep = "\x00"

func loadPositiveDurationOrDefault(getenv func(string) string, key string, defaultValue time.Duration) time.Duration {
	parsed, err := time.ParseDuration(strings.TrimSpace(getenv(key)))
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

// cachedIngesterQueueObserver serves the ingester queue depth, oldest-age,
// and source-system queue gauges from a snapshot.
type cachedIngesterQueueObserver struct {
	depths *snapshot.Source
	ages   *snapshot.Source
}

func (c cachedIngesterQueueObserver) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	counts, err := c.depths.Counts(ctx)
	if err != nil {
		return nil, err
	}
	nested := make(map[string]map[string]int64, len(counts))
	for key, count := range counts {
		queue, status, _ := strings.Cut(key, queueSnapshotKeySep)
		inner, ok := nested[queue]
		if !ok {
			inner = map[string]int64{}
			nested[queue] = inner
		}
		inner[status] = count
	}
	return nested, nil
}

func (c cachedIngesterQueueObserver) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	counts, err := c.ages.Counts(ctx)
	if err != nil {
		return nil, err
	}
	ages := make(map[string]float64, len(counts))
	for queue, millis := range counts {
		ages[queue] = float64(millis) / 1000
	}
	return ages, nil
}

// cachedIngesterSourceQueueObserver additionally serves the source-system
// queue gauges, keeping the SourceQueueObserver assertion meaning intact.
type cachedIngesterSourceQueueObserver struct {
	cachedIngesterQueueObserver
	sourceDepths *snapshot.Source
	sourceAges   *snapshot.Source
}

func (c cachedIngesterSourceQueueObserver) SourceQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	counts, err := c.sourceDepths.Counts(ctx)
	if err != nil {
		return nil, err
	}
	nested := make(map[string]map[string]map[string]int64, len(counts))
	for key, count := range counts {
		queue, rest, _ := strings.Cut(key, queueSnapshotKeySep)
		sourceSystem, status, _ := strings.Cut(rest, queueSnapshotKeySep)
		second, ok := nested[queue]
		if !ok {
			second = map[string]map[string]int64{}
			nested[queue] = second
		}
		third, ok := second[sourceSystem]
		if !ok {
			third = map[string]int64{}
			second[sourceSystem] = third
		}
		third[status] = count
	}
	return nested, nil
}

func (c cachedIngesterSourceQueueObserver) SourceQueueOldestAge(ctx context.Context) (map[string]map[string]float64, error) {
	counts, err := c.sourceAges.Counts(ctx)
	if err != nil {
		return nil, err
	}
	nested := make(map[string]map[string]float64, len(counts))
	for key, millis := range counts {
		queue, sourceSystem, _ := strings.Cut(key, queueSnapshotKeySep)
		second, ok := nested[queue]
		if !ok {
			second = map[string]float64{}
			nested[queue] = second
		}
		second[sourceSystem] = float64(millis) / 1000
	}
	return nested, nil
}

// registerPostgresQueueGauges wires the ingester's Postgres-backed queue
// gauges to a background snapshot refresher instead of querying Postgres
// inside the /metrics collection (#7064). It returns the refresher without
// starting it: the caller starts it with the process shutdown context and
// waits for it before closing the database. It returns nil when queueObs is
// nil.
func registerPostgresQueueGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	queueObs telemetry.QueueObserver,
	getenv func(string) string,
	logger *slog.Logger,
) (*snapshot.Refresher, error) {
	if queueObs == nil {
		return nil, nil
	}
	refresher, err := snapshot.New(snapshot.Config{
		Interval: loadPositiveDurationOrDefault(getenv, postgresQueueGaugeRefreshIntervalEnv, snapshot.DefaultInterval),
		Timeout:  loadPositiveDurationOrDefault(getenv, postgresQueueGaugeRefreshTimeoutEnv, snapshot.DefaultTimeout),
		Meter:    meter,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build ingester postgres gauge snapshot refresher: %w", err)
	}
	depths, err := refresher.Register(gaugeIngesterQueueDepth, func(ctx context.Context) (map[string]int64, error) {
		depths, err := queueObs.QueueDepths(ctx)
		if err != nil {
			return nil, err
		}
		flat := make(map[string]int64, len(depths))
		for queue, statuses := range depths {
			for status, count := range statuses {
				flat[queue+queueSnapshotKeySep+status] = count
			}
		}
		return flat, nil
	})
	if err != nil {
		return nil, fmt.Errorf("register ingester queue depth snapshot source: %w", err)
	}
	ages, err := refresher.Register(gaugeIngesterQueueOldestAge, func(ctx context.Context) (map[string]int64, error) {
		ages, err := queueObs.QueueOldestAge(ctx)
		if err != nil {
			return nil, err
		}
		millis := make(map[string]int64, len(ages))
		for queue, age := range ages {
			millis[queue] = int64(math.Round(age * 1000))
		}
		return millis, nil
	})
	if err != nil {
		return nil, fmt.Errorf("register ingester queue oldest age snapshot source: %w", err)
	}
	var cached telemetry.QueueObserver = cachedIngesterQueueObserver{depths: depths, ages: ages}
	if srcObs, ok := queueObs.(telemetry.SourceQueueObserver); ok {
		sourceDepths, err := refresher.Register(gaugeIngesterSourceQueueDepth, func(ctx context.Context) (map[string]int64, error) {
			depths, err := srcObs.SourceQueueDepths(ctx)
			if err != nil {
				return nil, err
			}
			flat := make(map[string]int64, len(depths))
			for queue, sources := range depths {
				for sourceSystem, statuses := range sources {
					for status, count := range statuses {
						flat[queue+queueSnapshotKeySep+sourceSystem+queueSnapshotKeySep+status] = count
					}
				}
			}
			return flat, nil
		})
		if err != nil {
			return nil, fmt.Errorf("register ingester source queue depth snapshot source: %w", err)
		}
		sourceAges, err := refresher.Register(gaugeIngesterSourceQueueAge, func(ctx context.Context) (map[string]int64, error) {
			ages, err := srcObs.SourceQueueOldestAge(ctx)
			if err != nil {
				return nil, err
			}
			millis := make(map[string]int64, len(ages))
			for queue, sources := range ages {
				for sourceSystem, age := range sources {
					millis[queue+queueSnapshotKeySep+sourceSystem] = int64(math.Round(age * 1000))
				}
			}
			return millis, nil
		})
		if err != nil {
			return nil, fmt.Errorf("register ingester source queue oldest age snapshot source: %w", err)
		}
		cached = cachedIngesterSourceQueueObserver{
			cachedIngesterQueueObserver: cachedIngesterQueueObserver{depths: depths, ages: ages},
			sourceDepths:                sourceDepths,
			sourceAges:                  sourceAges,
		}
	}
	if err := telemetry.RegisterObservableGauges(instruments, meter, cached, nil); err != nil {
		return nil, fmt.Errorf("register ingester queue observable gauges: %w", err)
	}
	return refresher, nil
}

// startPostgresQueueGauges starts refresher (a no-op for nil) under ctx, the
// process shutdown context, and returns a function that blocks until its
// loops have exited. Call that function before closing the database.
func startPostgresQueueGauges(ctx context.Context, refresher *snapshot.Refresher) (wait func()) {
	if refresher == nil {
		return func() {}
	}
	refresher.Start(ctx)
	return refresher.Wait
}
