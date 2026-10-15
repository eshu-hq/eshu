// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// maxPageRepoID is the repository every synthetic maximum-page row belongs to.
const maxPageRepoID = "repo-max-page"

// maxPageQuery is the content query the synthetic rows are ranked against.
const maxPageQuery = "payment refund ledger"

// maxPageBody returns a realistic-length body for row i. The last row is dense
// with the query terms, so it must rank first once every row is ranked.
func maxPageBody(i, count int) string {
	var body strings.Builder
	fmt.Fprintf(&body, "func handler%03d(ctx context.Context) error {\n", i)
	words := []string{"order", "invoice", "customer", "account", "balance", "amount", "retry", "status"}
	for n := 0; body.Len() < 1500; n++ {
		body.WriteString(words[(i+n)%len(words)])
		body.WriteByte(' ')
	}
	if i == count-1 {
		body.WriteString(strings.Repeat("payment refund ledger ", 20))
	}
	body.WriteString("\n}\n")
	return body.String()
}

// TestContentHybridRerankRanksAllRowsOfAMaximumPage pins that a content-search
// page of ContentSearchMaxLimit (200) rows, the largest page the handler
// serves, is hybrid-ranked in full for both entity and file rows. A page of
// more than 100 rows used to exceed the public retrieval limit, fail request
// validation, and stay in lexical order with applied=false (#7746). The row
// count is the page cap, not the ranker's own index cap, so lowering the
// ranker cap below the page size fails this test.
func TestContentHybridRerankRanksAllRowsOfAMaximumPage(t *testing.T) {
	t.Parallel()

	const count = ContentSearchMaxLimit
	ranker := NewContentHybridRanker(true)

	t.Run("entities", func(t *testing.T) {
		t.Parallel()
		rows := make([]querycontract.EntityContent, 0, count)
		for i := 0; i < count; i++ {
			rows = append(rows, querycontract.EntityContent{
				EntityID:     fmt.Sprintf("entity-%03d", i),
				RepoID:       maxPageRepoID,
				RelativePath: fmt.Sprintf("service/handler_%03d.go", i),
				EntityType:   "function",
				EntityName:   fmt.Sprintf("handler%03d", i),
				Language:     "go",
				SourceCache:  maxPageBody(i, count),
			})
		}
		got, applied := ranker.RerankEntities(context.Background(), maxPageRepoID, maxPageQuery, rows)
		if !applied {
			t.Fatal("RerankEntities applied = false, want true for a projectable maximum page")
		}
		if len(got) != count {
			t.Fatalf("RerankEntities returned %d rows, want %d", len(got), count)
		}
		for _, row := range got {
			if row.SearchBackend != "hybrid" {
				t.Fatalf("row %s search_backend = %q, want hybrid (every row ranked)", row.EntityID, row.SearchBackend)
			}
		}
		if want := fmt.Sprintf("entity-%03d", count-1); got[0].EntityID != want {
			t.Errorf("top row = %s, want %s", got[0].EntityID, want)
		}
	})

	t.Run("files", func(t *testing.T) {
		t.Parallel()
		rows := make([]querycontract.FileContent, 0, count)
		for i := 0; i < count; i++ {
			rows = append(rows, querycontract.FileContent{
				RepoID:       maxPageRepoID,
				RelativePath: fmt.Sprintf("service/handler_%03d.go", i),
				Language:     "go",
				Content:      maxPageBody(i, count),
			})
		}
		got, applied := ranker.RerankFiles(context.Background(), maxPageRepoID, maxPageQuery, rows)
		if !applied {
			t.Fatal("RerankFiles applied = false, want true for a projectable maximum page")
		}
		if len(got) != count {
			t.Fatalf("RerankFiles returned %d rows, want %d", len(got), count)
		}
		for _, row := range got {
			if row.SearchBackend != "hybrid" {
				t.Fatalf("row %s search_backend = %q, want hybrid (every row ranked)", row.RelativePath, row.SearchBackend)
			}
		}
		if want := fmt.Sprintf("service/handler_%03d.go", count-1); got[0].RelativePath != want {
			t.Errorf("top row = %s, want %s", got[0].RelativePath, want)
		}
	})
}
