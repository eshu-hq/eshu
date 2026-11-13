// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestRerankFileResultsPreserveSnippets proves the bounded hybrid re-rank
// passes the 05a snippet fields through untouched: rows reordered by fused
// rank keep their own match_snippet and match_line, and unranked rows keep
// theirs in lexical order.
func TestRerankFileResultsPreserveSnippets(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		byFileRepo: map[string][]querycontract.FileContent{
			"repo-team-a": {
				{
					RepoID:       "repo-team-a",
					RelativePath: "billing/totals.go",
					Language:     "go",
					Content:      "package billing\nfunc processOrderTotals() { return sum(prices) }",
					MatchSnippet: "processOrderTotals() { return sum",
					MatchLine:    2,
				},
				{
					RepoID:       "repo-team-a",
					RelativePath: "payments/refund.go",
					Language:     "go",
					Content:      "process payment refund: validate payment, process refund, emit payment refund event",
					MatchSnippet: "process payment refund: validate",
					MatchLine:    1,
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
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	matches := decodeContentMatches(t, rec)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", matches)
	}
	want := map[string]map[string]any{
		"billing/totals.go":  {"match_snippet": "processOrderTotals() { return sum", "match_line": float64(2)},
		"payments/refund.go": {"match_snippet": "process payment refund: validate", "match_line": float64(1)},
	}
	for _, match := range matches {
		path, _ := match["relative_path"].(string)
		fields, ok := want[path]
		if !ok {
			t.Fatalf("unexpected row %q", path)
		}
		for key, value := range fields {
			if match[key] != value {
				t.Errorf("row %s: %s = %v, want %v (re-rank must pass snippets through)", path, key, match[key], value)
			}
		}
	}
}
