// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/trace"
)

// queryHandlerTracer is this package's tracer AND the seam its span tests
// swap: language_query_span_test.go, graph_entity_inventory_counts_test.go and
// the runtime-probe tests replace it with a recording provider, so every span
// this package starts must read it at call time rather than capture it.
var queryHandlerTracer = querycontract.HandlerTracer

// startQueryHandlerSpan wraps query HTTP handlers in stable spans and attaches
// low-cardinality route/capability attributes for operator triage.
//
// The implementation moved to querycontract for #6060 so a handler-family
// subpackage can start the same span without importing this package, which it
// cannot do without an import cycle through root's compatibility aliases. The
// tracer name is unchanged ("eshu/go/internal/query"), so emitted spans and the
// dashboards built on them are unaffected. This wrapper keeps the original
// unexported name for the 99 root files that call it.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return querycontract.StartHandlerSpanWith(queryHandlerTracer, r, spanName, route, capability)
}
