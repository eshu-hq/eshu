// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/trace"
)

// BenchmarkReaderQueryObserveAccumulator repeats the BenchmarkReaderQueryObserve
// fixture with a per-request db.StageTimings accumulator on the context, so the
// accumulator's cost is the difference from the same arm without it. It exists
// only after #7545; the base arms of BenchmarkReaderQueryObserve also run on
// the commit before it.
func BenchmarkReaderQueryObserveAccumulator(b *testing.B) {
	arms := []struct {
		name        string
		observer    func(*testing.B, trace.Tracer) Observer
		requestSpan bool
	}{
		{"nil_observer", func(*testing.B, trace.Tracer) Observer { return nil }, false},
		{"otel_request_span", newBenchObserver, true},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			tracer := newBenchTracer(b)
			access, base := newStageFixtureAccess(b, arm.observer(b, tracer), 0)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx, timings := db.WithStageTimings(base)
				var request trace.Span
				if arm.requestSpan {
					ctx, request = tracer.Start(ctx, "request")
				}
				runStageFixtureQuery(b, access, ctx)
				if request != nil {
					request.End()
				}
				if !timings.Recorded() {
					b.Fatal("accumulator recorded nothing")
				}
			}
		})
	}
}
