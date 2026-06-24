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

// newDupTestInstruments returns an Instruments backed by an in-memory SDK reader
// so tests can assert counter values without a real OTEL backend.
func newDupTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
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

// collectDupRepoTotal returns the current value of
// eshu_dp_duplicate_repository_identity_total from the reader.
func collectDupRepoTotal(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_duplicate_repository_identity_total" {
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

// TestReportDuplicateRepoIdentities_DuplicatesFire verifies that when the same
// basename appears at multiple distinct paths, the warning log is emitted and
// the counter advances.
func TestReportDuplicateRepoIdentities_DuplicatesFire(t *testing.T) {
	t.Parallel()

	// Build a rooted corpus that mimics repos/repos nesting:
	//   root/
	//     repos/service-a/   (.git)
	//     repos/service-b/   (.git)
	//     repos/repos/service-a/  (.git)  ← same basename = duplicate
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
	inst, reader := newDupTestInstruments(t)

	reportDuplicateRepoIdentities(context.Background(), repoIDs, logger, inst)

	// Counter must be > 0 (at least the duplicate count incremented).
	if got := collectDupRepoTotal(t, reader); got == 0 {
		t.Errorf("eshu_dp_duplicate_repository_identity_total = 0, want > 0 for duplicate corpus")
	}

	// Structured warning must mention the duplicated basename.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "duplicate repository identity detected") {
		t.Errorf("expected duplicate warning log, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "service-a") {
		t.Errorf("expected log to mention 'service-a', got:\n%s", logOutput)
	}
}

// TestReportDuplicateRepoIdentities_NoDuplicatesSilent verifies that when all
// discovered repo IDs have distinct basenames, no warning is emitted and the
// counter stays at zero.
func TestReportDuplicateRepoIdentities_NoDuplicatesSilent(t *testing.T) {
	t.Parallel()

	repoIDs := []string{
		"org-a/service-a",
		"org-b/service-b",
		"platform/control-plane",
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newDupTestInstruments(t)

	reportDuplicateRepoIdentities(context.Background(), repoIDs, logger, inst)

	if got := collectDupRepoTotal(t, reader); got != 0 {
		t.Errorf("eshu_dp_duplicate_repository_identity_total = %d, want 0 for unique corpus", got)
	}
	logOutput := logBuf.String()
	if strings.Contains(logOutput, "duplicate repository identity") {
		t.Errorf("unexpected duplicate warning in logs:\n%s", logOutput)
	}
}

// TestReportDuplicateRepoIdentities_NilSafe verifies that nil logger and nil
// instruments do not panic.
func TestReportDuplicateRepoIdentities_NilSafe(t *testing.T) {
	t.Parallel()

	repoIDs := []string{
		"org/service-a",
		"nested/org/service-a",
	}
	// Must not panic with nil logger + nil instruments.
	reportDuplicateRepoIdentities(context.Background(), repoIDs, nil, nil)
}

// TestReportDuplicateRepoIdentities_EmptySilent verifies that an empty slice
// produces no warning and no counter increment.
func TestReportDuplicateRepoIdentities_EmptySilent(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newDupTestInstruments(t)

	reportDuplicateRepoIdentities(context.Background(), nil, logger, inst)

	if got := collectDupRepoTotal(t, reader); got != 0 {
		t.Errorf("eshu_dp_duplicate_repository_identity_total = %d, want 0 for empty input", got)
	}
	if logBuf.Len() > 0 {
		t.Errorf("unexpected log output for empty input:\n%s", logBuf.String())
	}
}

// TestReportDuplicateRepoIdentities_CounterMatchesDuplicateCount verifies
// that the counter value equals the number of duplicate-identity repos
// (i.e. repos at paths where the same basename was already seen).
func TestReportDuplicateRepoIdentities_CounterMatchesDuplicateCount(t *testing.T) {
	t.Parallel()

	// Three repos with the same basename → 2 are duplicates of the first.
	repoIDs := []string{
		"layer1/svc",
		"layer2/svc",
		"layer3/svc",
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inst, reader := newDupTestInstruments(t)

	reportDuplicateRepoIdentities(context.Background(), repoIDs, logger, inst)

	// 2 duplicate paths (the second and third "svc") should increment the counter by 2.
	if got := collectDupRepoTotal(t, reader); got != 2 {
		t.Errorf("eshu_dp_duplicate_repository_identity_total = %d, want 2", got)
	}
}

// TestNativeRepositorySelectorFilesystem_DuplicateRepoWarning is an integration
// test that builds a real filesystem corpus with a nested repos/repos/ duplication,
// runs SelectRepositories, and confirms the duplicate warning log fires.
func TestNativeRepositorySelectorFilesystem_DuplicateRepoWarning(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()

	// Create two real repos and one duplicate at nested depth.
	for _, rel := range []string{
		"service-a",
		"service-b",
		"repos/service-a", // same basename as service-a → duplicate
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
	inst, reader := newDupTestInstruments(t)

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

	if got := collectDupRepoTotal(t, reader); got == 0 {
		t.Error("eshu_dp_duplicate_repository_identity_total = 0, want > 0")
	}
	if !strings.Contains(logBuf.String(), "duplicate repository identity detected") {
		t.Errorf("expected duplicate warning log, got:\n%s", logBuf.String())
	}
}
