// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
)

// queryHandlerTracer is this package's tracer AND the seam its span tests
// swap. It keeps the pre-move name from root package query's
// handler_tracing.go so the moved probe tests keep their exact swap shape:
// root used one var for both handler spans and probe spans, and this package
// does the same. It must stay a package-local var, seeded from
// queryspan.HandlerTracer, so a recording provider swapped in for this
// family's tests cannot change what any other family or root records, and
// two such swaps cannot race (#6060).
var queryHandlerTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps this family's HTTP handlers in stable spans and
// attaches low-cardinality route/capability attributes for operator triage.
//
// The implementation lives in queryspan so this family can start the same span
// without importing root package query, which it cannot do without an import
// cycle through root's compatibility aliases (#6060). The tracer name is
// unchanged, so emitted spans and the dashboards built on them are unaffected.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(queryHandlerTracer, r, spanName, route, capability)
}

// Graph-configured checks go through querycontract.GraphConfigured, the
// single home for the predicate this family historically spelled
// supplyChainGraphConfigured. A family-local copy drifted by comment alone
// (#6542 review); the shared leaf keeps both families evaluating the same
// ten lines without an import cycle through root's compatibility aliases.
