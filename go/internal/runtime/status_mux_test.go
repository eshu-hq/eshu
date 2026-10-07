// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestNewStatusAdminMuxMountsApplicationHandler(t *testing.T) {
	t.Parallel()

	appMux := http.NewServeMux()
	appMux.HandleFunc("GET /api/v0/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"openapi":"3.0.3"}`))
	})

	mux, err := NewStatusAdminMux(
		"eshu-api",
		&fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			},
		},
		appMux,
		WithPrometheusHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("# otel metric\n"))
		})),
	)
	if err != nil {
		t.Fatalf("NewStatusAdminMux() error = %v, want nil", err)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, healthReq)
	if got, want := healthRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	mux.ServeHTTP(metricsRec, metricsReq)
	if got, want := metricsRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if got := metricsRec.Body.String(); !strings.Contains(got, "eshu_runtime_info") {
		t.Fatalf("GET /metrics body = %q, want runtime metrics", got)
	}
	if got := metricsRec.Body.String(); !strings.Contains(got, "# otel metric") {
		t.Fatalf("GET /metrics body = %q, want otel metrics", got)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil)
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)
	if got, want := apiRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/openapi.json status = %d, want %d", got, want)
	}
}

// startupFailingStatusReader reports an invalid configuration at startup, as a
// postgres.StatusStore does for an invalid ESHU_STATUS_SUMMARY_STALE_AFTER.
type startupFailingStatusReader struct {
	fakeStatusReader
	err error
}

func (r *startupFailingStatusReader) StartupError() error { return r.err }

// TestStatusConstructorsFailOnAReaderStartupError: every runtime that serves a
// status endpoint builds it through these constructors, so a reader that
// reports a configuration error fails the process at startup through each one.
func TestStatusConstructorsFailOnAReaderStartupError(t *testing.T) {
	t.Parallel()

	boom := errors.New("ESHU_STATUS_SUMMARY_STALE_AFTER=\"soon\": invalid")
	failing := &startupFailingStatusReader{err: boom}
	healthy := &startupFailingStatusReader{}
	cfg := Config{ServiceName: "webhook-listener", ListenAddr: "127.0.0.1:0", MetricsAddr: "127.0.0.1:0"}
	for name, build := range map[string]func(statuspkg.Reader) error{
		"admin mux": func(r statuspkg.Reader) error {
			_, err := NewStatusAdminMux("svc", r, nil)
			return err
		},
		"admin server": func(r statuspkg.Reader) error {
			_, err := NewStatusAdminServer(cfg, r)
			return err
		},
		"metrics server": func(r statuspkg.Reader) error {
			_, err := NewStatusMetricsServer(cfg, r)
			return err
		},
		"metrics handler": func(r statuspkg.Reader) error {
			_, err := NewStatusMetricsHandler("svc", r)
			return err
		},
	} {
		if err := build(failing); !errors.Is(err, boom) {
			t.Fatalf("%s: error = %v, want the reader's startup error", name, err)
		}
		if err := build(healthy); err != nil {
			t.Fatalf("%s: healthy reader error = %v, want nil", name, err)
		}
	}
}
