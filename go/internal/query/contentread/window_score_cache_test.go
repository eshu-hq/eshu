// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestScoreEntityWindowServesRepeatWindowFromCache is the #8216 behavioral
// contract on the search_entity_content lane: scoring the same window twice
// runs the ranking work once. The recording tracer sees exactly one
// query.hybrid_rerank span, and both calls return identical scores, so
// rank.Walk orders both pages the same way and the exactly-once and 409
// behavior cannot change. An edited body misses and records a second span.
func TestScoreEntityWindowServesRepeatWindowFromCache(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ranker := NewContentHybridRanker(true)
	ranker.tracer = provider.Tracer("test")
	rows := walkRows(rank.WindowSize)
	ctx := context.Background()

	first, err := ranker.ScoreEntityWindow(ctx, walkRepoID, "payment refund ledger", rows)
	if err != nil {
		t.Fatalf("first ScoreEntityWindow error = %v", err)
	}
	second, err := ranker.ScoreEntityWindow(ctx, walkRepoID, "payment refund ledger", rows)
	if err != nil {
		t.Fatalf("second ScoreEntityWindow error = %v", err)
	}
	if len(first) != rank.WindowSize || len(second) != rank.WindowSize {
		t.Fatalf("scored %d then %d rows, want %d both times", len(first), len(second), rank.WindowSize)
	}
	for id, score := range first {
		if second[id] != score {
			t.Fatalf("score for %q changed across pages: %v then %v", id, score, second[id])
		}
	}
	edited := walkRows(rank.WindowSize)
	edited[7].SourceCache = "func handler007() error {\nreturn 0\n}\n"
	if _, err := ranker.ScoreEntityWindow(ctx, walkRepoID, "payment refund ledger", edited); err != nil {
		t.Fatalf("edited ScoreEntityWindow error = %v", err)
	}

	spans := 0
	for _, span := range recorder.Ended() {
		if span.Name() == telemetry.SpanQueryHybridRerank {
			spans++
		}
	}
	if spans != 2 {
		t.Fatalf("hybrid_rerank spans = %d, want 2 (one cold pass, one edited-window pass, none for the repeat)", spans)
	}
}
