// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
)

// reversingRanker reverses the page it is handed, so a re-rank over a window
// that differs from page to page changes the order a client pages through.
type reversingRanker struct{ applied bool }

func (r reversingRanker) Rerank(_ context.Context, _, _ string, rows []map[string]any) ([]map[string]any, bool) {
	if !r.applied || len(rows) < 2 {
		return rows, false
	}
	out := make([]map[string]any, 0, len(rows))
	for index := len(rows) - 1; index >= 0; index-- {
		out = append(out, rows[index])
	}
	return out, true
}

func rankedFallbackHandler(rows int, ranker reversingRanker) *CodeHandler {
	entities := make([]EntityContent, 0, rows)
	for index := 0; index < rows; index++ {
		entities = append(entities, EntityContent{
			RepoID: "repo-a", EntityID: fmt.Sprintf("entity-%03d", index), EntityName: "decode", EntityType: "Function",
		})
	}
	return &CodeHandler{
		Neo4j:        fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil }},
		Content:      &recordingCodeSearchContentStore{byRepo: map[string][]EntityContent{"repo-a": entities}},
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: ranker,
	}
}

// TestCodeSearchContentFallbackPagesAreOffsetWindowsWhenReranked is the #7725
// review F1 site B regression. The hybrid re-rank must run inside the page,
// never over the growing probe window, so a plain offset walk with limit < 200
// returns every row exactly once and each page is the same window of the
// offset order that an unranked walk returns.
func TestCodeSearchContentFallbackPagesAreOffsetWindowsWhenReranked(t *testing.T) {
	t.Parallel()

	const rows, limit = 60, 25
	handler := rankedFallbackHandler(rows, reversingRanker{applied: true})
	var walk []string
	for offset := 0; offset < rows; offset += limit {
		status, response := searchWithBody(t, handler, fmt.Sprintf(`{"query":"decode","repo_id":"repo-a","limit":%d,"offset":%d}`, limit, offset))
		if status != http.StatusOK {
			t.Fatalf("offset %d status = %d, want 200", offset, status)
		}
		ids := resultIDs(t, response)
		window := append([]string(nil), ids...)
		sort.Strings(window)
		for index, id := range window {
			if want := fmt.Sprintf("entity-%03d", offset+index); id != want {
				t.Fatalf("page at offset %d holds %v, want the offset window %d.. in some order", offset, ids, offset)
			}
		}
		items, _ := response["results"].([]any)
		for index, item := range items {
			position, present := item.(map[string]any)["page_position"]
			if len(items) < 2 {
				continue
			}
			if !present {
				t.Fatalf("re-ranked row %d at offset %d lacks page_position", index, offset)
			}
			if got, want := ids[index], fmt.Sprintf("entity-%03d", offset+int(position.(float64))); got != want {
				t.Fatalf("row %d at offset %d is %s with page_position %v, want %s", index, offset, got, position, want)
			}
		}
		walk = append(walk, ids...)
	}
	if len(walk) != rows {
		t.Fatalf("walk returned %d rows, want %d", len(walk), rows)
	}
}

// TestCodeSearchContentFallbackKeepsOffsetOrderWhenRankerDoesNotApply proves a
// page the ranker left alone carries no page_position key, so its response
// bytes match an unranked page.
func TestCodeSearchContentFallbackKeepsOffsetOrderWhenRankerDoesNotApply(t *testing.T) {
	t.Parallel()

	handler := rankedFallbackHandler(10, reversingRanker{applied: false})
	_, response := searchWithBody(t, handler, `{"query":"decode","repo_id":"repo-a","limit":5}`)
	items, _ := response["results"].([]any)
	if len(items) != 5 {
		t.Fatalf("rows = %d, want 5", len(items))
	}
	for _, item := range items {
		if _, present := item.(map[string]any)["page_position"]; present {
			t.Fatalf("row %v carries page_position although the ranker did not apply", item)
		}
	}
}

// TestCodeSearchPageEchoesTheRequestedOffsetPastTheRows proves an offset past
// the rows found is echoed as asked, not clamped to the row count (#7725
// review F7).
func TestCodeSearchPageEchoesTheRequestedOffsetPastTheRows(t *testing.T) {
	t.Parallel()

	handler := rankedFallbackHandler(10, reversingRanker{})
	status, response := searchWithBody(t, handler, `{"query":"decode","repo_id":"repo-a","limit":5,"offset":150}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if response["offset"] != float64(150) || response["count"] != float64(0) {
		t.Fatalf("offset/count = %v/%v, want 150/0", response["offset"], response["count"])
	}
}
