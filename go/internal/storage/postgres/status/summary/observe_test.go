// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"bytes"
	"context"
	"log/slog"
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

func observeInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %s was not recorded", name)
	return metricdata.Metrics{}
}

func TestObserveCountsEveryReadBySourceAndReason(t *testing.T) {
	instruments, reader := observeInstruments(t)
	ctx := context.Background()
	for _, o := range []Observation{
		{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, Age: 4 * time.Second},
		{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, Age: 6 * time.Second},
		{ModelKey: ModelActiveWorkSummary, Source: SourceLive, Reason: ReasonFlagOff},
		{ModelKey: ModelActiveWorkSummary, Source: SourceLiveFallback, Reason: ReasonStale, Age: 40 * time.Second},
	} {
		Observe(ctx, instruments, o)
	}

	sum := collectMetric(t, reader, "eshu_dp_status_summary_read_total").Data.(metricdata.Sum[int64])
	got := map[string]int64{}
	for _, point := range sum.DataPoints {
		source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
		reason, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason))
		model, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionModelKey))
		if model.AsString() != ModelActiveWorkSummary {
			t.Fatalf("model_key = %q", model.AsString())
		}
		got[source.AsString()+"/"+reason.AsString()] = point.Value
	}
	want := map[string]int64{"model/fresh": 2, "live/flag_off": 1, "live_fallback/stale": 1}
	if len(got) != len(want) {
		t.Fatalf("series = %v, want %v", got, want)
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("series %s = %d, want %d (all %v)", key, got[key], count, got)
		}
	}

	ages := collectMetric(t, reader, "eshu_dp_status_summary_read_age_seconds").Data.(metricdata.Histogram[float64])
	var samples uint64
	for _, point := range ages.DataPoints {
		samples += point.Count
	}
	if samples != 2 {
		t.Fatalf("age samples = %d, want 2: only model reads record the served age", samples)
	}
}

func TestObserveToleratesNilInstruments(t *testing.T) {
	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceLive, Reason: ReasonFlagOff})
	Observe(context.Background(), &telemetry.Instruments{}, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh})
}

func TestObserveSetsSpanAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	Observe(ctx, nil, Observation{
		ModelKey: ModelActiveWorkSummary, Source: SourceLiveFallback, Reason: ReasonStale,
		AsOf: asOf, Age: 41500 * time.Millisecond, SignedAge: 41500 * time.Millisecond,
	})
	span.End()

	attrs := map[string]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	if attrs["status.active_work.source"].AsString() != "live_fallback" {
		t.Fatalf("source attr = %v", attrs["status.active_work.source"])
	}
	if attrs["status.active_work.fallback_reason"].AsString() != "stale" {
		t.Fatalf("fallback_reason attr = %v", attrs["status.active_work.fallback_reason"])
	}
	if attrs["status.active_work.as_of_age_signed_seconds"].AsFloat64() != 41.5 {
		t.Fatalf("as_of_age_signed_seconds attr = %v", attrs["status.active_work.as_of_age_signed_seconds"])
	}
	if attrs["status.active_work.as_of_age_seconds"].AsFloat64() != 41.5 {
		t.Fatalf("as_of_age_seconds attr = %v", attrs["status.active_work.as_of_age_seconds"])
	}
}

func TestObserveOmitsFallbackReasonAttributeForServedAnswers(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
	Observe(ctx, nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, Age: time.Second})
	span.End()
	for _, kv := range recorder.Ended()[0].Attributes() {
		if kv.Key == "status.active_work.fallback_reason" {
			t.Fatalf("a served answer carries fallback_reason %v", kv.Value)
		}
	}
}

func TestWarnLimiterAllowsOncePerMinutePerReason(t *testing.T) {
	t.Parallel()

	var limiter warnLimiter
	start := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if !limiter.allow(string(ReasonStale), start) {
		t.Fatal("the first stale fallback must log")
	}
	if limiter.allow(string(ReasonStale), start.Add(59*time.Second)) {
		t.Fatal("a second stale fallback inside a minute logged again")
	}
	if !limiter.allow(string(ReasonMissing), start.Add(time.Second)) {
		t.Fatal("a different reason shares the stale reason's budget")
	}
	if !limiter.allow(string(ReasonStale), start.Add(time.Minute)) {
		t.Fatal("the stale fallback did not log again after a minute")
	}
}

func TestObserveLogsAFallbackButNotAServedOrFlagOffRead(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fallbackLimiter.reset()

	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh})
	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceLive, Reason: ReasonFlagOff})
	if buf.Len() != 0 {
		t.Fatalf("a served or flag-off read logged: %s", buf.String())
	}
	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceLiveFallback, Reason: ReasonStale, Age: 41 * time.Second})
	line := buf.String()
	for _, want := range []string{`"level":"WARN"`, `"model_key":"active_work_summary"`, `"reason":"stale"`, `"age_seconds":41`, "status summary row not served"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %s is missing %s", line, want)
		}
	}
}

func TestObserveUsesTheModelsSpanPrefixAndWarnsOncePerModelAndReason(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
	asOf := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fallbackLimiter.reset()

	Observe(ctx, nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, AsOf: asOf, Age: time.Second})
	Observe(ctx, nil, Observation{
		ModelKey: ModelTerraformState, SpanPrefix: "status.terraform_state", Source: SourceLiveFallback, Reason: ReasonStale,
		AsOf: asOf, Age: 40 * time.Second, SignedAge: 40 * time.Second,
	})
	span.End()

	attrs := map[string]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	if attrs["status.active_work.source"].AsString() != "model" {
		t.Fatalf("active work source attr = %v", attrs["status.active_work.source"])
	}
	if attrs["status.terraform_state.source"].AsString() != "live_fallback" ||
		attrs["status.terraform_state.fallback_reason"].AsString() != "stale" ||
		attrs["status.terraform_state.as_of_age_seconds"].AsFloat64() != 40 {
		t.Fatalf("terraform attrs = %v", attrs)
	}

	// The same reason on another model is its own Warn: one noisy model must
	// not hide the other's.
	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceLiveFallback, Reason: ReasonStale, Age: 41 * time.Second})
	Observe(context.Background(), nil, Observation{ModelKey: ModelActiveWorkSummary, Source: SourceLiveFallback, Reason: ReasonStale, Age: 42 * time.Second})
	if got := strings.Count(buf.String(), "status summary row not served"); got != 2 {
		t.Fatalf("fallback warnings = %d, want 2 (one per model, the repeat suppressed): %s", got, buf.String())
	}
	if !strings.Contains(buf.String(), `"model_key":"terraform_state"`) || !strings.Contains(buf.String(), `"model_key":"active_work_summary"`) {
		t.Fatalf("warnings did not name both models: %s", buf.String())
	}
}

// TestObserveKeepsEachModelsSpanAttributesApartInEitherOrder: two models
// observed on one span, in both orders, leave each model's source, age and
// fallback reason under its own prefix. An observation that lost its prefix
// would overwrite the other model's attributes.
func TestObserveKeepsEachModelsSpanAttributesApartInEitherOrder(t *testing.T) {
	asOf := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	active := Observation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, AsOf: asOf, Age: time.Second, SignedAge: time.Second}
	terraform := Observation{
		ModelKey: ModelTerraformState, SpanPrefix: "status.terraform_state", Source: SourceLiveFallback, Reason: ReasonStale,
		AsOf: asOf, Age: 40 * time.Second, SignedAge: 40 * time.Second,
	}
	for name, order := range map[string][]Observation{"active then terraform": {active, terraform}, "terraform then active": {terraform, active}} {
		t.Run(name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
			ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
			for _, o := range order {
				Observe(ctx, nil, o)
			}
			span.End()
			attrs := map[string]attribute.Value{}
			for _, kv := range recorder.Ended()[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value
			}
			if attrs["status.active_work.source"].AsString() != "model" || attrs["status.active_work.as_of_age_seconds"].AsFloat64() != 1 {
				t.Fatalf("active-work attributes were overwritten: %v", attrs)
			}
			if _, present := attrs["status.active_work.fallback_reason"]; present {
				t.Fatalf("active-work carries the terraform model's fallback reason: %v", attrs)
			}
			if attrs["status.terraform_state.source"].AsString() != "live_fallback" ||
				attrs["status.terraform_state.fallback_reason"].AsString() != "stale" ||
				attrs["status.terraform_state.as_of_age_seconds"].AsFloat64() != 40 {
				t.Fatalf("terraform attributes = %v", attrs)
			}
		})
	}
}
