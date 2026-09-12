// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// freshnessHandlerTracer is this package's tracer AND the seam its span tests
// swap. Seeding it from queryspan.HandlerTracer keeps the swap private to
// this package rather than mutating what every other importer reads. See
// go/internal/query/language/handler_tracing.go for the identical seam.
var freshnessHandlerTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(freshnessHandlerTracer, r, spanName, route, capability)
}
