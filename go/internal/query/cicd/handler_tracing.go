// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/trace"
)

// cicdHandlerTracer is this package's tracer and the seam its span
// tests swap. Seeding it from tracing.HandlerTracer keeps a swap private to
// this package rather than mutating what every other importer reads, the
// same seam terraform/drift/handler_tracing.go uses.
var cicdHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(cicdHandlerTracer, r, spanName, route, capability)
}

// Fixed bodies for a failed CI/CD read. The store error is recorded on the
// request span, never written to the client (#7674).
const (
	runCorrelationsListFailedMessage      = "list CI/CD run correlations failed"
	runCorrelationsCountFailedMessage     = "count CI/CD run correlations failed"
	runCorrelationsInventoryFailedMessage = "read CI/CD run correlation inventory failed"
)
