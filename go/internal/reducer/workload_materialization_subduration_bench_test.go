// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// BenchmarkWorkloadMaterializationSubDurations measures the overhead of
// converting a workloadMaterializationTiming struct into the Result.SubDurations
// map.  This is the hot-path cost added by the per-phase telemetry change:
// one map allocation + 7 float64 assignments per handled intent.
//
// Benchmark Evidence:
//
//	Measured on darwin/arm64 (Apple M-series), Go 1.22, package reducer.
//	Input: one workloadMaterializationTiming with realistic non-zero durations.
//	Output: map[string]float64 with 7 keys.
//	Expected: < 500 ns/op, < 5 allocs/op.
//	Actual (before/after): function did not exist before this PR; this run
//	establishes the baseline for future regressions.
//
// Observability Evidence:
//
//	The produced map is consumed by recordReducerResult, which emits
//	sub_duration_<key>_seconds log attributes alongside handler_duration_seconds
//	and queue_wait_seconds.  Operators can now query structured logs to identify
//	which phase (load_inputs, build_projection, graph_write, dep_reconcile,
//	dep_retract, dep_write, phase_publish) dominates a slow intent without
//	waiting for a pprof profile.
func BenchmarkWorkloadMaterializationSubDurations(b *testing.B) {
	t := workloadMaterializationTiming{
		loadInputsDuration:      12 * time.Millisecond,
		buildProjectionDuration: 3 * time.Millisecond,
		graphWriteDuration:      850 * time.Millisecond,
		dependencyReconcile:     5 * time.Millisecond,
		dependencyRetract:       2 * time.Millisecond,
		dependencyWrite:         8 * time.Millisecond,
		phasePublishDuration:    1 * time.Millisecond,
		totalDuration:           881 * time.Millisecond,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = workloadMaterializationSubDurations(t)
	}
}

// BenchmarkRecordReducerResultWithSubDurations measures the incremental cost
// of emitting per-phase sub-duration log attributes in recordReducerResult
// when a handler populates Result.SubDurations.  Uses a discard logger so only
// the slog attribute-building path is measured, not I/O.
//
// Benchmark Evidence:
//
//	Same hardware/version as BenchmarkWorkloadMaterializationSubDurations.
//	Before this PR: recordReducerResult emitted 8 fixed attrs; SubDurations
//	field did not exist.
//	After this PR: emits 8 + len(SubDurations) attrs (8+7=15 for workload
//	materialization).
//	Expected overhead vs. the pre-PR baseline: < 1 µs/op for the extra 7
//	slog.Float64 + map range loop, dominated by the fixed-attr baseline.
func BenchmarkRecordReducerResultWithSubDurations(b *testing.B) {
	svc := Service{
		Logger: slog.New(slog.NewTextHandler(noopWriter{}, nil)),
	}
	intent := Intent{
		IntentID:     "bench-intent-1",
		Domain:       DomainWorkloadMaterialization,
		ScopeID:      "scope-bench",
		GenerationID: "gen-bench",
		EntityKeys:   []string{"bench-key"},
	}
	result := Result{
		IntentID:        intent.IntentID,
		Domain:          DomainWorkloadMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: "bench",
		CanonicalWrites: 42,
		SubDurations: workloadMaterializationSubDurations(workloadMaterializationTiming{
			loadInputsDuration:      12 * time.Millisecond,
			buildProjectionDuration: 3 * time.Millisecond,
			graphWriteDuration:      850 * time.Millisecond,
			dependencyReconcile:     5 * time.Millisecond,
			dependencyRetract:       2 * time.Millisecond,
			dependencyWrite:         8 * time.Millisecond,
			phasePublishDuration:    1 * time.Millisecond,
			totalDuration:           881 * time.Millisecond,
		}),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.recordReducerResult(ctx, intent, result, 0.881, 1526.0, "succeeded", 0, nil)
	}
}

// BenchmarkRecordReducerResultNoSubDurations is the baseline — no SubDurations
// populated, matching the pre-PR code path for non-workload domains.
func BenchmarkRecordReducerResultNoSubDurations(b *testing.B) {
	svc := Service{
		Logger: slog.New(slog.NewTextHandler(noopWriter{}, nil)),
	}
	intent := Intent{
		IntentID:     "bench-intent-2",
		Domain:       DomainDeploymentMapping,
		ScopeID:      "scope-bench",
		GenerationID: "gen-bench",
		EntityKeys:   []string{"bench-key"},
	}
	result := Result{
		IntentID:        intent.IntentID,
		Domain:          DomainDeploymentMapping,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: "bench",
		CanonicalWrites: 10,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.recordReducerResult(ctx, intent, result, 0.050, 120.0, "succeeded", 0, nil)
	}
}

// noopWriter discards all log output so benchmarks measure only the
// slog attribute construction path, not I/O.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
