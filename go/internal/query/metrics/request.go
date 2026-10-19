// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// RequestMiddleware records a per-endpoint duration histogram and server
// error counter for every request the mux serves. The route label is the
// matched route pattern (e.g. "GET /api/v0/iac/resources"), resolved via
// mux.Handler without mutating the request, so cardinality stays bounded by the
// registered routes rather than the unbounded concrete request paths. Requests
// that match no route are labeled "unmatched".
//
// It also exposes the per-request handler total as a Server-Timing trailer
// ("Server-Timing: total;dur=<ms>") so a slow call can be attributed without
// guessing (issue #7733). A trailer, not a response header, is deliberate:
// net/http flushes response headers on the handler's first body Write, so a
// header set after mux.ServeHTTP returns is silently dropped on the wire,
// while an announced trailer is delivered for buffered and streaming (SSE)
// responses alike. The announcement is gated: a Trailer header on a bodyless
// response is invalid with its Content-Length encoding and crashes
// Node-based proxies (Vite died with ERR_HTTP_TRAILER_INVALID on a 204 in
// the SSO E2E gate), so 1xx/204/304, HEAD, and handlers that return without
// writing never announce — the value lands as a plain header, or nowhere for
// 204/304. The trailer carries only the total this seam brackets; the
// middleware cannot split Postgres versus graph time without invasive
// per-store plumbing, so no finer stage is reported.
//
// It wraps the application mux only; the admin surface (probes, /metrics) is
// served by a separate mux and is intentionally not counted.
func RequestMiddleware(mux *http.ServeMux) http.Handler {
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
		if r.Method == http.MethodHead {
			// HEAD responses carry no body, so a trailer can never be
			// delivered; suppress the announcement. The value below lands
			// as a plain header only when the handler wrote nothing;
			// otherwise the headers are already on the wire and it drops.
			recorder.suppressTrailer = true
		}
		start := time.Now()
		mux.ServeHTTP(recorder, r)
		elapsed := time.Since(start)
		w.Header().Set("Server-Timing", formatServerTiming(elapsed))
		recordAPIRequest(r, route, recorder.status, elapsed)
	})
}

// serverTimingStageTotal is the only Server-Timing stage RequestMiddleware
// reports: the handler total in milliseconds. The name set stays fixed at
// one stage so clients can parse it without negotiation.
const serverTimingStageTotal = "total"

// formatServerTiming renders the Server-Timing field value for one handler
// total, e.g. "total;dur=12.3". Durations live only in this field, never in
// metric labels, so no cardinality grows with request traffic.
func formatServerTiming(elapsed time.Duration) string {
	ms := elapsed.Seconds() * 1000
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%s;dur=%.1f", serverTimingStageTotal, ms)
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
//
// It also gates the Server-Timing trailer announcement. A Trailer header on a
// bodyless response is invalid with its Content-Length encoding and crashes
// Node-based proxies: the Vite dev server died with ERR_HTTP_TRAILER_INVALID
// on an announced 204, failing the SSO E2E gate. So statuses that forbid a
// body (1xx, 204, 304) and HEAD requests never announce, and a handler that
// returns without writing never triggers the announcement, letting the
// post-handler value land as a plain response header instead. Known residual:
// an explicit body-allowed status with no following body still announces, as
// before this change; holding the send for that case would re-emit a stored
// status and trip the repo's no-direct-status-write guard (see
// server_status_guard_test.go), so it stays as-is. No production instance
// exists: the only such shape in the tree, the MCP 202 in
// internal/mcp/server_sse.go, serves POST /mcp/message outside this
// middleware (only /api/ is wrapped).
type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status          int
	wroteHeader     bool
	announced       bool
	suppressTrailer bool
}

// trailerBodyAllowed reports whether a response status can carry a body, and
// hence a trailer. 1xx, 204, and 304 never have bodies regardless of what the
// handler writes.
func trailerBodyAllowed(status int) bool {
	if status >= 100 && status <= 199 {
		return false
	}
	return status != http.StatusNoContent && status != http.StatusNotModified
}

// announceTrailer adds the Trailer declaration. Callers must invoke it before
// the first bytes reach the underlying writer: afterwards the header block is
// on the wire and the declaration would be silently dropped.
func (w *statusCapturingResponseWriter) announceTrailer() {
	if w.announced || w.suppressTrailer {
		return
	}
	w.announced = true
	w.Header().Add("Trailer", "Server-Timing")
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	if !trailerBodyAllowed(status) {
		// No body can follow, so no announcement ever goes out for this
		// response. The post-handler timing value is dropped by net/http
		// along with the already-sent headers.
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.announceTrailer()
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	// Implicit status: headers are still unsent, so announcing here rides
	// the header block flushed by this first Write.
	w.announceTrailer()
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
	// A flush implies a stream follows; announce first so the declaration
	// rides the header block the flush sends.
	w.announceTrailer()
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
