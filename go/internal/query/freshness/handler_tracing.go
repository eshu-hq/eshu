// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/trace"
)

// freshnessHandlerTracer is this package's tracer AND the seam its span tests
// swap. Seeding it from tracing.HandlerTracer keeps the swap private to
// this package rather than mutating what every other importer reads. See
// go/internal/query/language/handler_tracing.go for the identical seam.
var freshnessHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(freshnessHandlerTracer, r, spanName, route, capability)
}

// Fixed bodies for a failed freshness read, one per route. The store error is
// recorded on the request span, never written to the client (#7674).
const (
	changedSinceFailedMessage        = "compute changed-since delta failed"
	generationLifecycleFailedMessage = "list generation lifecycle failed"
	serviceChangedSinceFailedMessage = "compute service changed-since delta failed"
)

// writeFreshnessReadFailure answers a failed freshness read. A stale or
// timed-out PostgreSQL reader gets its shared verdict from
// querycontract.WriteGraphReadError (503 with Retry-After). Anything else
// answers message through tracing.WriteServerFailure: 500 with err recorded on
// the request span, or 499 with only the client-cancel event when the caller
// canceled the request.
func writeFreshnessReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
