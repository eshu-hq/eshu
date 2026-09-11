// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// languageHandlerTracer is this package's tracer AND the seam its span tests
// swap. Seeding it from queryspan.HandlerTracer keeps the swap private to
// this package rather than mutating what every other importer reads. See
// go/internal/query/incident/handler.go for the identical seam.
var languageHandlerTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(languageHandlerTracer, r, spanName, route, capability)
}
