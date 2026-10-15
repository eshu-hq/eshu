// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// rerankRepoID is the repository every synthetic rerank row belongs to.
const rerankRepoID = "repo-rerank"

// rerankQuery is the find_code query the synthetic rows are ranked against.
const rerankQuery = "payment refund ledger"

// rerankVocabulary is the filler vocabulary for synthetic source bodies. It
// avoids every term searchdocs treats as sensitive context, so each row
// projects into a search document.
var rerankVocabulary = []string{
	"func", "return", "err", "nil", "ctx", "context", "order", "invoice",
	"customer", "account", "balance", "amount", "currency", "total", "line",
	"item", "price", "tax", "discount", "shipping", "address", "status",
	"update", "insert", "select", "query", "row", "rows", "scan", "close",
	"retry", "backoff", "timeout", "deadline", "cancel", "span", "metric",
	"logger", "info", "debug", "warn", "error", "wrap", "fmt", "errorf",
	"string", "int", "bool", "map", "slice", "append", "range", "len",
}

// rerankRows builds count projectable find_code rows with realistic-length
// source bodies (about 3 KB each, under the 4 KB searchdocs context cap). The
// row with the highest entity id carries a body dense with the query terms, so
// it must rank first once every row is in the hybrid index.
func rerankRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	seed := uint32(7746)
	for i := 0; i < count; i++ {
		var body strings.Builder
		fmt.Fprintf(&body, "func handler%03d(ctx context.Context) error {\n", i)
		for body.Len() < 3000 {
			seed = seed*1664525 + 1013904223
			body.WriteString(rerankVocabulary[int(seed>>8)%len(rerankVocabulary)])
			body.WriteByte(' ')
			if seed%9 == 0 {
				body.WriteString("\n\t")
			}
		}
		if i == count-1 {
			body.WriteString(strings.Repeat("payment refund ledger ", 20))
		}
		body.WriteString("\n}\n")
		rows = append(rows, map[string]any{
			"entity_id":    fmt.Sprintf("entity-%03d", i),
			"repo_id":      rerankRepoID,
			"file_path":    fmt.Sprintf("service/handler_%03d.go", i),
			"entity_type":  "function",
			"entity_name":  fmt.Sprintf("handler%03d", i),
			"language":     "go",
			"source_cache": body.String(),
		})
	}
	return rows
}

// TestCodeHybridRerankRanksAllRowsOfAMaximumPage pins that a find_code page of
// the maximum size (CodeSearchRankedWindow rows) is hybrid-ranked in full. With
// an index cap below the page size, the rows past the cap (by entity id) were
// dropped as index overflow and appended unranked after the ranked rows, so a
// strong match with a high entity id could never rank first (#7746).
func TestCodeHybridRerankRanksAllRowsOfAMaximumPage(t *testing.T) {
	t.Parallel()

	ranker := NewCodeHybridRanker(true)
	rows := rerankRows(CodeSearchRankedWindow)

	got, applied := ranker.Rerank(context.Background(), rerankRepoID, rerankQuery, rows)
	if !applied {
		t.Fatal("Rerank applied = false, want true for a projectable maximum page")
	}
	if len(got) != CodeSearchRankedWindow {
		t.Fatalf("Rerank returned %d rows, want %d (no row may be dropped)", len(got), CodeSearchRankedWindow)
	}
	unranked := 0
	seen := make(map[string]struct{}, len(got))
	for _, row := range got {
		seen[mapString(row, "entity_id")] = struct{}{}
		if row["search_backend"] != "hybrid" {
			unranked++
		}
	}
	if unranked != 0 {
		t.Errorf("%d/%d rows were appended unranked, want every row hybrid-ranked", unranked, len(got))
	}
	if len(seen) != CodeSearchRankedWindow {
		t.Errorf("Rerank returned %d distinct rows, want %d", len(seen), CodeSearchRankedWindow)
	}
	wantTop := fmt.Sprintf("entity-%03d", CodeSearchRankedWindow-1)
	if top := mapString(got[0], "entity_id"); top != wantTop {
		t.Errorf("top row = %s, want %s (the strongest match has the highest entity id)", top, wantTop)
	}
}

// BenchmarkCodeHybridRerank measures one find_code hybrid re-rank over a page
// of realistic-length rows: half a maximum page and a full maximum page. The
// applied metric is the fraction of runs the re-rank reordered the page, so a
// run that does the work and then falls back to lexical order shows as 0.
func BenchmarkCodeHybridRerank(b *testing.B) {
	for _, count := range []int{CodeSearchRankedWindow / 2, CodeSearchRankedWindow} {
		b.Run(fmt.Sprintf("rows=%d", count), func(b *testing.B) {
			ranker := NewCodeHybridRanker(true)
			rows := rerankRows(count)
			ctx := context.Background()
			applied := 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, ok := ranker.Rerank(ctx, rerankRepoID, rerankQuery, rows); ok {
					applied++
				}
			}
			b.ReportMetric(float64(applied)/float64(b.N), "applied")
		})
	}
}
