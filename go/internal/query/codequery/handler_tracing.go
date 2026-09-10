// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// queryHandlerTracer is the code family's own handler-span tracer, seeded
// from the same queryspan.HandlerTracer() root's queryHandlerTracer
// (handler_tracing.go) seeds from. The code family cannot share root's var:
// it lives in its own subpackage and cannot reach an unexported root var at
// all, and root's span tests swap queryHandlerTracer to a recording provider
// for root's own handlers -- swapping the same var out from under the code
// family's tests would make the two suites interfere. Each side gets its own
// var pointed at the same underlying tracer name, so emitted spans are
// unaffected. handler_tracing_parity_test.go pins this copy byte-behavioral
// to root's.
var queryHandlerTracer = queryspan.HandlerTracer()

// startQueryHandlerSpan wraps a code-family HTTP handler in a stable span
// using queryHandlerTracer rather than root's. See the var doc above for why
// the code family needs its own tracer var instead of calling root's
// startQueryHandlerSpan (handler_tracing.go) directly.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(queryHandlerTracer, r, spanName, route, capability)
}
