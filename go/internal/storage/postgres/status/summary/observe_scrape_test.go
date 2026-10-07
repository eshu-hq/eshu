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
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestObserveScrapeCountsEveryScrapeBySourceAndReason(t *testing.T) {
	instruments, reader := observeInstruments(t)
	ctx := context.Background()
	for _, o := range []ScrapeObservation{
		{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, Age: 4 * time.Second},
		{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh, Age: 6 * time.Second},
		{ModelKey: ModelActiveWorkSummary, Source: SourceLastRow, Reason: ReasonStale, Age: 90 * time.Second},
		{ModelKey: ModelActiveWorkSummary, Source: SourceZero, Reason: ReasonMissing},
	} {
		ObserveScrape(ctx, instruments, o)
	}

	sum := collectMetric(t, reader, "eshu_dp_status_summary_scrape_total").Data.(metricdata.Sum[int64])
	got := map[string]int64{}
	for _, point := range sum.DataPoints {
		source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
		reason, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason))
		model, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionModelKey))
		if model.AsString() != ModelActiveWorkSummary || point.Attributes.Len() != 3 {
			t.Fatalf("labels = %v, want exactly model_key, source, reason", point.Attributes)
		}
		got[source.AsString()+"/"+reason.AsString()] = point.Value
	}
	want := map[string]int64{"model/fresh": 2, "last_row/stale": 1, "zero/missing": 1}
	if len(got) != len(want) {
		t.Fatalf("series = %v, want %v", got, want)
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("series %s = %d, want %d (all %v)", key, got[key], count, got)
		}
	}
}

func TestObserveScrapeToleratesNilInstruments(t *testing.T) {
	ObserveScrape(context.Background(), nil, ScrapeObservation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh})
	ObserveScrape(context.Background(), &telemetry.Instruments{}, ScrapeObservation{ModelKey: ModelActiveWorkSummary, Source: SourceZero, Reason: ReasonMissing})
}

func TestObserveScrapeSetsSpanAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "postgres.status_snapshot")
	ObserveScrape(ctx, nil, ScrapeObservation{
		ModelKey: ModelActiveWorkSummary, Source: SourceLastRow, Reason: ReasonStale,
		AsOf: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Age: 90500 * time.Millisecond,
	})
	span.End()

	attrs := map[string]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	if attrs["status.active_work.source"].AsString() != "last_row" ||
		attrs["status.active_work.fallback_reason"].AsString() != "stale" ||
		attrs["status.active_work.as_of_age_seconds"].AsFloat64() != 90.5 {
		t.Fatalf("span attributes = %v", attrs)
	}
}

func TestObserveScrapeLogsOnlyAStaleServeAndRateLimitsIt(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	scrapeLimiter.reset()

	ObserveScrape(context.Background(), nil, ScrapeObservation{ModelKey: ModelActiveWorkSummary, Source: SourceModel, Reason: ReasonFresh})
	if buf.Len() != 0 {
		t.Fatalf("a fresh scrape logged: %s", buf.String())
	}
	stale := ScrapeObservation{ModelKey: ModelActiveWorkSummary, Source: SourceLastRow, Reason: ReasonStale, Age: 41 * time.Second}
	ObserveScrape(context.Background(), nil, stale)
	line := buf.String()
	for _, want := range []string{`"level":"WARN"`, `"model_key":"active_work_summary"`, `"source":"last_row"`, `"reason":"stale"`, `"age_seconds":41`, `"failure_class":"status_summary_scrape_stale"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %s is missing %s", line, want)
		}
	}
	buf.Reset()
	ObserveScrape(context.Background(), nil, stale)
	if buf.Len() != 0 {
		t.Fatalf("a second stale scrape inside a minute logged again: %s", buf.String())
	}
}
