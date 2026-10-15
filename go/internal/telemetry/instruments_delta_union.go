// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// deltaUnionFactCountBuckets bounds the union fact-count histogram: delta
// generations stay small while their base can reach six figures.
var deltaUnionFactCountBuckets = []float64{10, 100, 1000, 10000, 50000, 100000, 250000}

// registerProjectorDeltaUnion registers the #7797 delta-base union
// instruments on inst: one counter of union read outcomes and one histogram
// of the fact counts behind each union. Labels stay bounded: outcome is the
// closed projector.DeltaUnionOutcome* set (union_base_found,
// union_base_missing_refused), kind is base, delta, or union. Commit SHAs,
// scope ids, and generation ids never become labels; counts travel as
// histogram values, never as label values.
func registerProjectorDeltaUnion(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ProjectorDeltaUnion, err = meter.Int64Counter(
		"eshu_dp_projector_delta_union_total",
		metric.WithDescription("Delta-base union fact-load decisions by outcome (#7797): "+
			"union_base_found (the delta projected over its base full generation) or "+
			"union_base_missing_refused (no projected base, the delta was refused)"),
	); err != nil {
		return fmt.Errorf("register ProjectorDeltaUnion counter: %w", err)
	}
	if inst.ProjectorDeltaUnionFacts, err = meter.Int64Histogram(
		"eshu_dp_projector_delta_union_facts",
		metric.WithDescription("Fact counts behind one delta-base union read (#7797), by outcome and kind (base, delta, union)"),
		metric.WithExplicitBucketBoundaries(deltaUnionFactCountBuckets...),
	); err != nil {
		return fmt.Errorf("register ProjectorDeltaUnionFacts histogram: %w", err)
	}
	return nil
}
