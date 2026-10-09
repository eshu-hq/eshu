// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Closed outcome values for
// eshu_dp_shared_projection_partition_visits_total (#7724). Every
// (domain, partition) cell records exactly one outcome per cycle, so the
// counter is the issue's idle-claim-attempt signal: every outcome except
// backoff_skipped is one partition lease claim attempt.
const (
	// SharedProjectionVisitOutcomeVisited means the lease was acquired
	// and selection ran (including error visits, which hold backoff and
	// retry next cycle).
	SharedProjectionVisitOutcomeVisited = "visited"
	// SharedProjectionVisitOutcomeBackoffSkipped means per-partition
	// backoff skipped the cell: no lease claim, no selection.
	SharedProjectionVisitOutcomeBackoffSkipped = "backoff_skipped"
	// SharedProjectionVisitOutcomeLeaseHeld means the lease claim lost
	// to another owner.
	SharedProjectionVisitOutcomeLeaseHeld = "lease_held"
	// SharedProjectionVisitOutcomeError means the visit failed (claim,
	// selection, write, or completion error) and holds backoff.
	SharedProjectionVisitOutcomeError = "error"
)

// Closed kind values for the eshu_dp_shared_projection_prefetch_*
// instruments (#7724). They mirror sharedintent.PrefetchKind; the worker
// maps that type onto these labels explicitly.
const (
	// SharedProjectionPrefetchKindAcceptance is the accepted-generation
	// prefetch over shared_projection_acceptance, re-queried fresh every
	// widen round and never cached.
	SharedProjectionPrefetchKindAcceptance = "acceptance"
	// SharedProjectionPrefetchKindReadiness is the graph-projection-phase
	// readiness prefetch over graph_projection_phase_state, served
	// through the cross-round cache except for the #7121 drain re-read.
	SharedProjectionPrefetchKindReadiness = "readiness"
)

// sharedProjectionPrefetchBuckets bounds the #7724 prefetch store-time
// histogram, shared with the bucket audit table so a future retune
// cannot drift one without the other.
var sharedProjectionPrefetchBuckets = []float64{0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// registerSharedProjectionCycle registers the #7724 shared-projection
// visit/backoff/prefetch instruments on inst. Labels are closed bounded
// sets: domain (the fixed sharedProjectionDomains list), outcome (the
// four visit outcomes above), kind (acceptance, readiness), and
// partition_id (the 0-based slot). No intent, scope, generation, key, or
// owner value ever rides a label.
func registerSharedProjectionCycle(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SharedProjectionPartitionVisits, err = meter.Int64Counter(
		"eshu_dp_shared_projection_partition_visits_total",
		metric.WithDescription("Shared-projection partition visits by domain and outcome (visited, backoff_skipped, lease_held, error); every non-skipped visit is one lease claim attempt (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPartitionVisits counter: %w", err)
	}
	if inst.SharedProjectionPartitionBackoff, err = meter.Float64Gauge(
		"eshu_dp_shared_projection_partition_backoff_seconds",
		metric.WithDescription("Current per-partition backoff delay by domain and partition_id; 0 means full cadence (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPartitionBackoff gauge: %w", err)
	}
	if inst.SharedProjectionCycleBackoff, err = meter.Float64Gauge(
		"eshu_dp_shared_projection_cycle_backoff_seconds",
		metric.WithDescription("Current global shared-projection cycle backoff interval, sampled every cycle (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionCycleBackoff gauge: %w", err)
	}
	if inst.SharedProjectionPartitionsAtMaxBackoff, err = meter.Int64Gauge(
		"eshu_dp_shared_projection_partitions_at_max_backoff",
		metric.WithDescription("Partitions pinned at T_max backoff, sampled every cycle (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPartitionsAtMaxBackoff gauge: %w", err)
	}
	if inst.SharedProjectionPrefetchKeys, err = meter.Int64Counter(
		"eshu_dp_shared_projection_prefetch_keys_total",
		metric.WithDescription("Distinct prefetch keys submitted to the store by domain and kind (acceptance, readiness); readiness counts only queried keys, cache hits are separate (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPrefetchKeys counter: %w", err)
	}
	if inst.SharedProjectionPrefetchQueries, err = meter.Int64Counter(
		"eshu_dp_shared_projection_prefetch_queries_total",
		metric.WithDescription("Prefetch SQL queries issued by domain and kind; batched prefetches issue at most ceil(keys/1000) (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPrefetchQueries counter: %w", err)
	}
	if inst.SharedProjectionPrefetchRows, err = meter.Int64Counter(
		"eshu_dp_shared_projection_prefetch_rows_total",
		metric.WithDescription("Prefetch rows returned by domain and kind (found keys) (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPrefetchRows counter: %w", err)
	}
	if inst.SharedProjectionPrefetchCacheHits, err = meter.Int64Counter(
		"eshu_dp_shared_projection_prefetch_cache_hits_total",
		metric.WithDescription("Readiness answers served from the cross-round cache by domain and kind; acceptance never records hits (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPrefetchCacheHits counter: %w", err)
	}
	if inst.SharedProjectionPrefetchDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_projection_prefetch_seconds",
		metric.WithDescription("Prefetch store time by domain and kind (#7724)"),
		metric.WithExplicitBucketBoundaries(sharedProjectionPrefetchBuckets...),
	); err != nil {
		return fmt.Errorf("register SharedProjectionPrefetchDuration histogram: %w", err)
	}
	if inst.SharedProjectionSelectionRounds, err = meter.Int64Counter(
		"eshu_dp_shared_projection_selection_rounds_total",
		metric.WithDescription("Selection widen rounds run by domain (#7724)"),
	); err != nil {
		return fmt.Errorf("register SharedProjectionSelectionRounds counter: %w", err)
	}
	return nil
}
