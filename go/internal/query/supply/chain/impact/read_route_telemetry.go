// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RouteSpanAttribute is the span attribute that names the read model a list,
// count, inventory, grouped-inventory or explain call read (the ReadRoute value).
const RouteSpanAttribute = "eshu.supply_chain.impact.read_route"

// countRoute records the route a read statement took: one increment of
// eshu_dp_supply_chain_impact_read_route_total and the route attribute on the
// active span. A list, count, inventory or grouped call runs it once, after the
// route function and before the supports readiness probe, so a call the probe
// refuses still counts the route it tried. An explain counts only the statements
// it runs: its readiness check runs first and a refused explain counts nothing,
// and an explain by public id that misses counts its fallback statement too, which
// is how a legacy fallback shows up. instruments is nil in tests and callers
// without the telemetry stack; the counter is then skipped and the span attribute
// alone is set.
func countRoute(ctx context.Context, instruments *telemetry.Instruments, route ReadRoute) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String(RouteSpanAttribute, string(route)))
	if instruments == nil || instruments.SupplyChainImpactReadRoute == nil {
		return
	}
	instruments.SupplyChainImpactReadRoute.Add(ctx, 1, metric.WithAttributes(telemetry.AttrRoute(string(route))))
}

// timeRoute starts the wall-time measurement of one read statement and returns
// the function that records it in eshu_dp_supply_chain_impact_read_duration_seconds
// under the route label. The caller defers it, so the duration covers the read, the
// jit-off transaction of the routes that open one, and the scan of the rows.
func timeRoute(ctx context.Context, instruments *telemetry.Instruments, route ReadRoute) func() {
	if instruments == nil || instruments.SupplyChainImpactReadDuration == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		instruments.SupplyChainImpactReadDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(telemetry.AttrRoute(string(route))))
	}
}

// observeRoute counts the route and starts its timer: the one call a read that is
// a single statement makes.
func observeRoute(ctx context.Context, instruments *telemetry.Instruments, route ReadRoute) func() {
	countRoute(ctx, instruments, route)
	return timeRoute(ctx, instruments, route)
}
