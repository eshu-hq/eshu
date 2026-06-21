package query

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// apiRequestMeterName scopes the lazily registered per-endpoint request
// instruments to this package. It mirrors queryHandlerTracer so traces and
// metrics for the same handler share an instrumentation scope.
const apiRequestMeterName = "eshu/go/internal/query"

// apiRequestInstruments holds the per-endpoint duration histogram and server
// error counter. The instrument names match the canonical definitions in
// go/internal/telemetry/instruments.go; the query package records to them
// through the global meter provider that cmd/api and cmd/mcp-server install via
// telemetry.NewProviders, the same way queryHandlerTracer pulls the global
// tracer provider. This keeps recording self-contained so the MCP server, which
// does not build a telemetry.Instruments value, records the same metrics.
type apiRequestInstruments struct {
	duration metric.Float64Histogram
	errors   metric.Int64Counter
}

var (
	apiRequestInstrumentsOnce sync.Once
	apiRequestInstrumentsVal  *apiRequestInstruments
)

// apiRequestMetrics returns the process-wide per-endpoint request instruments,
// registering them once against the global meter. When registration fails (for
// example before a meter provider is installed in a unit test) it returns an
// instruments value with nil fields; callers must nil-check before recording.
func apiRequestMetrics() *apiRequestInstruments {
	apiRequestInstrumentsOnce.Do(func() {
		meter := otel.Meter(apiRequestMeterName)
		inst := &apiRequestInstruments{}
		buckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
		if hist, err := meter.Float64Histogram(
			"eshu_dp_api_request_duration_seconds",
			metric.WithDescription("Per-endpoint query API/MCP read handler duration, labeled by route and status_class"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(buckets...),
		); err == nil {
			inst.duration = hist
		}
		if counter, err := meter.Int64Counter(
			"eshu_dp_api_request_errors_total",
			metric.WithDescription("Per-endpoint query API/MCP read handler server errors (5xx), labeled by route and status_class"),
		); err == nil {
			inst.errors = counter
		}
		apiRequestInstrumentsVal = inst
	})
	return apiRequestInstrumentsVal
}

// RequestMetricsMiddleware records a per-endpoint duration histogram and server
// error counter for every request the mux serves. The route label is the
// matched route pattern (e.g. "GET /api/v0/iac/resources"), resolved via
// mux.Handler without mutating the request, so cardinality stays bounded by the
// registered routes rather than the unbounded concrete request paths. Requests
// that match no route are labeled "unmatched".
//
// It wraps the application mux only; the admin surface (probes, /metrics) is
// served by a separate mux and is intentionally not counted.
func RequestMetricsMiddleware(mux *http.ServeMux) http.Handler {
	if mux == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		route := pattern
		if route == "" {
			route = "unmatched"
		}

		recorder := &statusCapturingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		mux.ServeHTTP(recorder, r)
		recordAPIRequest(r, route, recorder.status, time.Since(start))
	})
}

// recordAPIRequest records the request duration and, for server errors, a
// single error event. Labels are the bounded route pattern and the status class
// ("2xx", "4xx", "5xx", ...), so the metric stays dashboard-safe.
func recordAPIRequest(r *http.Request, route string, status int, elapsed time.Duration) {
	inst := apiRequestMetrics()
	if inst == nil {
		return
	}
	statusClass := strconv.Itoa(status/100) + "xx"
	attrs := metric.WithAttributes(
		telemetry.AttrRoute(route),
		telemetry.AttrStatusClass(statusClass),
	)
	ctx := r.Context()
	if inst.duration != nil {
		inst.duration.Record(ctx, elapsed.Seconds(), attrs)
	}
	if status >= http.StatusInternalServerError && inst.errors != nil {
		inst.errors.Add(ctx, 1, attrs)
	}
}

// statusCapturingResponseWriter records the response status code so the request
// middleware can label metrics by status class. It defaults to 200 because a
// handler that writes a body without calling WriteHeader implies a 200. Unwrap
// exposes the underlying writer so http.ResponseController can reach optional
// interfaces (Flusher, Hijacker) on streaming handlers.
type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusCapturingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush forwards to the underlying writer when it implements http.Flusher.
// Overriding Write/WriteHeader hides the embedded interface's promoted methods,
// and http.ResponseWriter does not declare Flush, so without this method the
// wrapper is not an http.Flusher. Streaming handlers (handleAskSSE) type-assert
// the writer to http.Flusher; issue #3381 traced the Ask SSE 500 to this gap.
// The assertion is a no-op when the underlying writer cannot flush.
func (w *statusCapturingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards to the underlying writer when it implements http.Hijacker so
// connection-upgrade handlers keep working behind the metrics middleware. It
// returns http.ErrNotSupported when the underlying writer is not hijackable,
// matching the standard library contract callers expect.
func (w *statusCapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
