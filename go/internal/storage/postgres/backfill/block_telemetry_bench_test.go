// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The block telemetry adds a fixed number of calls per pass (8 in-pass blocks,
// one reopen block per domain call, one probe observation per fan-in partition,
// and two clock reads per catalog row). These benchmarks price each so the
// overhead is a measured number against the 62 to 99 s pass, not a guess
// (#8082). Log output is discarded but the line is still formatted, so the
// formatting cost is included.

// benchBlockInstruments builds the real histogram over an in-memory meter
// provider with no exporter.
func benchBlockInstruments(b *testing.B) *telemetry.Instruments {
	b.Helper()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	instruments, err := telemetry.NewInstruments(provider.Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments
}

func discardLog(b *testing.B) {
	b.Helper()
	original := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(original) })
}

// BenchmarkBlockSpanStartEnd prices one block with the real histogram, a noop
// tracer, and a discarded log line: the cost of one block when tracing is off.
func BenchmarkBlockSpanStartEnd(b *testing.B) {
	discardLog(b)
	instruments := benchBlockInstruments(b)
	tracer := noop.NewTracerProvider().Tracer("bench")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StartBlock(ctx, tracer, instruments, BlockMemoGate).End(nil,
			IntAttr("candidate_partitions", 830), IntAttr("skipped", 761), IntAttr("loaded", 69))
	}
}

// BenchmarkBlockSpanStartEndSDKTracer prices one block with a real SDK tracer
// that has no span processor, the cost when tracing is on but nothing exports.
func BenchmarkBlockSpanStartEndSDKTracer(b *testing.B) {
	discardLog(b)
	instruments := benchBlockInstruments(b)
	tracer := sdktrace.NewTracerProvider().Tracer("bench")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StartBlock(ctx, tracer, instruments, BlockMemoGate).End(nil,
			IntAttr("candidate_partitions", 830), IntAttr("skipped", 761), IntAttr("loaded", 69))
	}
}

// BenchmarkArgoCDProbeTimerRecord prices the per-partition probe timing that
// writeDeferredBackfillPartitionMemos adds: two clock reads, one context
// lookup, and one atomic add.
func BenchmarkArgoCDProbeTimerRecord(b *testing.B) {
	timer := &argoCDProbeTimer{}
	ctx := withArgoCDProbeTimer(context.Background(), timer)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		recordArgoCDProbe(ctx, time.Since(start))
	}
}

// BenchmarkCatalogLoadStatsPerRow prices the per-catalog-row timing that
// LoadRepositoryCatalog adds when stats are on the context: one counter
// increment and two clock reads.
func BenchmarkCatalogLoadStatsPerRow(b *testing.B) {
	stats := &catalogLoadStats{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.rows++
		decodeStart := time.Now()
		stats.decode += time.Since(decodeStart)
	}
}
