// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// supplyChainGraphConfigured reports whether reader is both non-nil and,
// when it opts into the configured-reader interface, actually wired with a
// live driver or session factory. A reader that does not implement the
// interface is treated as configured whenever it is non-nil, preserving
// existing test-fake behavior (#5761 F1).
//
// Family-local copy of root package query's languageQueryGraphConfigured:
// the hub cannot call the root helper without an import cycle, and the
// predicate is ten stable lines both families must evaluate identically.
// It MUST stay behavior-identical to its root source (named above); do not
// extend it with family-specific semantics.
func supplyChainGraphConfigured(reader querycontract.GraphQuery) bool {
	if reader == nil {
		return false
	}
	if configurable, ok := reader.(graphConfiguredReader); ok {
		return configurable.GraphConfigured()
	}
	return true
}

// graphConfiguredReader is the optional interface a GraphQuery implementation
// may satisfy to report a live driver or session factory. Mirrors root
// package query's interface of the same shape; declared locally so the hub
// never imports root.
type graphConfiguredReader interface {
	GraphConfigured() bool
}
