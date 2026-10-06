// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

// The single-statement 16-term read records why it did not run the four
// partition shared-snapshot path. Only the 16-term shape can be parallel, so
// every other term count records nothing.
func TestRecordCodeTopicFallbackReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		termCount           int
		supportsSnapshotSet bool
		maxOpenConns        int
		want                string
	}{
		{name: "no_snapshot_set", termCount: 16, supportsSnapshotSet: false, maxOpenConns: 0, want: "snapshot_set_unavailable"},
		{name: "pool_too_small", termCount: 16, supportsSnapshotSet: true, maxOpenConns: codetopicparallel.Partitions - 1, want: "pool_capacity"},
		{name: "eligible_pool", termCount: 16, supportsSnapshotSet: true, maxOpenConns: codetopicparallel.Partitions, want: ""},
		{name: "three_terms", termCount: 3, supportsSnapshotSet: false, maxOpenConns: 0, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			_, span := provider.Tracer("fallback-reason-test").Start(context.Background(), "postgres.query")

			recordCodeTopicFallbackReason(span, tc.termCount, tc.supportsSnapshotSet, tc.maxOpenConns)
			span.End()

			got := ""
			for _, kv := range recorder.Ended()[0].Attributes() {
				if kv.Key == attribute.Key("code_topic.parallel_fallback_reason") {
					got = kv.Value.AsString()
				}
			}
			if got != tc.want {
				t.Fatalf("code_topic.parallel_fallback_reason = %q, want %q", got, tc.want)
			}
		})
	}
}
