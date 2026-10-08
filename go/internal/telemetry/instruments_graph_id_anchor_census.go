// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerGraphIDAnchorCensusInstruments registers the reducer's id-anchor
// census gauges, counter, and histogram on inst (#7212). The census is one
// AllNodesScan, so the reducer samples it on an interval and records the
// result after the pass; none of these instruments reads the graph on a scrape.
func registerGraphIDAnchorCensusInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.GraphIDAnchorUnreachableNodes, err = meter.Int64Gauge(
		"eshu_dp_graph_id_anchor_unreachable_nodes",
		metric.WithDescription("Snapshot of graph nodes with an id that the labeled Neo4j entity-context anchor cannot reach, from the last successful census pass; one read transaction, not a point in time, and a failed pass keeps the last good value"),
	); err != nil {
		return fmt.Errorf("register GraphIDAnchorUnreachableNodes gauge: %w", err)
	}
	if inst.GraphIDAnchorCensusLastSuccess, err = meter.Int64Gauge(
		"eshu_dp_graph_id_anchor_census_last_success_unixtime",
		metric.WithDescription("Unix second of the last successful id-anchor census pass; time() minus this gauge is the age of the unreachable-nodes snapshot"),
	); err != nil {
		return fmt.Errorf("register GraphIDAnchorCensusLastSuccess gauge: %w", err)
	}
	if inst.GraphIDAnchorCensusPasses, err = meter.Int64Counter(
		"eshu_dp_graph_id_anchor_census_passes_total",
		metric.WithDescription("Id-anchor census passes by outcome (ok, failed)"),
	); err != nil {
		return fmt.Errorf("register GraphIDAnchorCensusPasses counter: %w", err)
	}
	if inst.GraphIDAnchorCensusDuration, err = meter.Float64Histogram(
		"eshu_dp_graph_id_anchor_census_duration_seconds",
		metric.WithDescription("Wall time of one id-anchor census pass, by outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 30, 60, 120),
	); err != nil {
		return fmt.Errorf("register GraphIDAnchorCensusDuration histogram: %w", err)
	}
	return nil
}
