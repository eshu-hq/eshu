package query

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// resetAPIRequestMetricsForTest rebinds the lazily registered request
// instruments so a test can register them against its own meter provider
// regardless of test ordering.
func resetAPIRequestMetricsForTest() {
	apiRequestInstrumentsOnce = sync.Once{}
	apiRequestInstrumentsVal = nil
}

func TestRequestMetricsMiddlewareEmitsPerEndpointMetrics(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		t.Fatalf("otelprom.New() error = %v", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous) })
	resetAPIRequestMetricsForTest()
	t.Cleanup(resetAPIRequestMetricsForTest)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/v0/boom", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	handler := RequestMetricsMiddleware(mux)

	// Exercise a success, a server error, and an unmatched route.
	for _, target := range []string{"/api/v0/ok", "/api/v0/boom", "/api/v0/missing"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	}

	scrape := scrapeMetrics(t, registry)

	// Duration histogram emits per-route series including status class.
	for _, want := range []string{
		`eshu_dp_api_request_duration_seconds_count{`,
		`route="GET /api/v0/ok"`,
		`status_class="2xx"`,
		`route="GET /api/v0/boom"`,
		`status_class="5xx"`,
		`route="unmatched"`,
	} {
		if !strings.Contains(scrape, want) {
			t.Fatalf("scrape missing %q\n--- scrape ---\n%s", want, scrape)
		}
	}

	// Server errors increment the error counter for the failing route only.
	if !strings.Contains(scrape, `eshu_dp_api_request_errors_total{`) {
		t.Fatalf("scrape missing eshu_dp_api_request_errors_total\n%s", scrape)
	}
	if strings.Contains(scrape, `eshu_dp_api_request_errors_total{`) &&
		!strings.Contains(scrape, `route="GET /api/v0/boom"`) {
		t.Fatalf("error counter missing failing route label\n%s", scrape)
	}
}

func TestRequestMetricsMiddlewareNilMuxIsNotFound(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	RequestMetricsMiddleware(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil mux status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStatusCapturingResponseWriterDefaultsTo200(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w := &statusCapturingResponseWriter{ResponseWriter: rec, status: http.StatusOK}
	_, _ = w.Write([]byte("body without explicit WriteHeader"))
	if w.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 when handler writes a body without WriteHeader", w.status)
	}
	if w.Unwrap() != rec {
		t.Fatal("Unwrap() should return the underlying ResponseWriter")
	}
}

// flushHijackRecorder is a minimal http.ResponseWriter that also implements
// http.Flusher and http.Hijacker so a test can assert the metrics wrapper
// forwards those optional streaming interfaces to the underlying writer.
type flushHijackRecorder struct {
	header   http.Header
	status   int
	flushed  bool
	hijacked bool
	buf      strings.Builder
}

func (w *flushHijackRecorder) Header() http.Header         { return w.header }
func (w *flushHijackRecorder) WriteHeader(code int)        { w.status = code }
func (w *flushHijackRecorder) Write(b []byte) (int, error) { return w.buf.Write(b) }
func (w *flushHijackRecorder) Flush()                      { w.flushed = true }
func (w *flushHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, nil
}

// TestStatusCapturingResponseWriterForwardsFlush proves the metrics wrapper
// preserves http.Flusher so SSE handlers (handleAskSSE) can stream. Issue #3381:
// the wrapper previously embedded http.ResponseWriter without a Flush method, so
// w.(http.Flusher) failed and the Ask SSE endpoint returned 500.
func TestStatusCapturingResponseWriterForwardsFlush(t *testing.T) {
	t.Parallel()

	underlying := &flushHijackRecorder{header: make(http.Header)}
	w := &statusCapturingResponseWriter{ResponseWriter: underlying, status: http.StatusOK}

	flusher, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("statusCapturingResponseWriter must implement http.Flusher")
	}
	flusher.Flush()
	if !underlying.flushed {
		t.Fatal("Flush() must forward to the underlying ResponseWriter")
	}
}

// TestStatusCapturingResponseWriterForwardsHijack proves the wrapper preserves
// http.Hijacker when the underlying writer supports it, so connection-upgrade
// handlers keep working behind the metrics middleware.
func TestStatusCapturingResponseWriterForwardsHijack(t *testing.T) {
	t.Parallel()

	underlying := &flushHijackRecorder{header: make(http.Header)}
	w := &statusCapturingResponseWriter{ResponseWriter: underlying, status: http.StatusOK}

	hijacker, ok := any(w).(http.Hijacker)
	if !ok {
		t.Fatal("statusCapturingResponseWriter must implement http.Hijacker")
	}
	if _, _, err := hijacker.Hijack(); err != nil {
		t.Fatalf("Hijack() error = %v", err)
	}
	if !underlying.hijacked {
		t.Fatal("Hijack() must forward to the underlying ResponseWriter")
	}
}

// TestStatusCapturingResponseWriterFlushWithoutUnderlyingFlusher proves Flush is
// a safe no-op when the underlying writer is not an http.Flusher, so wrapping a
// non-streaming writer never panics.
func TestStatusCapturingResponseWriterFlushWithoutUnderlyingFlusher(t *testing.T) {
	t.Parallel()

	underlying := &noFlushWriter{header: make(http.Header)}
	w := &statusCapturingResponseWriter{ResponseWriter: underlying, status: http.StatusOK}

	flusher, ok := any(w).(http.Flusher)
	if !ok {
		t.Fatal("statusCapturingResponseWriter must implement http.Flusher")
	}
	flusher.Flush() // must not panic when underlying lacks Flush
}

// TestAskSSE_StreamsThroughMetricsMiddleware is the end-to-end regression for
// issue #3381: POST /api/v0/ask with Accept: text/event-stream served behind
// RequestMetricsMiddleware must stream a 200 event stream, not a 500 "streaming
// not supported by this server configuration" error.
func TestAskSSE_StreamsThroughMetricsMiddleware(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{
		answer: AskAnswer{Prose: "streamed answer", Narrated: true},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/ask", h.handleAsk)
	handler := RequestMetricsMiddleware(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(`{"question":"stream check"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream; body=%s", ct, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: answer") {
		t.Fatalf("stream missing answer event; body=%s", rec.Body.String())
	}
}

func scrapeMetrics(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}
