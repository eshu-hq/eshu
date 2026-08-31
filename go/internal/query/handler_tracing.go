// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// queryHandlerTracer is this package's tracer AND the seam its span tests swap.
// language_query_span_test.go, graph_entity_inventory_counts_test.go and the
// runtime-probe tests replace it with a recording provider, so it must stay a
// package-local var: seeding it from queryspan.HandlerTracer keeps the swap
// private to this package rather than mutating what every other importer reads.
var queryHandlerTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps query HTTP handlers in stable spans and attaches
// low-cardinality route/capability attributes for operator triage.
//
// The implementation lives in queryspan so a handler-family subpackage can start
// the same span without importing this package, which it cannot do without an
// import cycle through root's compatibility aliases (#6060). The tracer name is
// unchanged, so emitted spans and the dashboards built on them are unaffected.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(queryHandlerTracer, r, spanName, route, capability)
}
