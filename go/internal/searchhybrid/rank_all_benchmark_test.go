// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchembed"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// rankAllBenchmarkDocs returns count repository-scoped documents with
// realistic-length (about 3 KB) bodies drawn from a fixed vocabulary.
func rankAllBenchmarkDocs(count int) []searchdocs.Document {
	words := []string{
		"func", "return", "err", "ctx", "order", "invoice", "customer", "account",
		"balance", "amount", "currency", "total", "price", "retry", "status",
		"payment", "refund", "ledger", "update", "query", "row", "scan", "close",
	}
	docs := make([]searchdocs.Document, 0, count)
	seed := uint32(7746)
	for i := 0; i < count; i++ {
		var body strings.Builder
		for body.Len() < 3000 {
			seed = seed*1664525 + 1013904223
			body.WriteString(words[int(seed>>8)%len(words)])
			body.WriteByte(' ')
		}
		docs = append(docs, searchdocs.Document{
			ID:           fmt.Sprintf("doc-%03d", i),
			RepoID:       "repo-bench",
			SourceKind:   searchdocs.SourceKindCodeEntity,
			Title:        fmt.Sprintf("function handler%03d", i),
			ContextText:  body.String(),
			GraphHandles: []searchdocs.GraphHandle{{Kind: "content_entity", ID: fmt.Sprintf("doc-%03d", i)}},
		})
	}
	return docs
}

// BenchmarkRankAll measures RankAll alone, over a prebuilt index with the
// shipped local hash embedder, at half and full maximum page size (#7746).
func BenchmarkRankAll(b *testing.B) {
	for _, count := range []int{100, 200} {
		b.Run(fmt.Sprintf("docs=%d", count), func(b *testing.B) {
			embedder, err := searchembed.NewHashEmbedder(searchembed.DefaultDimensions)
			if err != nil {
				b.Fatalf("NewHashEmbedder error = %v", err)
			}
			index, err := searchhybrid.NewIndex(rankAllBenchmarkDocs(count), searchhybrid.Options{
				MaxDocuments: count,
				Embedder:     embedder,
			})
			if err != nil {
				b.Fatalf("NewIndex error = %v", err)
			}
			backend := searchhybrid.Backend{Index: index}
			scope := searchretrieval.Scope{RepoID: "repo-bench"}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				candidates, err := backend.RankAll(ctx, "payment refund ledger", scope)
				if err != nil || len(candidates) != count {
					b.Fatalf("RankAll returned %d candidates, err = %v; want %d", len(candidates), err, count)
				}
			}
		})
	}
}
