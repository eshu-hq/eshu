// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// walkBenchmarkVocabulary is filler for synthetic bodies; it avoids every
// term searchdocs treats as sensitive, so each row projects. It mirrors
// codemodel's rerankVocabulary so the figures compare with
// BenchmarkCodeHybridRerank.
var walkBenchmarkVocabulary = []string{
	"func", "return", "err", "nil", "ctx", "context", "order", "invoice",
	"customer", "account", "balance", "amount", "currency", "total", "line",
	"item", "price", "tax", "discount", "shipping", "address", "status",
	"update", "insert", "select", "query", "row", "rows", "scan", "close",
	"retry", "backoff", "timeout", "deadline", "cancel", "span", "metric",
}

// walkBenchmarkRows builds count content-lane rows with about 3 KB bodies,
// the shape of BenchmarkCodeHybridRerank's rows.
func walkBenchmarkRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	seed := uint32(7746)
	for i := 0; i < count; i++ {
		var body strings.Builder
		fmt.Fprintf(&body, "func handler%03d(ctx context.Context) error {\n", i)
		for body.Len() < 3000 {
			seed = seed*1664525 + 1013904223
			body.WriteString(walkBenchmarkVocabulary[int(seed>>8)%len(walkBenchmarkVocabulary)])
			body.WriteByte(' ')
		}
		body.WriteString("\n}\n")
		rows = append(rows, map[string]any{
			"entity_id": fmt.Sprintf("entity-%03d", i), "repo_id": "repo-bench",
			"file_path": fmt.Sprintf("service/handler_%03d.go", i), "entity_type": "function",
			"entity_name": fmt.Sprintf("handler%03d", i), "language": "go", "source_cache": body.String(),
		})
	}
	return rows
}

// BenchmarkFindCodeRankWalkPage measures the CPU and allocation of one hybrid
// find_code walk page at the full 200-row window, past the store read:
// "page" is ScoreCodeWindow + MapRows + Walk + MapPage, the work
// handleRankWalk does per request; "walk_only" is MapRows + Walk + MapPage
// over precomputed scores, the cost the walk adds on top of scoring (#7746).
func BenchmarkFindCodeRankWalkPage(b *testing.B) {
	const query = "payment refund ledger"
	ranker := codemodel.NewCodeHybridRanker(true)
	rows := walkBenchmarkRows(rank.WindowSize)
	ctx := context.Background()
	scores, err := ranker.ScoreCodeWindow(ctx, "repo-bench", query, rows)
	if err != nil || len(scores) != rank.WindowSize {
		b.Fatalf("ScoreCodeWindow scored %d rows, err = %v; want %d", len(scores), err, rank.WindowSize)
	}
	walkPage := func(scores map[string]float64) {
		windowRows, byID := rank.MapRows(rows, codeRankWalkContentKeys...)
		page, err := rank.Walk(rank.Window{
			Tool: rank.ToolFindCode, Lane: rank.LaneContent, Order: rank.OrderHybrid,
			Ranker: codemodel.CodeRankerIdentity, Scope: []string{"repo-bench", query, "", "false"},
			Rows: windowRows,
		}, scores, rank.StartToken, 50)
		if err != nil || len(page.IDs) != 50 {
			b.Fatalf("Walk served %d rows, err = %v", len(page.IDs), err)
		}
		_ = rank.MapPage(byID, scores, page.IDs)
	}
	b.Run("page", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pageScores, err := ranker.ScoreCodeWindow(ctx, "repo-bench", query, rows)
			if err != nil {
				b.Fatalf("ScoreCodeWindow error = %v", err)
			}
			walkPage(pageScores)
		}
	})
	b.Run("walk_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			walkPage(scores)
		}
	})
}
