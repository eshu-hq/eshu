package collector

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newCollisionTestInstruments returns an Instruments backed by an in-memory SDK
// reader so tests can assert counter values without a real OTEL backend.
func newCollisionTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	inst, err := telemetry.NewInstruments(mp.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return inst, reader
}

// collectBasenameCollisionTotal returns the current value of
// eshu_dp_repository_basename_collision_total from the reader.
func collectBasenameCollisionTotal(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_repository_basename_collision_total" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q: unexpected data type %T", m.Name, m.Data)
			}
			var total int64
			for _, dp := range data.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

// TestReportRepositoryBasenameCollisions_CollisionsFire verifies that when the
// same basename appears at multiple distinct paths, the warning log is emitted
// and the counter advances.
func TestReportRepositoryBasenameCollisions_CollisionsFire(t *testing.T) {
	t.Parallel()

	// Build a rooted corpus that mimics repos/repos nesting:
	//   root/
	//     repos/service-a/   (.git)
	//     repos/service-b/   (.git)
	//     repos/repos/service-a/  (.git)  ← same basename = collision
	root := t.TempDir()
	repoIDs := []string{
		"repos/service-a",
		"repos/service-b",
		"repos/repos/service-a",
	}
	for _, id := range repoIDs {
		dir := filepath.Join(root, filepath.FromSlash(id))
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newCollisionTestInstruments(t)

	reportRepositoryBasenameCollisions(context.Background(), repoIDs, logger, inst)

	// Counter must be > 0 (at least the surplus collision incremented).
	if got := collectBasenameCollisionTotal(t, reader); got == 0 {
		t.Errorf("eshu_dp_repository_basename_collision_total = 0, want > 0 for colliding corpus")
	}

	// Structured warning must mention the colliding basename and the new fields.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "repository basename collision detected (possible accidental corpus nesting)") {
		t.Errorf("expected basename collision warning log, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "service-a") {
		t.Errorf("expected log to mention 'service-a', got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "surplus_count") {
		t.Errorf("expected log to carry surplus_count field, got:\n%s", logOutput)
	}
}

// TestReportRepositoryBasenameCollisions_NoCollisionsSilent verifies that when
// all discovered repo IDs have distinct basenames, no warning is emitted and the
// counter stays at zero.
func TestReportRepositoryBasenameCollisions_NoCollisionsSilent(t *testing.T) {
	t.Parallel()

	repoIDs := []string{
		"org-a/service-a",
		"org-b/service-b",
		"platform/control-plane",
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newCollisionTestInstruments(t)

	reportRepositoryBasenameCollisions(context.Background(), repoIDs, logger, inst)

	if got := collectBasenameCollisionTotal(t, reader); got != 0 {
		t.Errorf("eshu_dp_repository_basename_collision_total = %d, want 0 for unique corpus", got)
	}
	logOutput := logBuf.String()
	if strings.Contains(logOutput, "repository basename collision") {
		t.Errorf("unexpected basename collision warning in logs:\n%s", logOutput)
	}
}

// TestReportRepositoryBasenameCollisions_NilSafe verifies that nil logger and
// nil instruments do not panic.
func TestReportRepositoryBasenameCollisions_NilSafe(t *testing.T) {
	t.Parallel()

	repoIDs := []string{
		"org/service-a",
		"nested/org/service-a",
	}
	// Must not panic with nil logger + nil instruments.
	reportRepositoryBasenameCollisions(context.Background(), repoIDs, nil, nil)
}

// TestReportRepositoryBasenameCollisions_EmptySilent verifies that an empty
// slice produces no warning and no counter increment.
func TestReportRepositoryBasenameCollisions_EmptySilent(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newCollisionTestInstruments(t)

	reportRepositoryBasenameCollisions(context.Background(), nil, logger, inst)

	if got := collectBasenameCollisionTotal(t, reader); got != 0 {
		t.Errorf("eshu_dp_repository_basename_collision_total = %d, want 0 for empty input", got)
	}
	if logBuf.Len() > 0 {
		t.Errorf("unexpected log output for empty input:\n%s", logBuf.String())
	}
}

// TestReportRepositoryBasenameCollisions_CounterMatchesSurplusCount verifies
// that the counter value equals the number of surplus (non-first) paths sharing
// a basename — and that the logged surplus_count reconciles with that delta.
func TestReportRepositoryBasenameCollisions_CounterMatchesSurplusCount(t *testing.T) {
	t.Parallel()

	// Three repos with the same basename → 2 are surplus collisions of the first.
	repoIDs := []string{
		"layer1/svc",
		"layer2/svc",
		"layer3/svc",
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newCollisionTestInstruments(t)

	reportRepositoryBasenameCollisions(context.Background(), repoIDs, logger, inst)

	// 2 surplus paths (the second and third "svc") increment the counter by 2.
	if got := collectBasenameCollisionTotal(t, reader); got != 2 {
		t.Errorf("eshu_dp_repository_basename_collision_total = %d, want 2", got)
	}
	// The log surplus_count must reconcile with the metric delta (2).
	if !strings.Contains(logBuf.String(), "surplus_count=2") {
		t.Errorf("expected surplus_count=2 in log to reconcile with metric, got:\n%s", logBuf.String())
	}
}

// TestNativeRepositorySelectorFilesystem_BasenameCollisionWarning is an
// integration test that builds a real filesystem corpus where two distinct
// paths share a basename (service-a and repos/service-a — exactly the
// accidental-nesting heuristic case), runs SelectRepositories, and confirms the
// collision warning log fires.
func TestNativeRepositorySelectorFilesystem_BasenameCollisionWarning(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()

	// Create two real repos and one that collides on basename at nested depth.
	for _, rel := range []string{
		"service-a",
		"service-b",
		"repos/service-a", // same basename as service-a → collision
	} {
		dir := filepath.Join(filesystemRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	inst, reader := newCollisionTestInstruments(t)

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
		Logger:      logger,
		Instruments: inst,
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories: %v", err)
	}

	if got := collectBasenameCollisionTotal(t, reader); got == 0 {
		t.Error("eshu_dp_repository_basename_collision_total = 0, want > 0")
	}
	if !strings.Contains(logBuf.String(), "repository basename collision detected (possible accidental corpus nesting)") {
		t.Errorf("expected basename collision warning log, got:\n%s", logBuf.String())
	}
}
