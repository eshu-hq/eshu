package query

import (
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
