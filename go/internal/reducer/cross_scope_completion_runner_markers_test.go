// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCrossScopeCompletionRunnerCountsSkippedManifestMarkers pins the #10972
// operator signal: each committed fan-out adds the number of manifest-marker
// consumers it left alone to eshu_dp_cross_scope_completion_markers_skipped_total
// under the producer domain, and a zero count adds nothing.
func TestCrossScopeCompletionRunnerCountsSkippedManifestMarkers(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	for _, skipped := range []int{3, 0, 4} {
		queue := &fakeCrossScopeCompletionQueue{
			claimed: true,
			lease: CrossScopeCompletionLease{
				EventID: 7, ProducerDomain: DomainCICDRunCorrelation, ClaimEpoch: 1, AttemptCount: 1,
			},
			fanoutResult: CrossScopeCompletionResult{EventsProcessed: 1, ManifestMarkersSkipped: skipped},
		}
		runner := CrossScopeCompletionRunner{Queue: queue, LeaseOwner: "runner-test", Instruments: instruments}
		if _, _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_cross_scope_completion_markers_skipped_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if domain, _ := point.Attributes.Value(telemetry.MetricDimensionDomain); domain.AsString() != string(DomainCICDRunCorrelation) {
					t.Fatalf("skipped-marker point domain = %q, want the producer domain", domain.AsString())
				}
				total += point.Value
			}
		}
	}
	if total != 7 {
		t.Fatalf("skipped-marker counter = %d, want 7 (3 + 0 + 4)", total)
	}
}

// TestCrossScopeCompletionRunnerLogsSkippedManifestMarkers: the per-event log
// line carries the skipped count, so one event's spared markers are visible
// without the metric.
func TestCrossScopeCompletionRunnerLogsSkippedManifestMarkers(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	queue := &drainingMarkerQueue{}
	runner := CrossScopeCompletionRunner{
		Queue: queue, LeaseOwner: "runner-test", Logger: logger,
		Wait: func(ctx context.Context, _ time.Duration) error { return context.Canceled },
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(logs.String(), "manifest_markers_skipped=5") {
		t.Fatalf("fan-out log = %q, want manifest_markers_skipped=5", logs.String())
	}
}

// drainingMarkerQueue serves one event whose fan-out skipped five markers.
type drainingMarkerQueue struct {
	drainingCrossScopeCompletionQueue
}

func (*drainingMarkerQueue) Fanout(
	context.Context, CrossScopeCompletionLease, int,
) (CrossScopeCompletionResult, error) {
	return CrossScopeCompletionResult{EventsProcessed: 1, ManifestMarkersSkipped: 5}, nil
}
