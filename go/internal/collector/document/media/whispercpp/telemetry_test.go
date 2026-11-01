// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type telemetryHarness struct {
	reader   *sdkmetric.ManualReader
	recorder *tracetest.SpanRecorder
	logs     *bytes.Buffer
}

func newTelemetryEngine(t *testing.T, mode string, limits Limits) (*Engine, telemetryHarness) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("whispercpp-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	recorder := tracetest.NewSpanRecorder()
	logs := &bytes.Buffer{}
	cfg := fakeConfig(t, mode)
	cfg.Limits = limits
	cfg.Instruments = instruments
	cfg.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("whispercpp-test")
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return engine, telemetryHarness{reader: reader, recorder: recorder, logs: logs}
}

func TestTranscribeEmitsRunSignalsWithoutTranscriptText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode    string
		limits  Limits
		outcome string
		kept    int64
		dropped int64
	}{
		{mode: "success", outcome: "completed", kept: 2, dropped: 1},
		{mode: "nospeech", outcome: "no_speech", dropped: 2},
		{mode: "exit", outcome: "engine_failed"},
		{mode: "sleep", limits: Limits{Timeout: 200 * time.Millisecond}, outcome: "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			engine, h := newTelemetryEngine(t, tc.mode, tc.limits)
			_, _ = engine.Transcribe(context.Background(), wavMedia(t))

			var metrics metricdata.ResourceMetrics
			if err := h.reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			runs := counterByAttr(metrics, "eshu_dp_media_transcript_engine_runs_total", telemetry.MetricDimensionOutcome)
			if runs[tc.outcome] != 1 || len(runs) != 1 {
				t.Fatalf("runs_total by outcome = %v, want {%s:1}", runs, tc.outcome)
			}
			if !histogramHasAttr(metrics, "eshu_dp_media_transcript_engine_duration_seconds", telemetry.MetricDimensionOutcome, tc.outcome) {
				t.Fatalf("duration histogram missing outcome %q", tc.outcome)
			}
			segments := counterByAttr(metrics, "eshu_dp_media_transcript_engine_segments_total", telemetry.MetricDimensionKind)
			if segments["kept"] != tc.kept || segments["non_speech_dropped"] != tc.dropped {
				t.Fatalf("segments_total = %v, want kept=%d dropped=%d", segments, tc.kept, tc.dropped)
			}

			spans := h.recorder.Ended()
			if len(spans) != 1 || spans[0].Name() != telemetry.SpanMediaTranscriptEngineRun {
				t.Fatalf("spans = %d, want one %s", len(spans), telemetry.SpanMediaTranscriptEngineRun)
			}
			attrs := map[string]string{}
			for _, kv := range spans[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value.String()
			}
			if tc.mode == "success" && attrs["media.transcript.engine_peak_rss_bytes"] == "0" && runtime.GOOS == "linux" {
				t.Fatalf("span missing engine peak RSS on Linux: %v", attrs)
			}
			if attrs["media.transcript.outcome"] != tc.outcome || attrs["media.transcript.engine"] != EngineName {
				t.Fatalf("span attrs = %v", attrs)
			}
			logText := h.logs.String()
			if !strings.Contains(logText, `"outcome":"`+tc.outcome+`"`) || !strings.Contains(logText, `"pipeline_phase":"parsing"`) {
				t.Fatalf("log line missing outcome or phase: %s", logText)
			}
			evidence := logText + fmt.Sprint(attrs)
			for _, canary := range []string{canaryPassword, canaryEmail, canaryStderr, "payments worker"} {
				if strings.Contains(evidence, canary) {
					t.Fatalf("telemetry leaked transcript or stderr text %q", canary)
				}
			}
		})
	}
}

func TestTranscribeWithoutTelemetryIsSafe(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "success", Limits{})
	if _, err := engine.Transcribe(context.Background(), wavMedia(t)); err != nil {
		t.Fatalf("Transcribe() without telemetry error = %v", err)
	}
}

func counterByAttr(metrics metricdata.ResourceMetrics, name, key string) map[string]int64 {
	out := map[string]int64{}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(attribute.Key(key))
				out[value.AsString()] += point.Value
			}
		}
	}
	return out
}

func histogramHasAttr(metrics metricdata.ResourceMetrics, name, key, want string) bool {
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, point := range hist.DataPoints {
				if value, _ := point.Attributes.Value(attribute.Key(key)); value.AsString() == want && point.Count == 1 {
					return true
				}
			}
		}
	}
	return false
}
