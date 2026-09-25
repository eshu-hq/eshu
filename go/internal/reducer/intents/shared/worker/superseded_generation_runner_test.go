// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// supersededFakeReader adds the superseded-generation port to the runner test
// reader so a whole partition cycle can drain an orphan.
type supersededFakeReader struct {
	*fakeSharedIntentReader
	superseded map[string]struct{}
}

func (s supersededFakeReader) SupersededGenerationIDs(
	_ context.Context,
	_ []string,
) (map[string]struct{}, error) {
	return s.superseded, nil
}

// TestProcessPartitionDrainsSupersededGenerationWithReasonTelemetry drives one
// full runs_in partition cycle over an orphan on a superseded generation with no
// phase row. It must be completed (drained), counted under
// reason=generation_superseded (not acceptance_mismatch), logged with that
// reason, and must not be reported as a readiness block.
func TestProcessPartitionDrainsSupersededGenerationWithReasonTelemetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	orphan := runsInRow("repo-a", "run-old", "gen-old", now)
	reader := supersededFakeReader{
		fakeSharedIntentReader: &fakeSharedIntentReader{intents: []sharedintent.Row{orphan}},
		superseded:             map[string]struct{}{"gen-old": {}},
	}

	metricReader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &logs)

	runner := Runner{
		IntentReader: reader,
		LeaseManager: &fakeLeaseManager{granted: true},
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-old", true),
		// Readiness lookup answers "no phase row" for every key.
		ReadinessLookup: readinessLookupFixed(false, false),
		Config:          RunnerConfig{PartitionCount: 1, LeaseOwner: "test-runner"},
		Instruments:     instruments,
		Logger:          logger,
	}

	result, err := runner.processPartitionWithTelemetry(context.Background(), now, "runs_in", 0, 1)
	if err != nil {
		t.Fatalf("processPartitionWithTelemetry() error = %v", err)
	}
	if result.StaleIntents != 1 || result.SupersededGenerationIntents != 1 {
		t.Fatalf("StaleIntents = %d SupersededGenerationIntents = %d, want 1/1",
			result.StaleIntents, result.SupersededGenerationIntents)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (drained blocked rows leave BlockedRows)", result.BlockedReadiness)
	}
	if !slices.Contains(reader.marked, orphan.IntentID) {
		t.Fatalf("marked = %v, want the orphan completed", reader.marked)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := staleCounterByReason(rm); got[StaleReasonGenerationSuperseded] != 1 || got[StaleReasonAcceptanceMismatch] != 0 {
		t.Fatalf("stale counter by reason = %v, want generation_superseded=1 only", got)
	}
	out := logs.String()
	if !strings.Contains(out, "drained intents of superseded generations") ||
		!strings.Contains(out, `"stale_reason":"generation_superseded"`) {
		t.Fatalf("log missing superseded drain line with reason:\n%s", out)
	}
	if strings.Contains(out, "skipped intents") {
		t.Fatalf("superseded drain must not log a readiness skip:\n%s", out)
	}
}

func staleCounterByReason(rm metricdata.ResourceMetrics) map[string]int64 {
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_shared_projection_stale_intents_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if reason, ok := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason)); ok {
					out[reason.AsString()] += dp.Value
				}
			}
		}
	}
	return out
}
