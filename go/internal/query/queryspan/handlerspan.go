// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryspan

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation-scope name every query HTTP span is
// recorded under. It stays "eshu/go/internal/query" even though this code no
// longer lives in that directory: a tracer name is an operator-facing
// identifier that saved span queries and dashboards match on, not a source
// path. Renaming it to follow the directory would break every saved query built
// on the old name while looking like tidying.
const tracerName = "eshu/go/internal/query"

// HandlerTracer returns the tracer query HTTP handlers record spans under.
//
// It is a function rather than an exported variable on purpose. A package-level
// var would be reassignable by any importer, and a test in one family package
// swapping it would mutate what every other family reads -- two such tests under
// t.Parallel() race. Callers that need a swappable seam keep their own
// package-local var seeded from this and pass it to StartHandlerSpanWith, which
// keeps the swap private to the package that wants it.
func HandlerTracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// StartHandlerSpanWith opens the per-route span for a query HTTP read and tags
// it with the low-cardinality attributes an operator triages on: the route
// template, the capability the route serves, and the service namespace. It
// returns the request carrying the span's context, and the span itself, which
// the caller must End.
//
// The tracer is a parameter rather than a package lookup because callers hold
// their own swappable tracer var for tests. Reading a package-level tracer here
// instead would ignore that swap: handlers would emit spans to the real tracer
// while a test's recording provider observed none, and the test would report
// "ended spans = 0, want 1" with nothing in the handler visibly wrong. An
// earlier version of this seam did exactly that and broke six span tests
// silently while compiling cleanly.
func StartHandlerSpanWith(
	tracer trace.Tracer,
	r *http.Request,
	spanName, route, capability string,
) (*http.Request, trace.Span) {
	ctx, span := tracer.Start(r.Context(), spanName)
	span.SetAttributes(
		attribute.String("http.route", route),
		attribute.String("eshu.capability", capability),
		attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
	)
	return r.WithContext(ctx), span
}
