// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionResolvedBy labels eshu_dp_entity_context_resolution_total
// with how GET /api/v0/entities/{entity_id}/context answered a request. The
// value set is closed: anchor, fallback, content, none.
const MetricDimensionResolvedBy = "resolved_by"

// AttrResolvedBy returns a resolved_by attribute naming how an entity-context
// request was answered. v must be one of anchor (a non-final anchor
// statement returned the row), fallback (the final unlabeled statement did),
// content (the graph returned no row and the content store answered), or none
// (nothing answered, 404).
func AttrResolvedBy(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionResolvedBy, v)
}

// registerEntityContextResolutionInstruments registers the entity-context
// resolution counter on inst (#7212).
func registerEntityContextResolutionInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.EntityContextResolution, err = meter.Int64Counter(
		"eshu_dp_entity_context_resolution_total",
		metric.WithDescription("Entity-context requests that ended without an error, by resolved_by (anchor: a non-final anchor statement returned the row; fallback: the final unlabeled MATCH (e) statement did; content: the graph returned no row and the content store answered; none: nothing answered, 404)"),
	); err != nil {
		return fmt.Errorf("register EntityContextResolution counter: %w", err)
	}
	return nil
}
