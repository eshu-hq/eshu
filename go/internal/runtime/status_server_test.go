package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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

func (s *fakeRecoveryStoreForStatus) ReplayCollectorGenerations(
	context.Context,
	recovery.CollectorGenerationReplayFilter,
	time.Time,
) (recovery.CollectorGenerationReplayResult, error) {
	return recovery.CollectorGenerationReplayResult{}, nil
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

func (r *fakeStatusReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	_ statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshot(ctx, asOf)
}
