// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// queryHandlerTracer is this package's tracer AND the seam its span tests
// swap. It keeps the pre-move name from root package query's
// handler_tracing.go so the moved probe tests keep their exact swap shape:
// root used one var for both handler spans and probe spans, and this package
// does the same. It must stay a package-local var, seeded from
// tracing.HandlerTracer, so a recording provider swapped in for this
// family's tests cannot change what any other family or root records, and
// two such swaps cannot race (#6060).
var queryHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this family's HTTP handlers in stable spans and
// attaches low-cardinality route/capability attributes for operator triage.
//
// The implementation lives in package tracing so this family can start the same
// span without importing root package query, which it cannot do without an
// import cycle through root's compatibility aliases (#6060). The tracer name is
// unchanged, so emitted spans and the dashboards built on them are unaffected.
//
// This is a family-local copy of root's handler_tracing.go helper (and of
// supply/chain/handler_tracing.go): the three copies must stay
// behavior-identical, and handler_tracing_test.go pins this copy's emitted
// span against the tracing package's operator contract so drift fails loudly.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(queryHandlerTracer, r, spanName, route, capability)
}
