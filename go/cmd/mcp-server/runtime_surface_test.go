package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type fakeStatusReader struct {
	snapshot statuspkg.RawSnapshot
	err      error
}

func (f fakeStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (statuspkg.RawSnapshot, error) {
	if f.err != nil {
		return statuspkg.RawSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func TestMountRuntimeSurfaceServesSharedAdminRoutes(t *testing.T) {
	t.Parallel()

	handler, err := mountRuntimeSurface(
		"mcp-server",
		fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			},
		},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("# otel metric\n"))
		}),
	)
	if err != nil {
		t.Fatalf("mountRuntimeSurface() error = %v, want nil", err)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if got, want := healthRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if got, want := readyRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/admin/status?format=json", nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if got, want := statusRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /admin/status status = %d, want %d", got, want)
	}
	if got := statusRec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("GET /admin/status Content-Type = %q, want application/json", got)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)
	if got, want := metricsRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if got := metricsRec.Body.String(); !strings.Contains(got, "eshu_runtime_info") {
		t.Fatalf("GET /metrics body = %q, want runtime metrics", got)
	}
	if got := metricsRec.Body.String(); !strings.Contains(got, "# otel metric") {
		t.Fatalf("GET /metrics body = %q, want otel metrics", got)
	}
}
