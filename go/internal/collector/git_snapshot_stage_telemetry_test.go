package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSnapshotRepositoryRecordsPerStageTelemetry proves the git-collector
// snapshot path emits the eshu_dp_collector_snapshot_stage_duration_seconds
// histogram and a collector.snapshot_stage span for every bounded stage,
// including the value-flow evidence stage that was previously untimed (#3586).
// Before this telemetry an operator could see a slow repository through the
// whole-snapshot histogram but could not attribute the cost to a stage.
func TestSnapshotRepositoryRecordsPerStageTelemetry(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "app.py"),
		"def handler():\n    return 1\n\nclass Worker:\n    pass\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-stage-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("collector-stage-test")

	now := time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{
		Engine:      engine,
		Instruments: instruments,
		Tracer:      tracer,
		Now:         func() time.Time { return now },
	}

	if _, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot, RemoteURL: "https://github.com/example/service"},
	); err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	wantStages := []string{
		telemetry.SnapshotStageDiscovery,
		telemetry.SnapshotStagePreScan,
		telemetry.SnapshotStageGoPackageSemanticPreScan,
		telemetry.SnapshotStageParse,
		telemetry.SnapshotStageMaterialize,
		telemetry.SnapshotStageValueFlowEvidence,
	}
	for _, stage := range wantStages {
		got := scipHistogramCount(t, rm, "eshu_dp_collector_snapshot_stage_duration_seconds", map[string]string{
			"collector_kind": "git",
			"stage":          stage,
		})
		if got != 1 {
			t.Fatalf(
				"eshu_dp_collector_snapshot_stage_duration_seconds{collector_kind=git,stage=%s} count = %d, want 1",
				stage,
				got,
			)
		}
	}

	stageSpanCount := 0
	for _, span := range spanRecorder.Ended() {
		if span.Name() == telemetry.SpanCollectorSnapshotStage {
			stageSpanCount++
		}
	}
	if stageSpanCount != len(wantStages) {
		t.Fatalf(
			"collector.snapshot_stage span count = %d, want %d",
			stageSpanCount,
			len(wantStages),
		)
	}
}

// TestSnapshotRepositoryStageTelemetryNoInstrumentsNoPanic proves the snapshot
// path stays safe when telemetry is not configured: no Instruments and no
// Tracer must not panic and must still produce a snapshot.
func TestSnapshotRepositoryStageTelemetryNoInstrumentsNoPanic(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "app.py"),
		"def handler():\n    return 1\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	if _, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	); err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
}
