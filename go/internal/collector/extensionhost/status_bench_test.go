// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"io"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// BenchmarkNextClaimedRetryableUnrecorded measures one retryable claim result
// through Source.NextClaimed with no status recorder: the worker's cost before
// #8908 wired one, and the floor any recorder adds to.
func BenchmarkNextClaimedRetryableUnrecorded(b *testing.B) {
	item := testWorkItem()
	runner := &benchRunner{result: retryableResultWithDelays(item, 30, 120)}
	benchNextClaimed(b, mustNewSourceWithManifest(b, testManifest(), runner, nil))
}

// BenchmarkNextClaimedUnchangedUnrecorded is the same floor for an unchanged
// result, the steady-state outcome of a collector polling a quiet source.
func BenchmarkNextClaimedUnchangedUnrecorded(b *testing.B) {
	item := testWorkItem()
	runner := &benchRunner{result: unchangedBenchResult(item)}
	benchNextClaimed(b, mustNewSourceWithManifest(b, testManifest(), runner, nil))
}

// BenchmarkNextClaimedRetryableRecorded is the retryable path with the
// production StatusTelemetry adapter: two status rows, one result, one WARN
// log line to a discarding JSON handler.
func BenchmarkNextClaimedRetryableRecorded(b *testing.B) {
	item := testWorkItem()
	runner := &benchRunner{result: retryableResultWithDelays(item, 30, 120)}
	benchNextClaimed(b, mustNewSourceWithManifest(b, testManifest(), runner, benchStatusTelemetry(b, slog.LevelInfo)))
}

// BenchmarkNextClaimedUnchangedRecorded is the unchanged path with the
// adapter and its INFO log line enabled.
func BenchmarkNextClaimedUnchangedRecorded(b *testing.B) {
	item := testWorkItem()
	runner := &benchRunner{result: unchangedBenchResult(item)}
	benchNextClaimed(b, mustNewSourceWithManifest(b, testManifest(), runner, benchStatusTelemetry(b, slog.LevelInfo)))
}

// BenchmarkNextClaimedUnchangedRecordedWarnLevel is the unchanged path with a
// WARN-level logger, the cost when INFO result lines are filtered out.
func BenchmarkNextClaimedUnchangedRecordedWarnLevel(b *testing.B) {
	item := testWorkItem()
	runner := &benchRunner{result: unchangedBenchResult(item)}
	benchNextClaimed(b, mustNewSourceWithManifest(b, testManifest(), runner, benchStatusTelemetry(b, slog.LevelWarn)))
}

func benchStatusTelemetry(b *testing.B, level slog.Level) *StatusTelemetry {
	b.Helper()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: level}))
	return NewStatusTelemetry(inst, logger)
}

// benchRunner returns a fixed result without recording requests, so the
// benchmark measures the host path rather than a growing request slice.
type benchRunner struct {
	result sdkcollector.Result
}

func (r *benchRunner) RunCollector(context.Context, Request) (sdkcollector.Result, error) {
	return r.result, nil
}

func unchangedBenchResult(item workflow.WorkItem) sdkcollector.Result {
	result := baseResult(item, sdkcollector.ResultUnchanged)
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete, SourceLatencyMS: 250}}
	return result
}

func benchNextClaimed(b *testing.B, source *Source) {
	b.Helper()
	item := testWorkItem()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = source.NextClaimed(ctx, item)
	}
}
