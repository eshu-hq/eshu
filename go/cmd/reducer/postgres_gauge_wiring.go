// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	"go.opentelemetry.io/otel/metric"
)

const (
	// postgresGaugeRefreshIntervalEnv is the delay between background
	// refreshes of the Postgres-backed observable gauges (#7064). It is a
	// single shared pair for all Postgres families: no family has shown a
	// need for its own cadence.
	postgresGaugeRefreshIntervalEnv = "ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL"
	// postgresGaugeRefreshTimeoutEnv bounds each background Postgres read
	// that feeds those gauges (#7064).
	postgresGaugeRefreshTimeoutEnv = "ESHU_POSTGRES_GAUGE_REFRESH_TIMEOUT"

	// Closed values of the `gauge` label on the eshu_dp_gauge_snapshot_*
	// metrics; one per Postgres-backed gauge served from a background
	// snapshot. The reducer_ prefix distinguishes these sources from the
	// ingester's identically-shaped queue sources in the shared stale
	// alert, which cannot tell binaries apart otherwise.
	gaugeQueueDepth              = "reducer_queue_depth"
	gaugeQueueOldestAge          = "reducer_queue_oldest_age"
	gaugeSourceQueueDepth        = "reducer_source_queue_depth"
	gaugeSourceQueueOldestAge    = "reducer_source_queue_oldest_age"
	gaugeSharedAcceptanceRows    = "reducer_shared_acceptance_rows"
	gaugeWorkflowFamilyQueueSize = "reducer_workflow_family_queue_depth"
	gaugeActiveGenerations       = "reducer_active_generations"
	gaugePoisonLiveness          = "reducer_poison_liveness"
)

// snapshotKeySep joins nested observation dimensions into one snapshot key.
// Postgres text cannot hold a NUL byte, so a key the database returned can
// never contain the separator: flattening is collision-free by construction,
// and the served series cardinality is identical to the direct callbacks.
// Keys never leave the process — each Fetch and its cached adapter live in
// the same binary — so the encoding is an internal convention, not a contract.
const snapshotKeySep = "\x00"

func flattenDepth2(nested map[string]map[string]int64) map[string]int64 {
	flat := make(map[string]int64, len(nested))
	for outer, inner := range nested {
		for name, count := range inner {
			flat[outer+snapshotKeySep+name] = count
		}
	}
	return flat
}

func unflattenDepth2(flat map[string]int64) map[string]map[string]int64 {
	nested := make(map[string]map[string]int64, len(flat))
	for key, count := range flat {
		outer, name, _ := strings.Cut(key, snapshotKeySep)
		inner, ok := nested[outer]
		if !ok {
			inner = map[string]int64{}
			nested[outer] = inner
		}
		inner[name] = count
	}
	return nested
}

func flattenDepth3(nested map[string]map[string]map[string]int64) map[string]int64 {
	flat := make(map[string]int64, len(nested))
	for first, second := range nested {
		for name, third := range second {
			for leaf, count := range third {
				flat[first+snapshotKeySep+name+snapshotKeySep+leaf] = count
			}
		}
	}
	return flat
}

func unflattenDepth3(flat map[string]int64) map[string]map[string]map[string]int64 {
	nested := make(map[string]map[string]map[string]int64, len(flat))
	for key, count := range flat {
		first, rest, _ := strings.Cut(key, snapshotKeySep)
		name, leaf, _ := strings.Cut(rest, snapshotKeySep)
		second, ok := nested[first]
		if !ok {
			second = map[string]map[string]int64{}
			nested[first] = second
		}
		third, ok := second[name]
		if !ok {
			third = map[string]int64{}
			second[name] = third
		}
		third[leaf] = count
	}
	return nested
}

// secondsToMillis encodes a float-seconds age into the int64 snapshot value.
// Ages are seconds-scale operational signals; sub-millisecond precision is
// below the gauge's resolution.
func secondsToMillis(seconds float64) int64 {
	return int64(math.Round(seconds * 1000))
}

func millisToSeconds(millis int64) float64 {
	return float64(millis) / 1000
}

const (
	acceptanceRowsKey        = "rows"
	poisonScopesKey          = "scopes"
	poisonItemsKey           = "items"
	poisonOldestAgeMillisKey = "oldest_age_millis"
)

// errPostgresGaugeSnapshotNotReady reports a scalar gauge with no published
// snapshot yet. Scalar gauges must error rather than observe a zero: a zero
// poison scope count means "class empty", so a pre-success zero would lie.
// Map gauges range over the nil snapshot instead and observe nothing.
var errPostgresGaugeSnapshotNotReady = errors.New("postgres gauge snapshot not yet available")

// cachedQueueObserver serves the queue depth and oldest-age gauges from a
// snapshot.
type cachedQueueObserver struct {
	depths *snapshot.Source
	ages   *snapshot.Source
}

func (c cachedQueueObserver) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	counts, err := c.depths.Counts(ctx)
	if err != nil {
		return nil, err
	}
	return unflattenDepth2(counts), nil
}

func (c cachedQueueObserver) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	counts, err := c.ages.Counts(ctx)
	if err != nil {
		return nil, err
	}
	ages := make(map[string]float64, len(counts))
	for queue, millis := range counts {
		ages[queue] = millisToSeconds(millis)
	}
	return ages, nil
}

// cachedSourceQueueObserver additionally serves the source-system queue
// gauges. It is a separate type so the SourceQueueObserver assertion in
// RegisterObservableGauges keeps its exact meaning: source gauges register
// only when the underlying observer provides them.
type cachedSourceQueueObserver struct {
	cachedQueueObserver
	sourceDepths *snapshot.Source
	sourceAges   *snapshot.Source
}

func (c cachedSourceQueueObserver) SourceQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	counts, err := c.sourceDepths.Counts(ctx)
	if err != nil {
		return nil, err
	}
	return unflattenDepth3(counts), nil
}

func (c cachedSourceQueueObserver) SourceQueueOldestAge(ctx context.Context) (map[string]map[string]float64, error) {
	counts, err := c.sourceAges.Counts(ctx)
	if err != nil {
		return nil, err
	}
	ages := make(map[string]map[string]float64, len(counts))
	for queue, sources := range unflattenDepth2(counts) {
		perSource := make(map[string]float64, len(sources))
		for sourceSystem, millis := range sources {
			perSource[sourceSystem] = millisToSeconds(millis)
		}
		ages[queue] = perSource
	}
	return ages, nil
}

// cachedAcceptanceObserver serves the shared-acceptance row gauge from a
// snapshot.
type cachedAcceptanceObserver struct{ source *snapshot.Source }

func (c cachedAcceptanceObserver) AcceptanceRowCount(ctx context.Context) (int64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, err
	}
	if counts == nil {
		return 0, errPostgresGaugeSnapshotNotReady
	}
	return counts[acceptanceRowsKey], nil
}

// cachedWorkflowFamilyQueueDepthObserver serves the per-family queue-depth
// gauge from a snapshot.
type cachedWorkflowFamilyQueueDepthObserver struct{ source *snapshot.Source }

func (c cachedWorkflowFamilyQueueDepthObserver) WorkflowFamilyQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return nil, err
	}
	return unflattenDepth3(counts), nil
}

// cachedActiveGenerationAgeObserver serves the active-generation age-bucket
// gauge from a snapshot. The buckets are already flat, so this is a direct
// pass-through like the graph orphan adapter.
type cachedActiveGenerationAgeObserver struct{ source *snapshot.Source }

func (c cachedActiveGenerationAgeObserver) ActiveGenerationsByAge(ctx context.Context) (map[string]int64, error) {
	return c.source.Counts(ctx)
}

// cachedPoisonLivenessObserver serves the three poison-class gauges from one
// snapshot. One refresh fetch feeds all three gauge callbacks, preserving the
// single-query-per-scrape idiom of the direct wiring.
type cachedPoisonLivenessObserver struct{ source *snapshot.Source }

func (c cachedPoisonLivenessObserver) PoisonDeadLetterCounts(ctx context.Context) (int64, int64, float64, error) {
	counts, err := c.source.Counts(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	if counts == nil {
		return 0, 0, 0, errPostgresGaugeSnapshotNotReady
	}
	return counts[poisonScopesKey], counts[poisonItemsKey], millisToSeconds(counts[poisonOldestAgeMillisKey]), nil
}

// registerPostgresBackedGauges wires every observable gauge whose value comes
// from a Postgres read to a background snapshot refresher instead of querying
// Postgres inside the /metrics collection. A slow or locked Postgres read
// used to hold the metrics collection lock and wedge every scrape behind it
// (#7064); now the gauge callbacks only read the last published snapshot, and
// the refresher reads Postgres on its own goroutines with a per-read
// deadline. It returns the refresher without starting it: the caller starts
// it with the process shutdown context and waits for it before closing the
// database. It returns nil when every observer is nil.
//
// The registrations in go/internal/telemetry are untouched: the cached
// adapters implement the same observer contracts, so the served series and
// their cardinality are identical to the direct callbacks by construction.
func registerPostgresBackedGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	queueObs telemetry.QueueObserver,
	acceptanceObs telemetry.AcceptanceObserver,
	workflowObs telemetry.WorkflowFamilyQueueDepthObserver,
	activeGenObs telemetry.ActiveGenerationAgeObserver,
	poisonObs telemetry.PoisonLivenessObserver,
	getenv func(string) string,
	logger *slog.Logger,
) (*snapshot.Refresher, error) {
	if queueObs == nil && acceptanceObs == nil && workflowObs == nil && activeGenObs == nil && poisonObs == nil {
		return nil, nil
	}
	refresher, err := snapshot.New(snapshot.Config{
		Interval: loadDurationOrDefault(getenv, postgresGaugeRefreshIntervalEnv, snapshot.DefaultInterval),
		Timeout:  loadDurationOrDefault(getenv, postgresGaugeRefreshTimeoutEnv, snapshot.DefaultTimeout),
		Meter:    meter,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build postgres gauge snapshot refresher: %w", err)
	}
	if queueObs != nil {
		if err := registerPostgresQueueGauges(refresher, instruments, meter, queueObs); err != nil {
			return nil, err
		}
	}
	if acceptanceObs != nil {
		acceptance, err := refresher.Register(gaugeSharedAcceptanceRows, func(ctx context.Context) (map[string]int64, error) {
			rows, err := acceptanceObs.AcceptanceRowCount(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]int64{acceptanceRowsKey: rows}, nil
		})
		if err != nil {
			return nil, fmt.Errorf("register shared acceptance snapshot source: %w", err)
		}
		if err := telemetry.RegisterAcceptanceObservableGauges(instruments, meter, cachedAcceptanceObserver{source: acceptance}); err != nil {
			return nil, fmt.Errorf("register shared acceptance observable gauge: %w", err)
		}
	}
	if workflowObs != nil {
		family, err := refresher.Register(gaugeWorkflowFamilyQueueSize, func(ctx context.Context) (map[string]int64, error) {
			depths, err := workflowObs.WorkflowFamilyQueueDepths(ctx)
			if err != nil {
				return nil, err
			}
			return flattenDepth3(depths), nil
		})
		if err != nil {
			return nil, fmt.Errorf("register workflow family queue snapshot source: %w", err)
		}
		if err := telemetry.RegisterWorkflowFamilyQueueDepthObservableGauge(instruments, meter, cachedWorkflowFamilyQueueDepthObserver{source: family}); err != nil {
			return nil, fmt.Errorf("register workflow family queue depth observable gauge: %w", err)
		}
	}
	if activeGenObs != nil {
		generations, err := refresher.Register(gaugeActiveGenerations, activeGenObs.ActiveGenerationsByAge)
		if err != nil {
			return nil, fmt.Errorf("register active generations snapshot source: %w", err)
		}
		if err := telemetry.RegisterActiveGenerationAgeObservableGauge(instruments, meter, cachedActiveGenerationAgeObserver{source: generations}); err != nil {
			return nil, fmt.Errorf("register active generation age observable gauge: %w", err)
		}
	}
	if poisonObs != nil {
		poison, err := refresher.Register(gaugePoisonLiveness, func(ctx context.Context) (map[string]int64, error) {
			scopes, items, oldestAgeSeconds, err := poisonObs.PoisonDeadLetterCounts(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]int64{
				poisonScopesKey:          scopes,
				poisonItemsKey:           items,
				poisonOldestAgeMillisKey: secondsToMillis(oldestAgeSeconds),
			}, nil
		})
		if err != nil {
			return nil, fmt.Errorf("register poison liveness snapshot source: %w", err)
		}
		if err := telemetry.RegisterPoisonLivenessObservableGauges(instruments, meter, cachedPoisonLivenessObserver{source: poison}); err != nil {
			return nil, fmt.Errorf("register poison liveness observable gauges: %w", err)
		}
	}
	return refresher, nil
}

// registerPostgresQueueGauges serves the queue depth, oldest-age, and
// source-system queue gauges from snapshot sources fed by queueObs.
func registerPostgresQueueGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	queueObs telemetry.QueueObserver,
) error {
	depths, err := refresher.Register(gaugeQueueDepth, func(ctx context.Context) (map[string]int64, error) {
		depths, err := queueObs.QueueDepths(ctx)
		if err != nil {
			return nil, err
		}
		return flattenDepth2(depths), nil
	})
	if err != nil {
		return fmt.Errorf("register queue depth snapshot source: %w", err)
	}
	ages, err := refresher.Register(gaugeQueueOldestAge, func(ctx context.Context) (map[string]int64, error) {
		ages, err := queueObs.QueueOldestAge(ctx)
		if err != nil {
			return nil, err
		}
		millis := make(map[string]int64, len(ages))
		for queue, age := range ages {
			millis[queue] = secondsToMillis(age)
		}
		return millis, nil
	})
	if err != nil {
		return fmt.Errorf("register queue oldest age snapshot source: %w", err)
	}
	var cached telemetry.QueueObserver = cachedQueueObserver{depths: depths, ages: ages}
	if srcObs, ok := queueObs.(telemetry.SourceQueueObserver); ok {
		sourceDepths, err := refresher.Register(gaugeSourceQueueDepth, func(ctx context.Context) (map[string]int64, error) {
			depths, err := srcObs.SourceQueueDepths(ctx)
			if err != nil {
				return nil, err
			}
			return flattenDepth3(depths), nil
		})
		if err != nil {
			return fmt.Errorf("register source queue depth snapshot source: %w", err)
		}
		sourceAges, err := refresher.Register(gaugeSourceQueueOldestAge, func(ctx context.Context) (map[string]int64, error) {
			ages, err := srcObs.SourceQueueOldestAge(ctx)
			if err != nil {
				return nil, err
			}
			millis := make(map[string]int64, len(ages))
			for queue, sources := range ages {
				for sourceSystem, age := range sources {
					millis[queue+snapshotKeySep+sourceSystem] = secondsToMillis(age)
				}
			}
			return millis, nil
		})
		if err != nil {
			return fmt.Errorf("register source queue oldest age snapshot source: %w", err)
		}
		cached = cachedSourceQueueObserver{
			cachedQueueObserver: cachedQueueObserver{depths: depths, ages: ages},
			sourceDepths:        sourceDepths,
			sourceAges:          sourceAges,
		}
	}
	if err := telemetry.RegisterObservableGauges(instruments, meter, cached, nil); err != nil {
		return fmt.Errorf("register queue observable gauges: %w", err)
	}
	return nil
}

// startPostgresGaugeRefresher starts refresher (a no-op for nil) under ctx,
// the process shutdown context, and returns a function that blocks until its
// loops have exited. Call that function before closing the database so no
// in-flight refresh read fails against a closed pool.
func startPostgresGaugeRefresher(ctx context.Context, refresher *snapshot.Refresher) (wait func()) {
	if refresher == nil {
		return func() {}
	}
	refresher.Start(ctx)
	return refresher.Wait
}
