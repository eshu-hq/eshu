// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestCompositeMetricsStatusFailureKeepsTelemetryParseable(t *testing.T) {
	prometheus := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("otel_requests_total 42\n"))
	})
	status := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "load runtime metrics: reader freshness refused", http.StatusInternalServerError)
	})
	response := httptest.NewRecorder()
	NewCompositeMetricsHandler(status, prometheus).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(response.Body.String()))
	if err != nil {
		t.Fatalf("status failure corrupted independent telemetry: %v; body %q", err, response.Body.String())
	}
	if families["otel_requests_total"] == nil {
		t.Fatal("independent OTEL metrics disappeared")
	}
	if strings.Contains(response.Body.String(), "reader freshness refused") {
		t.Fatal("raw dependency error leaked into metrics text")
	}
	if got := families["eshu_runtime_status_snapshot_available"].Metric[0].GetGauge().GetValue(); got != 0 {
		t.Fatalf("availability = %v, want 0", got)
	}
}

func TestCompositeMetricsNegotiatesPrometheusText(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "otel_fixture", Help: "fixture"})
	registry.MustRegister(gauge)
	gauge.Set(7)
	status := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", "999")
		_, _ = w.Write([]byte("eshu_runtime_queue_outstanding 5\n"))
	})
	handler := NewCompositeMetricsHandler(status, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	for _, accept := range []string{
		"application/openmetrics-text; version=1.0.0",
		"application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited",
		"text/plain; version=0.0.4",
	} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.Header.Set("Accept", accept)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if got := rec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding = %q", got)
			}
			if got := rec.Header().Get("Content-Length"); got != "" {
				t.Fatalf("stale Content-Length = %q", got)
			}
			if got := req.Header.Get("Accept-Encoding"); got != "gzip" {
				t.Fatalf("caller Accept-Encoding mutated: %q", got)
			}
			if got := req.Header.Get("Accept"); got != accept {
				t.Fatalf("caller Accept mutated: %q", got)
			}
			families := parseCompositeMetrics(t, rec.Body.String())
			if families["otel_fixture"] == nil || families["eshu_runtime_queue_outstanding"] == nil {
				t.Fatalf("missing source metrics: %q", rec.Body.String())
			}
			if got := families["eshu_runtime_status_snapshot_available"].Metric[0].GetGauge().GetValue(); got != 1 {
				t.Fatalf("availability = %v, want 1", got)
			}
		})
	}
}

func TestCompositeMetricsFailureAndRecovery(t *testing.T) {
	statusFails := true
	status := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if statusFails {
			http.Error(w, "private\npassword=secret", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("eshu_runtime_queue_outstanding 5\n"))
	})
	otel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("otel_requests_total 42\n"))
	})
	handler := NewCompositeMetricsHandler(status, otel)
	for _, want := range []float64{0, 1} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		families := parseCompositeMetrics(t, rec.Body.String())
		if got := families["eshu_runtime_status_snapshot_available"].Metric[0].GetGauge().GetValue(); got != want {
			t.Fatalf("availability = %v, want %v", got, want)
		}
		if strings.Contains(rec.Body.String(), "private") || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("dependency error leaked: %q", rec.Body.String())
		}
		if want == 0 && families["eshu_runtime_queue_outstanding"] != nil {
			t.Fatal("failed snapshot values appeared")
		}
		statusFails = false
	}
}

func TestCompositeMetricsUpstreamFailure(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusServiceUnavailable, http.StatusTeapot, http.StatusMovedPermanently} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			statusCalled := false
			status := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				statusCalled = true
				http.Error(w, "status secret", http.StatusInternalServerError)
			})
			otel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Private", "secret")
				w.WriteHeader(code)
				_, _ = w.Write([]byte("password=secret\n"))
			})
			rec := httptest.NewRecorder()
			NewCompositeMetricsHandler(status, otel).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			want := code
			if code < 400 || code > 599 {
				want = http.StatusBadGateway
			}
			if rec.Code != want || statusCalled || strings.Contains(rec.Body.String(), "secret") || rec.Header().Get("X-Private") != "" {
				t.Fatalf("code=%d statusCalled=%v headers=%v body=%q", rec.Code, statusCalled, rec.Header(), rec.Body.String())
			}
		})
	}
}

func TestCompositeMetricsMethodsAndNilPassthrough(t *testing.T) {
	called := 0
	child := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called++; w.WriteHeader(http.StatusAccepted) })
	handler := NewCompositeMetricsHandler(child, child)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" || called != 0 {
		t.Fatalf("method response code=%d allow=%q called=%d", rec.Code, rec.Header().Get("Allow"), called)
	}
	for _, passthrough := range []http.Handler{NewCompositeMetricsHandler(nil, child), NewCompositeMetricsHandler(child, nil)} {
		rec := httptest.NewRecorder()
		passthrough.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("nil child did not pass through: %d", rec.Code)
		}
	}
}

func TestCompositeMetricsHeadAndCanceledStatus(t *testing.T) {
	status := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		http.Error(w, "secret", http.StatusInternalServerError)
	})
	otel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("otel_requests_total 1\n")) })
	handler := NewCompositeMetricsHandler(status, otel)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodHead, "/metrics", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestCompositeMetricsConcurrentOutcomes(t *testing.T) {
	status := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fail") == "yes" {
			http.Error(w, "secret", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("eshu_runtime_queue_outstanding 1\n"))
	})
	otel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("otel_requests_total 1\n")) })
	handler := NewCompositeMetricsHandler(status, otel)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			want := float64(1)
			if i%2 == 0 {
				req.Header.Set("X-Fail", "yes")
				want = 0
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			families := parseCompositeMetrics(t, rec.Body.String())
			if got := families["eshu_runtime_status_snapshot_available"].Metric[0].GetGauge().GetValue(); got != want {
				t.Errorf("availability = %v, want %v", got, want)
			}
		}()
	}
	wg.Wait()
}

func parseCompositeMetrics(t *testing.T, body string) map[string]*dto.MetricFamily {
	t.Helper()
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse metrics: %v; body %q", err, body)
	}
	return families
}

func BenchmarkCompositeMetricsHealthy(b *testing.B) {
	otel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("otel_requests_total 42\n"))
	})
	status := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("eshu_runtime_queue_outstanding 5\n"))
	})
	baseline := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prometheusRecorder := httptest.NewRecorder()
		otel.ServeHTTP(prometheusRecorder, r)
		statusRecorder := httptest.NewRecorder()
		status.ServeHTTP(statusRecorder, r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(prometheusRecorder.Body.Bytes())
		_, _ = w.Write([]byte("\n"))
		_, _ = w.Write(statusRecorder.Body.Bytes())
	})
	for _, candidate := range []struct {
		name    string
		handler http.Handler
	}{
		{"baseline", baseline},
		{"composite", NewCompositeMetricsHandler(status, otel)},
	} {
		b.Run(candidate.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			b.ReportAllocs()
			for range b.N {
				rec := httptest.NewRecorder()
				candidate.handler.ServeHTTP(rec, req)
			}
		})
	}
}
