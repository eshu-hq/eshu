// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"go.opentelemetry.io/otel/trace"
)

// codeQueryHandlerTracer is the code family's own handler-span tracer,
// seeded from the same queryspan.HandlerTracer() root's queryHandlerTracer
// (handler_tracing.go) seeds from. The code family cannot share root's var:
// once the family moves to its own subpackage it cannot reach an unexported
// root var at all, and even while it still lives here, root's span tests
// swap queryHandlerTracer to a recording provider for root's own handlers --
// swapping the same var out from under the code family's tests would make
// the two suites interfere. Each side gets its own var pointed at the same
// underlying tracer name, so emitted spans are unaffected.
//
// The name diverges from the supplychain/codeowners convention on purpose.
// Those two families are already separate packages, so each can name its
// copy queryHandlerTracer without colliding with root's. The code family
// still shares package query with root, so it cannot reuse that name yet;
// it uses codeQueryHandlerTracer/startCodeQueryHandlerSpan until the family
// moves to its own subpackage, at which point it takes the canonical
// queryHandlerTracer/startQueryHandlerSpan names and this file joins
// handler_tracing_parity_test.go's drift-guarded copies (#6060).
var codeQueryHandlerTracer = queryspan.HandlerTracer()

// startCodeQueryHandlerSpan wraps a code-family HTTP handler in a stable span
// using codeQueryHandlerTracer rather than root's queryHandlerTracer. See the
// var doc above for why the code family needs its own tracer var instead of
// calling root's startQueryHandlerSpan (handler_tracing.go) directly.
func startCodeQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return queryspan.StartHandlerSpanWith(codeQueryHandlerTracer, r, spanName, route, capability)
}
