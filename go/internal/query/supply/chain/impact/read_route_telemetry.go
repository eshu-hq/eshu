// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RouteSpanAttribute is the span attribute that names the read model a list,
// count, inventory or grouped-inventory call read (the ReadRoute value).
const RouteSpanAttribute = "eshu.supply_chain.impact.read_route"

// observeRoute records the route a read took: one increment of
// eshu_dp_supply_chain_impact_read_route_total and the route attribute on the
// active span. It runs once per call, after the route function and before the
// readiness probe, so a call the probe refuses still counts the route it tried.
// instruments is nil in tests and callers without the telemetry stack; the
// counter is then skipped and the span attribute alone is set.
func observeRoute(ctx context.Context, instruments *telemetry.Instruments, route ReadRoute) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String(RouteSpanAttribute, string(route)))
	if instruments == nil || instruments.SupplyChainImpactReadRoute == nil {
		return
	}
	instruments.SupplyChainImpactReadRoute.Add(ctx, 1, metric.WithAttributes(telemetry.AttrRoute(string(route))))
}
