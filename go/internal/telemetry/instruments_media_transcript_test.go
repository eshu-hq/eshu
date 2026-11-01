// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMediaTranscriptEngineSignalsAreRegistered(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := NewInstruments(provider.Meter("media-transcript-engine-contract"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	instruments.MediaTranscriptEngineRuns.Add(t.Context(), 1)
	instruments.MediaTranscriptEngineDuration.Record(t.Context(), 4.2)
	instruments.MediaTranscriptEngineSegments.Add(t.Context(), 3)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	want := map[string]bool{
		"eshu_dp_media_transcript_engine_runs_total":       false,
		"eshu_dp_media_transcript_engine_duration_seconds": false,
		"eshu_dp_media_transcript_engine_segments_total":   false,
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, measured := range scope.Metrics {
			if _, ok := want[measured.Name]; ok {
				want[measured.Name] = true
			}
		}
	}
	for name, registered := range want {
		if !registered {
			t.Errorf("metric %q was not emitted", name)
		}
	}
	if SpanMediaTranscriptEngineRun != "media.transcript_engine.run" {
		t.Fatalf("SpanMediaTranscriptEngineRun = %q", SpanMediaTranscriptEngineRun)
	}
}
