// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The closed resolved_by vocabulary for GetEntityContext (#7212). The same
// four values label eshu_dp_entity_context_resolution_total and the
// eshu.entity_context.resolved_by span attribute, so an operator can read the
// share of true misses (none) over a window without tracing.
const (
	// resolvedByAnchor means a non-final anchor statement returned the row:
	// the Neo4j indexed CALL () anchor, or a labeled statement of the
	// NornicDB per-label loop.
	resolvedByAnchor = "anchor"
	// resolvedByFallback means the final unlabeled `MATCH (e)` statement
	// returned the row after every anchor statement missed.
	resolvedByFallback = "fallback"
	// resolvedByContent means the graph produced no usable row and the content
	// store answered.
	resolvedByContent = "content"
	// resolvedByNone means nothing answered and the request ended in 404.
	resolvedByNone = "none"
)

const (
	spanAttrEntityContextResolvedBy      = "eshu.entity_context.resolved_by"
	spanAttrEntityContextStatementsTried = "eshu.entity_context.statements_tried"
)

// entityContextResolution records how one GetEntityContext request was
// answered. It is request-local and never shared across goroutines.
type entityContextResolution struct {
	// started is set once the request passed input and access validation, so
	// a 400 or an empty-grant 404 emits no resolution signal.
	started bool
	// statementsTotal is the number of graph statements available to the
	// request, and statementsTried the number sent before it resolved or
	// ended. Both are zero when no graph reader is configured.
	statementsTotal int
	statementsTried int
	// resolvedBy is empty until the request ends in an answer (200 or a
	// 404). A request that ends in an error never sets it.
	resolvedBy string
}

// graphResolvedBy names how the graph answered after the statement loop
// returned a row. The loop stops at the first row, so the answering statement
// is the last one tried, and only the final statement is the fallback.
func (r *entityContextResolution) graphResolvedBy() string {
	if r.statementsTried == r.statementsTotal {
		return resolvedByFallback
	}
	return resolvedByAnchor
}

// recordEntityContextResolution writes the request's resolution onto the
// server span and the resolution counter. It adds no log line: the two keys
// ride on the handler's existing warnings instead. h.Instruments may be nil.
func (h *Handler) recordEntityContextResolution(ctx context.Context, res *entityContextResolution) {
	if res == nil || !res.started {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int(spanAttrEntityContextStatementsTried, res.statementsTried))
	if res.resolvedBy == "" {
		return
	}
	span.SetAttributes(attribute.String(spanAttrEntityContextResolvedBy, res.resolvedBy))
	if h.Instruments != nil && h.Instruments.EntityContextResolution != nil {
		h.Instruments.EntityContextResolution.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrResolvedBy(res.resolvedBy)))
	}
}
