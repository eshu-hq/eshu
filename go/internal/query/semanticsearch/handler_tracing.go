// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
)

// semanticSearchTracer is this package's tracer AND the seam its span tests
// swap. Mirrors root package query's handler_tracing.go and packagereg's: it
// must stay a package-local var, seeded from queryspan.HandlerTracer, so a
// recording provider swapped in for this family's tests cannot change what any
// other family or root records, and two such swaps cannot race (#6060).
var semanticSearchTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps this family's HTTP handlers in stable spans and
// attaches low-cardinality route/capability attributes for operator triage.
//
// The implementation lives in queryspan so this family can start the same span
// without importing root package query, which it cannot do without an import
// cycle through root's compatibility aliases (#6060). The tracer name is
// unchanged, so emitted spans and the dashboards built on them are unaffected.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(semanticSearchTracer, r, spanName, route, capability)
}
