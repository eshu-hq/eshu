package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var queryHandlerTracer = otel.Tracer("eshu/go/internal/query")

// startQueryHandlerSpan wraps query HTTP handlers in stable spans and attaches
// low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	ctx, span := queryHandlerTracer.Start(r.Context(), spanName)
	span.SetAttributes(
		attribute.String("http.route", route),
		attribute.String("eshu.capability", capability),
		attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
	)
	return r.WithContext(ctx), span
}
