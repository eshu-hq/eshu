// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

func normalizeImpactListLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func trimImpactRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

// Closed span attribute keys for the trace-resource-to-code traversal stage
// (#7931 option C). Values are request-shape facts (labels, bounds, counts,
// timings), never ids, names, paths, or statement text, so the set stays
// low-cardinality.
const (
	resourceToCodeAttrStartLabel      = "eshu.query.resource_to_code.start_label"
	resourceToCodeAttrMaxDepth        = "eshu.query.resource_to_code.max_depth"
	resourceToCodeAttrLimit           = "eshu.query.resource_to_code.limit"
	resourceToCodeAttrScoped          = "eshu.query.resource_to_code.scoped"
	resourceToCodeAttrRowsExpanded    = "eshu.query.resource_to_code.rows_expanded"
	resourceToCodeAttrRowsReturned    = "eshu.query.resource_to_code.rows_returned"
	resourceToCodeAttrMaxDepthReached = "eshu.query.resource_to_code.max_depth_reached"
	resourceToCodeAttrTraversalMS     = "eshu.query.resource_to_code.traversal_ms"
	resourceToCodeAttrOutcome         = "eshu.query.resource_to_code.outcome"
)

// Closed outcome values for the traversal graph-stage histogram, matching
// the impact ownership-check convention.
const (
	resourceToCodeOutcomeOK    = "ok"
	resourceToCodeOutcomeError = "error"
)

// resourceToCodeTraversal is the #7931 option-C stage timer for one
// trace-resource-to-code traversal. It opens the
// query.resource_to_code.traversal span, times the graph stage (traversal
// build plus the bounded graph run), and records
// eshu_dp_query_resource_to_code_traversal_duration_seconds. The traversal
// statements and the response are unchanged; only stage timing is observed.
//
// The timer lives beside the page-bound helpers because every count it
// reports is a bound of the same bounded page: the requested max depth and
// limit, the rows the traversal expanded, the rows the page returned, and
// the depth the traversal reached.
type resourceToCodeTraversal struct {
	span  trace.Span
	ctx   context.Context
	begin time.Time
	label string
}

// startResourceToCodeTraversal opens the traversal stage span with the
// closed request-shape facts (start label, max depth, limit, scoped). It
// returns the request carrying the traversal context — the bounded graph
// read runs under it so graph-read spans nest under the traversal, and
// downstream error/cancel reporting attributes to it — plus the stage timer,
// which the caller must End (usually deferred).
func startResourceToCodeTraversal(r *http.Request, label string, maxDepth, limit int, scoped bool) (*http.Request, *resourceToCodeTraversal) {
	ctx, span := queryHandlerTracer.Start(r.Context(), telemetry.SpanQueryResourceToCodeTraversal)
	span.SetAttributes(
		attribute.String(resourceToCodeAttrStartLabel, label),
		attribute.Int(resourceToCodeAttrMaxDepth, maxDepth),
		attribute.Int(resourceToCodeAttrLimit, limit),
		attribute.Bool(resourceToCodeAttrScoped, scoped),
	)
	return r.WithContext(ctx), &resourceToCodeTraversal{span: span, ctx: ctx, begin: time.Now(), label: label}
}

// Context returns the traversal stage context for the bounded graph run.
func (t *resourceToCodeTraversal) Context() context.Context {
	return t.ctx
}

// End closes the traversal span.
func (t *resourceToCodeTraversal) End() {
	t.span.End()
}

// ObserveRun records one graph run on the span and the duration histogram.
// outcome is error when the run failed, in which case rows is nil and the
// expanded count and reached depth record zero. A nil Instruments or a nil
// histogram records no metric, so handlers built without the telemetry
// stack keep working; the span attributes are always set.
func (t *resourceToCodeTraversal) ObserveRun(ctx context.Context, instruments *telemetry.Instruments, rows []map[string]any, runErr error) {
	outcome := resourceToCodeOutcomeOK
	if runErr != nil {
		outcome = resourceToCodeOutcomeError
	}
	elapsed := time.Since(t.begin)
	t.span.SetAttributes(
		attribute.Int(resourceToCodeAttrRowsExpanded, len(rows)),
		attribute.Int(resourceToCodeAttrMaxDepthReached, maxResourceToCodeDepth(rows)),
		attribute.Float64(resourceToCodeAttrTraversalMS, float64(elapsed.Nanoseconds())/1e6),
		attribute.String(resourceToCodeAttrOutcome, outcome),
	)
	if instruments == nil || instruments.QueryResourceToCodeTraversalDuration == nil {
		return
	}
	instruments.QueryResourceToCodeTraversalDuration.Record(ctx, elapsed.Seconds(),
		metric.WithAttributes(
			telemetry.AttrRoute(ownership.RouteTraceResourceToCode),
			telemetry.AttrNodeLabel(t.label),
			telemetry.AttrOutcome(outcome),
		))
}

// SetReturned records the shaped page size on the span once paths are built.
func (t *resourceToCodeTraversal) SetReturned(n int) {
	t.span.SetAttributes(attribute.Int(resourceToCodeAttrRowsReturned, n))
}

// maxResourceToCodeDepth returns the greatest depth across raw traversal
// rows, or zero when the traversal expanded nothing.
func maxResourceToCodeDepth(rows []map[string]any) int {
	max := 0
	for _, row := range rows {
		if depth := querycontract.IntVal(row, "depth"); depth > max {
			max = depth
		}
	}
	return max
}
