// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// #7008: the single-statement topic read records its per-stage breakdown
// (probe execution, row assembly, entity/file row counts) so the 30 s
// dispatch budget has an attributable split without a second query.
func TestRecordCodeTopicProbeStage(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	_, span := provider.Tracer("probe-stage-test").Start(context.Background(), "postgres.query")

	recordCodeTopicProbeStage(span, 1200, 35, 40, 2, true, true)
	span.End()

	got := map[string]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		got[string(kv.Key)] = kv.Value
	}
	cases := []struct {
		key  string
		want int64
	}{
		{"code_topic.probe_duration_ms", 1200},
		{"code_topic.assembly_duration_ms", 35},
		{"code_topic.entity_probe_rows", 40},
		{"code_topic.file_probe_rows", 2},
	}
	for _, tc := range cases {
		val, ok := got[tc.key]
		if !ok {
			t.Fatalf("missing span attribute %q", tc.key)
		}
		if val.AsInt64() != tc.want {
			t.Fatalf("%s = %d, want %d", tc.key, val.AsInt64(), tc.want)
		}
	}
	for _, key := range []string{"code_topic.pool_truncated", "code_topic.deadline_truncated"} {
		val, ok := got[key]
		if !ok {
			t.Fatalf("missing span attribute %q", key)
		}
		if !val.AsBool() {
			t.Fatalf("%s = false, want true", key)
		}
	}
}
