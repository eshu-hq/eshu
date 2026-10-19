// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cacheTestRows builds count projectable rows with distinct bodies.
func cacheTestRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{
			"entity_id":   fmt.Sprintf("entity-%03d", i),
			"repo_id":     "repo-cache",
			"file_path":   fmt.Sprintf("service/handler_%03d.go", i),
			"entity_type": "function",
			"entity_name": fmt.Sprintf("handler%03d", i),
			"language":    "go",
			"source_cache": fmt.Sprintf(
				"func handler%03d(ctx context.Context) error {\nreturn invoiceTotal(line, tax)\n}\n", i),
		})
	}
	return rows
}

func TestWindowScoreCacheHitAcrossIdenticalWindows(t *testing.T) {
	t.Parallel()

	cache := NewWindowScoreCache(8)
	rows := cacheTestRows(10)
	key := KeyWindowScores(CodeRankerIdentity, "repo-cache", "payment refund", codeWindowScoreRows(rows))
	scores := map[string]float64{"entity-001": 1.5, "entity-002": 0.5}
	cache.Put(key, scores)

	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get(key) = miss after Put, want hit")
	}
	if len(got) != len(scores) || got["entity-001"] != 1.5 {
		t.Fatalf("Get(key) = %v, want %v", got, scores)
	}
	hits, misses := cache.Stats()
	if hits != 1 || misses != 0 {
		t.Fatalf("Stats() = (%d hits, %d misses), want (1, 0)", hits, misses)
	}
}

func TestWindowScoreCacheMissOnAnyInputChange(t *testing.T) {
	t.Parallel()

	base := cacheTestRows(5)
	baseKey := KeyWindowScores(CodeRankerIdentity, "repo-cache", "payment refund", codeWindowScoreRows(base))
	mutate := func(rows []map[string]any) []map[string]any {
		out := make([]map[string]any, len(rows))
		for i, row := range rows {
			cp := make(map[string]any, len(row))
			for k, v := range row {
				cp[k] = v
			}
			out[i] = cp
		}
		return out
	}
	cases := map[string]func([]map[string]any) []map[string]any{
		"body edit": func(rows []map[string]any) []map[string]any {
			rows[2]["source_cache"] = "func handler002() error {\nreturn 0\n}\n"
			return rows
		},
		"entity swap": func(rows []map[string]any) []map[string]any {
			rows[0], rows[1] = rows[1], rows[0]
			return rows
		},
		"language change": func(rows []map[string]any) []map[string]any {
			rows[3]["language"] = "java"
			return rows
		},
		"entity name change": func(rows []map[string]any) []map[string]any {
			rows[4]["entity_name"] = "renamed"
			return rows
		},
	}
	for name, change := range cases {
		changed := change(mutate(base))
		if got := KeyWindowScores(CodeRankerIdentity, "repo-cache", "payment refund", codeWindowScoreRows(changed)); got == baseKey {
			t.Errorf("%s: key unchanged after row change, want a miss", name)
		}
	}
	if got := KeyWindowScores(CodeRankerIdentity, "repo-cache", "payment ledger", codeWindowScoreRows(mutate(base))); got == baseKey {
		t.Error("query change: key unchanged, want a miss")
	}
	if got := KeyWindowScores(CodeRankerIdentity, "repo-other", "payment refund", codeWindowScoreRows(mutate(base))); got == baseKey {
		t.Error("repo change: key unchanged, want a miss")
	}
	if got := KeyWindowScores("other-ranker/v1", "repo-cache", "payment refund", codeWindowScoreRows(mutate(base))); got == baseKey {
		t.Error("ranker change: key unchanged, want a miss")
	}
	if got := KeyWindowScores(CodeRankerIdentity, "repo-cache", "payment refund", codeWindowScoreRows(mutate(base))); got != baseKey {
		t.Error("identical window: key changed, want a hit")
	}
}

func TestWindowScoreCacheIsBounded(t *testing.T) {
	t.Parallel()

	cache := NewWindowScoreCache(4)
	for i := 0; i < 16; i++ {
		rows := cacheTestRows(3)
		rows[0]["entity_id"] = fmt.Sprintf("entity-%03d", i)
		cache.Put(KeyWindowScores(CodeRankerIdentity, "repo-cache", fmt.Sprintf("query %d", i), codeWindowScoreRows(rows)), map[string]float64{"x": 1})
	}
	if got := cache.Len(); got > 4 {
		t.Fatalf("Len() = %d, want at most 4", got)
	}
}

func TestWindowScoreCacheClonesScores(t *testing.T) {
	t.Parallel()

	cache := NewWindowScoreCache(8)
	rows := cacheTestRows(4)
	key := KeyWindowScores(CodeRankerIdentity, "repo-cache", "q", codeWindowScoreRows(rows))
	cache.Put(key, map[string]float64{"entity-001": 2.0})

	first, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get(key) = miss after Put, want hit")
	}
	first["entity-001"] = 99.0
	delete(first, "entity-001")

	second, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get(key) = miss on second read, want hit")
	}
	if second["entity-001"] != 2.0 {
		t.Fatalf("cached scores mutated through a served copy: %v", second)
	}
}

func TestWindowScoreCacheConcurrentUse(t *testing.T) {
	t.Parallel()

	cache := NewWindowScoreCache(16)
	rows := cacheTestRows(8)
	key := KeyWindowScores(CodeRankerIdentity, "repo-cache", "q", codeWindowScoreRows(rows))
	want := map[string]float64{"entity-001": 3.0}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				cache.Put(key, want)
			} else if got, ok := cache.Get(key); ok && got["entity-001"] != 3.0 {
				t.Errorf("concurrent Get = %v, want %v", got, want)
			}
		}(i)
	}
	wg.Wait()
}

// TestScoreCodeWindowServesRepeatWindowFromCache is the #8216 behavioral
// contract at the scorer: scoring the same window twice runs the ranking work
// once. The recording tracer sees exactly one query.hybrid_rerank span, and
// both calls return identical scores, so rank.Walk orders both pages the same
// way and the exactly-once and 409 behavior cannot change.
func TestScoreCodeWindowServesRepeatWindowFromCache(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ranker := NewCodeHybridRanker(true)
	ranker.tracer = provider.Tracer("test")
	rows := cacheTestRows(rank.WindowSize)
	ctx := context.Background()

	first, err := ranker.ScoreCodeWindow(ctx, "repo-cache", "payment refund ledger", rows)
	if err != nil {
		t.Fatalf("first ScoreCodeWindow error = %v", err)
	}
	second, err := ranker.ScoreCodeWindow(ctx, "repo-cache", "payment refund ledger", rows)
	if err != nil {
		t.Fatalf("second ScoreCodeWindow error = %v", err)
	}
	if len(first) != rank.WindowSize || len(second) != rank.WindowSize {
		t.Fatalf("scored %d then %d rows, want %d both times", len(first), len(second), rank.WindowSize)
	}
	for id, score := range first {
		if second[id] != score {
			t.Fatalf("score for %q changed across pages: %v then %v", id, score, second[id])
		}
	}
	spans := 0
	for _, span := range recorder.Ended() {
		if span.Name() == telemetry.SpanQueryHybridRerank {
			spans++
		}
	}
	if spans != 1 {
		t.Fatalf("hybrid_rerank spans = %d, want 1 (the repeat window must not re-score)", spans)
	}
}

// TestMapIntSaturatesUint64Overflow is the regression test for the
// gosec G115 finding on mapInt: a decoded uint64 wider than math.MaxInt
// must saturate instead of wrapping on conversion to int.
func TestMapIntSaturatesUint64Overflow(t *testing.T) {
	got := mapInt(map[string]any{"start_line": uint64(math.MaxInt) + 1}, "start_line")
	if got != math.MaxInt {
		t.Fatalf("mapInt(uint64 over MaxInt) = %d, want %d (saturate, no wrap)", got, math.MaxInt)
	}
	if got := mapInt(map[string]any{"start_line": uint64(41)}, "start_line"); got != 41 {
		t.Fatalf("mapInt(uint64 41) = %d, want 41", got)
	}
	if got := mapInt(map[string]any{"missing": uint64(7)}, "start_line"); got != 0 {
		t.Fatalf("mapInt(absent key) = %d, want 0", got)
	}
}
