// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/trace"
)

// driftHandlerTracer is this package's tracer and the seam its span
// tests swap. Seeding it from tracing.HandlerTracer keeps a swap private to
// this package rather than mutating what every other importer reads, the
// same seam workitem/handler_tracing.go uses.
var driftHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(driftHandlerTracer, r, spanName, route, capability)
}
