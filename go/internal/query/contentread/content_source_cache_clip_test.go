// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSearchEntityContentClipsAfterHybridRerank proves the read-time clip runs
// after the hybrid re-rank: the strong match's query terms sit past the 4,096
// byte clip, so a clip applied before the rerank would rank it last. The
// returned body is clipped, the row reports the clip, and the response reports
// the ceiling and the clipped-row count.
func TestSearchEntityContentClipsAfterHybridRerank(t *testing.T) {
	t.Parallel()

	strongBody := strings.Repeat("filler ", 800) +
		"process payment refund: validate payment, process refund, emit payment refund event"
	store := &recordingContentAuthzStore{
		byEntityRepo: map[string][]querycontract.EntityContent{
			"repo-team-a": {
				{
					RepoID: "repo-team-a", EntityID: "entity-weak", EntityName: "processOrderTotals",
					EntityType: "function", RelativePath: "billing/totals.go", Language: "go",
					SourceCache: "func processOrderTotals() { return sum(prices) }",
				},
				{
					RepoID: "repo-team-a", EntityID: "entity-strong", EntityName: "processPaymentRefund",
					EntityType: "function", RelativePath: "payments/refund.go", Language: "go",
					StartLine: 7, EndLine: 120, SourceCache: strongBody,
				},
			},
		},
	}
	handler := &ContentHandler{
		Content:      store,
		Profile:      querycontract.ProfileLocalAuthoritative,
		HybridRanker: NewContentHybridRanker(true),
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	rec := httptest.NewRecorder()

	handler.searchEntities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	matches := decodeContentMatches(t, rec)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", matches)
	}
	first := matches[0]
	if first["entity_id"] != "entity-strong" {
		t.Fatalf("top entity_id = %v, want entity-strong (the rerank must see the full body before the clip)", first["entity_id"])
	}
	if got := len(first["source_cache"].(string)); got != querycontract.SourceCacheClipBytes {
		t.Fatalf("top source_cache = %d bytes, want the %d byte clip", got, querycontract.SourceCacheClipBytes)
	}
	if first["source_cache_clipped"] != true || first["source_cache_total_bytes"] != float64(len(strongBody)) {
		t.Fatalf("top row markers = clipped:%v total:%v, want true/%d", first["source_cache_clipped"], first["source_cache_total_bytes"], len(strongBody))
	}
	handle, _ := first["source_handle"].(map[string]any)
	if handle["repo_id"] != "repo-team-a" || handle["file_path"] != "payments/refund.go" || handle["start_line"] != float64(7) || handle["end_line"] != float64(120) {
		t.Fatalf("top source_handle = %#v, want the drill-down locator", first["source_handle"])
	}
	if _, present := matches[1]["source_cache_clipped"]; present {
		t.Fatalf("small row carries source_cache_clipped, want markers only on clipped rows")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body["source_cache_clip_bytes"] != float64(4096) || body["source_cache_clipped_rows"] != float64(1) {
		t.Fatalf("response markers = clip_bytes:%v clipped_rows:%v, want 4096/1", body["source_cache_clip_bytes"], body["source_cache_clipped_rows"])
	}
}
