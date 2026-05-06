package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestNewStatusAdminServerServesStatusAndReadyChecks(t *testing.T) {
	t.Parallel()

	reader := &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	}

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "collector-git",
			ListenAddr:  "127.0.0.1:0",
		},
		reader,
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

	statusResponse, err := http.Get("http://" + server.Addr() + "/admin/status?format=json")
	if err != nil {
		t.Fatalf("GET /admin/status error = %v, want nil", err)
	}
	defer func() {
		_ = statusResponse.Body.Close()
	}()
	if got, want := statusResponse.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET /admin/status status = %d, want %d", got, want)
	}

	readyResponse, err := http.Get("http://" + server.Addr() + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v, want nil", err)
	}
	defer func() {
		_ = readyResponse.Body.Close()
	}()
	if got, want := readyResponse.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}
}

func TestNewStatusAdminServerServesMetrics(t *testing.T) {
	t.Parallel()

	reader := &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: statuspkg.ScopeActivitySnapshot{
				Active:  5,
				Changed: 2,
			},
			Queue: statuspkg.QueueSnapshot{
				Outstanding:          2,
				OldestOutstandingAge: 30 * time.Second,
			},
		},
	}

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "collector-git",
			ListenAddr:  "127.0.0.1:0",
		},
		reader,
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

	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if got := string(body); !strings.Contains(got, `eshu_runtime_queue_outstanding{service_name="collector-git"} 2`) {
		t.Fatalf("GET /metrics body = %q, want queue metric", got)
	}
	if got := string(body); !strings.Contains(got, `eshu_runtime_scope_active{service_name="collector-git"} 5`) {
		t.Fatalf("GET /metrics body = %q, want scope activity metric", got)
	}
}

func TestNewStatusMetricsServerServesDedicatedMetricsPort(t *testing.T) {
	t.Parallel()

	reader := &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: statuspkg.ScopeActivitySnapshot{
				Active: 3,
			},
			Queue: statuspkg.QueueSnapshot{
				Outstanding: 1,
			},
		},
	}

	server, err := NewStatusMetricsServer(
		Config{
			ServiceName: "workflow-coordinator",
			MetricsAddr: "127.0.0.1:0",
		},
		reader,
	)
	if err != nil {
		t.Fatalf("NewStatusMetricsServer() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatal("NewStatusMetricsServer() = nil, want server")
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

	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if got := string(body); !strings.Contains(got, `eshu_runtime_queue_outstanding{service_name="workflow-coordinator"} 1`) {
		t.Fatalf("GET /metrics body = %q, want queue metric", got)
	}
	if got := string(body); !strings.Contains(got, `eshu_runtime_scope_active{service_name="workflow-coordinator"} 3`) {
		t.Fatalf("GET /metrics body = %q, want scope metric", got)
	}
}

func TestNewStatusAdminServerSurfacesReaderFailureThroughReadyz(t *testing.T) {
	t.Parallel()

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "collector-git",
			ListenAddr:  "127.0.0.1:0",
		},
		&fakeStatusReader{err: errors.New("postgres unavailable")},
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

	response, err := http.Get("http://" + server.Addr() + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v, want nil", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if got, want := response.StatusCode, http.StatusServiceUnavailable; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}
	if got := string(body); got == "" {
		t.Fatal("GET /readyz body = empty, want readiness error details")
	}
}

func TestNewStatusAdminServerWithRecoveryMountsRecoveryRoutes(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStoreForStatus{
		replayResult: recovery.ReplayResult{
			Stage:       recovery.StageProjector,
			Replayed:    1,
			WorkItemIDs: []string{"item-1"},
		},
	}
	recoveryHandler := mustBuildRecoveryHandlerForStatus(t, store)

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "ingester",
			ListenAddr:  "127.0.0.1:0",
		},
		&fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			},
		},
		WithRecoveryHandler(recoveryHandler),
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

	body, _ := json.Marshal(map[string]any{"stage": "projector"})
	resp, err := http.Post(
		"http://"+server.Addr()+"/admin/replay",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		t.Fatalf("POST /admin/replay error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /admin/replay status = %d, want %d; body = %s", got, want, respBody)
	}

	var result replayResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("json.Decode() error = %v", err)
	}
	if result.Status != "replayed" {
		t.Fatalf("response status = %q, want %q", result.Status, "replayed")
	}
	if result.Replayed != 1 {
		t.Fatalf("response replayed = %d, want 1", result.Replayed)
	}
}

func TestNewStatusAdminServerWithRecoveryMountsRefinalizeRoute(t *testing.T) {
	t.Parallel()

	store := &fakeRecoveryStoreForStatus{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued: 2,
			ScopeIDs: []string{"s1", "s2"},
		},
	}
	recoveryHandler := mustBuildRecoveryHandlerForStatus(t, store)

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "ingester",
			ListenAddr:  "127.0.0.1:0",
		},
		&fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			},
		},
		WithRecoveryHandler(recoveryHandler),
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

	body, _ := json.Marshal(map[string]any{"scope_ids": []string{"s1", "s2"}})
	resp, err := http.Post(
		"http://"+server.Addr()+"/admin/refinalize",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		t.Fatalf("POST /admin/refinalize error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /admin/refinalize status = %d, want %d; body = %s", got, want, respBody)
	}

	var result refinalizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("json.Decode() error = %v", err)
	}
	if result.Enqueued != 2 {
		t.Fatalf("response enqueued = %d, want 2", result.Enqueued)
	}
}

func TestNewStatusAdminServerWithoutRecoveryReturns404ForRecoveryRoutes(t *testing.T) {
	t.Parallel()

	server, err := NewStatusAdminServer(
		Config{
			ServiceName: "ingester",
			ListenAddr:  "127.0.0.1:0",
		},
		&fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			},
		},
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

	resp, err := http.Post(
		"http://"+server.Addr()+"/admin/replay",
		"application/json",
		strings.NewReader(`{"stage":"projector"}`),
	)
	if err != nil {
		t.Fatalf("POST /admin/replay error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if got, want := resp.StatusCode, http.StatusNotFound; got != want {
		t.Fatalf("POST /admin/replay without recovery status = %d, want %d", got, want)
	}
}

// --- recovery test helpers ---

func mustBuildRecoveryHandlerForStatus(t *testing.T, store recovery.ReplayStore) *RecoveryHandler {
	t.Helper()

	handler, err := recovery.NewHandler(store)
	if err != nil {
		t.Fatalf("recovery.NewHandler() error = %v", err)
	}

	rh, err := NewRecoveryHandler(handler)
	if err != nil {
		t.Fatalf("NewRecoveryHandler() error = %v", err)
	}

	return rh
}

type fakeRecoveryStoreForStatus struct {
	replayResult     recovery.ReplayResult
	replayErr        error
	refinalizeResult recovery.RefinalizeResult
	refinalizeErr    error
}

func (s *fakeRecoveryStoreForStatus) ReplayFailedWorkItems(
	_ context.Context,
	_ recovery.ReplayFilter,
	_ time.Time,
) (recovery.ReplayResult, error) {
	return s.replayResult, s.replayErr
}

func (s *fakeRecoveryStoreForStatus) RefinalizeScopeProjections(
	_ context.Context,
	_ recovery.RefinalizeFilter,
	_ time.Time,
) (recovery.RefinalizeResult, error) {
	return s.refinalizeResult, s.refinalizeErr
}

// --- existing stubs ---

type fakeStatusReader struct {
	snapshot statuspkg.RawSnapshot
	err      error
}

func (r *fakeStatusReader) ReadStatusSnapshot(context.Context, time.Time) (statuspkg.RawSnapshot, error) {
	if r.err != nil {
		return statuspkg.RawSnapshot{}, r.err
	}
	return r.snapshot, nil
}

func TestCompositeMetricsHandlerCombinesOutput(t *testing.T) {
	t.Parallel()

	// Create fake prometheus handler that returns sample OTEL metrics
	prometheusHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# HELP otel_metric_total Sample OTEL metric\n"))
		_, _ = w.Write([]byte("# TYPE otel_metric_total counter\n"))
		_, _ = w.Write([]byte("otel_metric_total 42\n"))
	})

	// Create fake status handler that returns sample status metrics
	statusHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// Verify both outputs are present
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

	prometheusHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
