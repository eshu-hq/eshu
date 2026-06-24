// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestCompositeMetricsHandlerCombinesOutput(t *testing.T) {
	t.Parallel()

	prometheusHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# HELP otel_metric_total Sample OTEL metric\n"))
		_, _ = w.Write([]byte("# TYPE otel_metric_total counter\n"))
		_, _ = w.Write([]byte("otel_metric_total 42\n"))
	})

	statusHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("eshu_runtime_queue_outstanding{service_name=\"test\"} 5\n"))
	})

	composite := compositeMetricsHandler{
		statusHandler:     statusHandler,
		prometheusHandler: prometheusHandler,
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	composite.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "otel_metric_total 42") {
		t.Errorf("composite handler body missing OTEL metrics, got: %s", body)
	}
	if !strings.Contains(body, "eshu_runtime_queue_outstanding") {
		t.Errorf("composite handler body missing status metrics, got: %s", body)
	}
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Errorf("composite handler status = %d, want %d", got, want)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("composite handler Content-Type = %q, want %q", got, want)
	}
}

func TestWithPrometheusHandlerOption(t *testing.T) {
	t.Parallel()

	prometheusHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("otel_custom_metric 100\n"))
	})

	reader := &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			Queue: statuspkg.QueueSnapshot{
				Outstanding: 3,
			},
		},
	}

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "test-service",
			ListenAddr:  "127.0.0.1:0",
		},
		reader,
		WithPrometheusHandler(prometheusHandler),
	)
	if err != nil {
		t.Fatalf("NewStatusAdminServer() error = %v, want nil", err)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	defer func() {
		_ = server.Stop(context.Background())
	}()

	response, err := http.Get("http://" + server.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v, want nil", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "otel_custom_metric 100") {
		t.Errorf("GET /metrics body missing OTEL metric, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `eshu_runtime_queue_outstanding{service_name="test-service"} 3`) {
		t.Errorf("GET /metrics body missing status metric, got: %s", bodyStr)
	}
}
