// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// TestBackendThroughRetrievalRunner proves the backend conforms to the bounded
// retrieval contract end to end: the runner validates the request, normalizes
// candidates into ranked results with derived truth labels, and reports
// truncation when more than the limit matched.
func TestBackendThroughRetrievalRunner(t *testing.T) {
	t.Parallel()

	runner := searchretrieval.Runner{
		Backend: Backend{Index: mustIndex(t, corpus(), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})},
	}

	response, err := runner.Retrieve(context.Background(), searchretrieval.Request{
		Query:   "process payment refund",
		Scope:   searchretrieval.Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   1,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Retrieve error = %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("results = %d, want 1 (bounded by limit)", len(response.Results))
	}
	if !response.Truncated {
		t.Error("expected truncated = true when more than limit matched")
	}
	top := response.Results[0]
	if top.Rank != 1 {
		t.Errorf("top rank = %d, want 1", top.Rank)
	}
	if top.Document.RepoID != "repo-1" {
		t.Errorf("out-of-scope result: %q", top.Document.RepoID)
	}
	if top.TruthScope.Level != searchdocs.TruthLevelDerived {
		t.Errorf("truth level = %q, want derived", top.TruthScope.Level)
	}
	if response.FalseCanonicalClaimCount != 0 {
		t.Errorf("false canonical claims = %d, want 0", response.FalseCanonicalClaimCount)
	}
}
