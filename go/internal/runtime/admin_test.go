package runtime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewAdminMuxRequiresServiceName(t *testing.T) {
	t.Parallel()

	_, err := NewAdminMux(AdminMuxConfig{})
	if err == nil {
		t.Fatal("NewAdminMux() error = nil, want non-nil")
	}
}

func TestAdminMuxServesHealthz(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{ServiceName: "collector-git"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "collector-git") {
		t.Fatalf("GET /healthz body = %q, want service name", got)
	}
}

func TestAdminMuxRejectsUnsupportedProbeMethod(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{ServiceName: "collector-git"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("POST /healthz status = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("POST /healthz Allow = %q, want %q", got, want)
	}
}

func TestAdminMuxServesHeadProbeWithoutBody(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{ServiceName: "collector-git"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/readyz", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("HEAD /readyz status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); got != "" {
		t.Fatalf("HEAD /readyz body = %q, want empty", got)
	}
}

func TestAdminMuxServesReadyzFailures(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{
		ServiceName: "collector-git",
		Ready: func() error {
			return errors.New("postgres unavailable")
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "postgres unavailable") {
		t.Fatalf("GET /readyz body = %q, want readiness failure", got)
	}
}

func TestAdminMuxMountsStatusHandler(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{
		ServiceName:   "collector-git",
		StatusHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("status-ok")) }),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /admin/status status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); got != "status-ok" {
		t.Fatalf("GET /admin/status body = %q, want %q", got, "status-ok")
	}
}

func TestAdminMuxMountsMetricsHandler(t *testing.T) {
	t.Parallel()

	mux := mustNewAdminMux(t, AdminMuxConfig{
		ServiceName:    "collector-git",
		MetricsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("metrics-ok")) }),
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	mux.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); got != "metrics-ok" {
		t.Fatalf("GET /metrics body = %q, want %q", got, "metrics-ok")
	}
}

func mustNewAdminMux(t *testing.T, cfg AdminMuxConfig) *http.ServeMux {
	t.Helper()

	mux, err := NewAdminMux(cfg)
	if err != nil {
		t.Fatalf("NewAdminMux() error = %v, want nil", err)
	}

	return mux
}
