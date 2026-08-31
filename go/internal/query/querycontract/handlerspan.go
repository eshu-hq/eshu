// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HandlerTracer is the tracer every query HTTP read path starts its span from.
// It is exported because a few read paths start a span directly rather than
// going through StartHandlerSpan, and after #6060 those call sites live in
// family subpackages that cannot reach an unexported symbol here.
//
// Its name is deliberately still "eshu/go/internal/query" even though this file
// now lives in the querycontract subpackage. A tracer name is an operator-facing
// identifier that dashboards and saved span queries match on, not a source path,
// so renaming it to follow the directory would break every query built on the
// old name. The name tracks the surface these spans describe -- the query HTTP
// layer -- which has not moved.
var HandlerTracer = otel.Tracer("eshu/go/internal/query")

// StartHandlerSpan wraps query HTTP handlers in stable spans and attaches
// low-cardinality route/capability attributes for operator triage.
//
// It lives here rather than in package query because #6060 moves each handler
// family into its own subpackage, and a family subpackage cannot import the
// root package back without an import cycle through root's compatibility
// aliases. 84 root files start a handler span today -- 80 through
// startQueryHandlerSpan and 4 that call the tracer directly -- so every family needs
// this. Package query keeps startQueryHandlerSpan as a forwarding wrapper.
//
// The attributes are deliberately low-cardinality: route and capability are
// bounded sets, so they stay safe as span dimensions. Do not add a repo id,
// a selector, or any caller-supplied value here.
func StartHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return StartHandlerSpanWith(HandlerTracer, r, spanName, route, capability)
}

// StartHandlerSpanWith is StartHandlerSpan against a caller-supplied tracer.
//
// The tracer is a parameter rather than a captured package value because
// package query holds its tracer in a swappable var that its span tests
// replace with a recording provider before calling the handler. Reading
// HandlerTracer directly here would ignore that swap, and the tests would
// observe zero ended spans while the handler quietly emitted them to the real
// tracer -- a refactor that disconnects six tests from the code they assert on
// without failing to compile.
func StartHandlerSpanWith(tracer trace.Tracer, r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	ctx, span := tracer.Start(r.Context(), spanName)
	span.SetAttributes(
		attribute.String("http.route", route),
		attribute.String("eshu.capability", capability),
		attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
	)
	return r.WithContext(ctx), span
}
