// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/trace"
)

// manifestHandlerTracer is this package's tracer and the seam its span tests
// swap. Seeding it from tracing.HandlerTracer keeps a swap private to this
// package rather than mutating what every other importer reads, the same seam
// workitem/handler_tracing.go uses.
var manifestHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(manifestHandlerTracer, r, spanName, route, capability)
}

// manifestDependenciesBaseRefusedMessage is the fixed body when the read
// refuses a delta base: the delta-only rows are partial evidence, so the
// handler answers unavailable instead of serving them as the complete page.
// manifestDependenciesReadFailedMessage is the fixed body for a failed
// manifest dependency store read. The store error is recorded on the request
// span, never written to the client (#7674).
const (
	manifestDependenciesBaseRefusedMessage = "repository manifest dependency base generation unavailable"
	manifestDependenciesReadFailedMessage  = "repository manifest dependency read failed"
)
