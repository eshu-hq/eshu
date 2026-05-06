package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestNewWiresBootstrapForService(t *testing.T) {
	t.Parallel()

	got, err := New("collector-git")
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if got.Config.ServiceName != "collector-git" {
		t.Fatalf("Config.ServiceName = %q, want %q", got.Config.ServiceName, "collector-git")
	}

	if got.Config.Command != "collector-git" {
		t.Fatalf("Config.Command = %q, want %q", got.Config.Command, "collector-git")
	}

	if got.Config.ListenAddr != "0.0.0.0:8080" {
		t.Fatalf("Config.ListenAddr = %q, want %q", got.Config.ListenAddr, "0.0.0.0:8080")
	}

	if got.Config.MetricsAddr != "0.0.0.0:9464" {
		t.Fatalf("Config.MetricsAddr = %q, want %q", got.Config.MetricsAddr, "0.0.0.0:9464")
	}

	lifecycle, ok := got.Lifecycle.(runtime.Lifecycle)
	if !ok {
		t.Fatalf("Lifecycle type = %T, want runtime.Lifecycle", got.Lifecycle)
	}

	if lifecycle.ServiceName != "collector-git" {
		t.Fatalf("Lifecycle.ServiceName = %q, want %q", lifecycle.ServiceName, "collector-git")
	}

	if _, ok := got.Runner.(runtime.ContextRunner); !ok {
		t.Fatalf("Runner type = %T, want runtime.ContextRunner", got.Runner)
	}
}

func TestNewWiresObservabilityContract(t *testing.T) {
	t.Parallel()

	got, err := New("projector")
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if got.Observability.MetricDimensions[0] != telemetry.MetricDimensionScopeID {
		t.Fatalf("Observability.MetricDimensions[0] = %q, want %q", got.Observability.MetricDimensions[0], telemetry.MetricDimensionScopeID)
	}

	if got.Observability.SpanNames[4] != telemetry.SpanProjectorRun {
		t.Fatalf("Observability.SpanNames[4] = %q, want %q", got.Observability.SpanNames[4], telemetry.SpanProjectorRun)
	}

	if !slices.Equal(got.Observability.LogKeys, telemetry.LogKeys()) {
		t.Fatalf("Observability.LogKeys = %v, want %v", got.Observability.LogKeys, telemetry.LogKeys())
	}

	got.Observability.MetricDimensions[0] = "mutated"
	if telemetry.MetricDimensionKeys()[0] != telemetry.MetricDimensionScopeID {
		t.Fatalf("telemetry contract was mutated through bootstrap seam")
	}
}

func TestRunStartsLifecycleRunsServiceAndStopsLifecycle(t *testing.T) {
	t.Parallel()

	lifecycle := &stubLifecycle{}
	runner := &stubRunner{}

	app := Application{
		Lifecycle: lifecycle,
		Runner:    runner,
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := lifecycle.startCalls, 1; got != want {
		t.Fatalf("lifecycle start calls = %d, want %d", got, want)
	}
	if got, want := runner.runCalls, 1; got != want {
		t.Fatalf("runner run calls = %d, want %d", got, want)
	}
	if got, want := lifecycle.stopCalls, 1; got != want {
		t.Fatalf("lifecycle stop calls = %d, want %d", got, want)
	}
}

func TestRunReturnsRunnerErrorAfterStoppingLifecycle(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("runner failed")
	lifecycle := &stubLifecycle{}
	runner := &stubRunner{runErr: wantErr}

	app := Application{
		Lifecycle: lifecycle,
		Runner:    runner,
	}

	if err := app.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	if got, want := lifecycle.stopCalls, 1; got != want {
		t.Fatalf("lifecycle stop calls = %d, want %d", got, want)
	}
}

func TestComposeLifecyclesStartsAndStopsBoth(t *testing.T) {
	t.Parallel()

	first := &stubLifecycle{}
	second := &stubLifecycle{}
	lifecycle := ComposeLifecycles(first, second)
	runner := &stubRunner{}

	app := Application{
		Lifecycle: lifecycle,
		Runner:    runner,
	}

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := first.startCalls, 1; got != want {
		t.Fatalf("first start calls = %d, want %d", got, want)
	}
	if got, want := second.startCalls, 1; got != want {
		t.Fatalf("second start calls = %d, want %d", got, want)
	}
	if got, want := second.stopCalls, 1; got != want {
		t.Fatalf("second stop calls = %d, want %d", got, want)
	}
	if got, want := first.stopCalls, 1; got != want {
		t.Fatalf("first stop calls = %d, want %d", got, want)
	}
}

func TestNewHostedWithStatusServerRejectsNilReader(t *testing.T) {
	t.Parallel()

	_, err := NewHostedWithStatusServer("collector-git", runtime.ContextRunner{}, nil)
	if err == nil {
		t.Fatal("NewHostedWithStatusServer() error = nil, want non-nil")
	}
}

func TestMountStatusServerComposesRuntimeLifecycle(t *testing.T) {
	t.Setenv("ESHU_LISTEN_ADDR", "127.0.0.1:0")

	base, err := NewHosted("collector-git", runtime.ContextRunner{})
	if err != nil {
		t.Fatalf("NewHosted() error = %v, want nil", err)
	}

	mounted, err := MountStatusServer(base, &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("MountStatusServer() error = %v, want nil", err)
	}

	if err := mounted.Lifecycle.Start(context.Background()); err != nil {
		t.Fatalf("Lifecycle.Start() error = %v, want nil", err)
	}
	if err := mounted.Lifecycle.Stop(context.Background()); err != nil {
		t.Fatalf("Lifecycle.Stop() error = %v, want nil", err)
	}
}

func TestMountStatusServerStartsDedicatedMetricsServer(t *testing.T) {
	t.Setenv("ESHU_LISTEN_ADDR", reserveTCPAddress(t))
	t.Setenv("ESHU_METRICS_ADDR", reserveTCPAddress(t))

	base, err := NewHosted("workflow-coordinator", runtime.ContextRunner{})
	if err != nil {
		t.Fatalf("NewHosted() error = %v, want nil", err)
	}

	mounted, err := MountStatusServer(base, &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: statuspkg.QueueSnapshot{
				Outstanding: 2,
			},
		},
	})
	if err != nil {
		t.Fatalf("MountStatusServer() error = %v, want nil", err)
	}

	if err := mounted.Lifecycle.Start(context.Background()); err != nil {
		t.Fatalf("Lifecycle.Start() error = %v, want nil", err)
	}
	defer func() {
		_ = mounted.Lifecycle.Stop(context.Background())
	}()

	statusResponse, err := http.Get("http://" + mounted.Config.ListenAddr + "/admin/status?format=json")
	if err != nil {
		t.Fatalf("GET admin status error = %v, want nil", err)
	}
	defer func() {
		_ = statusResponse.Body.Close()
	}()
	if got, want := statusResponse.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET admin status code = %d, want %d", got, want)
	}

	metricsResponse, err := http.Get("http://" + mounted.Config.MetricsAddr + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics error = %v, want nil", err)
	}
	defer func() {
		_ = metricsResponse.Body.Close()
	}()
	body, err := io.ReadAll(metricsResponse.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if got, want := metricsResponse.StatusCode, http.StatusOK; got != want {
		t.Fatalf("GET metrics code = %d, want %d", got, want)
	}
	if got := string(body); !strings.Contains(got, `eshu_runtime_queue_outstanding{service_name="workflow-coordinator"} 2`) {
		t.Fatalf("GET metrics body = %q, want queue metric", got)
	}
}

type stubLifecycle struct {
	startCalls int
	stopCalls  int
}

func (l *stubLifecycle) Start(context.Context) error {
	l.startCalls++
	return nil
}

func (l *stubLifecycle) Stop(context.Context) error {
	l.stopCalls++
	return nil
}

type stubRunner struct {
	runCalls int
	runErr   error
}

func (r *stubRunner) Run(context.Context) error {
	r.runCalls++
	return r.runErr
}

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

func reserveTCPAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v, want nil", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v, want nil", err)
	}
	return addr
}
