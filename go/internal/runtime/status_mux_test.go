package runtime

import (
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
