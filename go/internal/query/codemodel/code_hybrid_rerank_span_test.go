// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCodeHybridRerankEmitsStageSpan pins the query.hybrid_rerank stage span on
// the find_code re-rank: one span per pass that reaches the ranking work, with
// tool=find_code, the page row count, and applied matching what Rerank
// returned. A page whose rows all fail projection still pays for the pass, so
// it records applied=false (#7746).
func TestCodeHybridRerankEmitsStageSpan(t *testing.T) {
	t.Parallel()

	sensitive := []map[string]any{
		{"entity_id": "e-1", "repo_id": rerankRepoID, "entity_type": "function", "entity_name": "a", "source_cache": "password = 1"},
		{"entity_id": "e-2", "repo_id": rerankRepoID, "entity_type": "function", "entity_name": "b", "source_cache": "password = 2"},
	}
	cases := []struct {
		name    string
		rows    []map[string]any
		applied bool
	}{
		{name: "maximum page applied", rows: rerankRows(CodeSearchRankedWindow), applied: true},
		{name: "unprojectable page falls back", rows: sensitive, applied: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ranker := NewCodeHybridRanker(true)
			ranker.tracer = provider.Tracer("code-hybrid-rerank-test")

			_, applied := ranker.Rerank(context.Background(), rerankRepoID, rerankQuery, tc.rows)
			if applied != tc.applied {
				t.Fatalf("Rerank applied = %v, want %v", applied, tc.applied)
			}
			ended := recorder.Ended()
			if len(ended) != 1 || ended[0].Name() != telemetry.SpanQueryHybridRerank {
				t.Fatalf("ended spans = %d, want one %s span", len(ended), telemetry.SpanQueryHybridRerank)
			}
			assertRerankSpan(t, ended[0].Attributes(), tracing.HybridRerankToolFindCode, len(tc.rows), tc.applied)
		})
	}
}

// TestCodeHybridRerankDisabledEmitsNoSpan pins that a disabled ranker records
// no stage span: it does no ranking work.
func TestCodeHybridRerankDisabledEmitsNoSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ranker := NewCodeHybridRanker(false)
	ranker.tracer = provider.Tracer("code-hybrid-rerank-test")

	if _, applied := ranker.Rerank(context.Background(), rerankRepoID, rerankQuery, rerankRows(3)); applied {
		t.Fatal("disabled Rerank applied = true")
	}
	if ended := recorder.Ended(); len(ended) != 0 {
		t.Fatalf("ended spans = %d, want 0 for a disabled ranker", len(ended))
	}
}

func assertRerankSpan(t *testing.T, attrs []attribute.KeyValue, tool string, rows int, applied bool) {
	t.Helper()
	seen := map[attribute.Key]attribute.Value{}
	for _, attr := range attrs {
		seen[attr.Key] = attr.Value
	}
	if got := seen[tracing.HybridRerankAttrTool].AsString(); got != tool {
		t.Errorf("tool = %q, want %q", got, tool)
	}
	if got := seen[tracing.HybridRerankAttrRows].AsInt64(); got != int64(rows) {
		t.Errorf("rows = %d, want %d", got, rows)
	}
	value, ok := seen[tracing.HybridRerankAttrApplied]
	if !ok || value.AsBool() != applied {
		t.Errorf("applied = %v (present %v), want %v", value.AsBool(), ok, applied)
	}
}
