// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestRankDeterministicOverRuns runs each query 100 times, on a fresh ranker
// each time (a fresh build re-randomizes Go map iteration), and requires one
// identical ordered result including scores and Why text.
func TestRankDeterministicOverRuns(t *testing.T) {
	tools, entries := synthetic(163)
	queries := []router.Query{
		{Intent: "show deployment story for a service", Limit: 5},
		{Intent: "alpha beta gamma", Limit: 10},
		{Intent: "find the dead code in a repository", Limit: 10, Anchors: router.Anchors{RepoID: "r-1"}},
		{Intent: "zebra", Limit: 5},
	}
	for _, q := range queries {
		var first []router.Candidate
		for run := 0; run < 100; run++ {
			got, err := New(tools, entries).Rank(context.Background(), q, tools, entries, allowAll)
			if err != nil {
				t.Fatalf("Rank: %v", err)
			}
			if run == 0 {
				first = got
				continue
			}
			if len(got) != len(first) {
				t.Fatalf("query %q run %d: %d candidates, want %d", q.Intent, run, len(got), len(first))
			}
			for i := range got {
				if got[i].Name != first[i].Name || got[i].Score != first[i].Score || got[i].Why != first[i].Why ||
					fmt.Sprint(got[i].Example) != fmt.Sprint(first[i].Example) {
					t.Fatalf("query %q run %d position %d: %+v, want %+v", q.Intent, run, i, got[i], first[i])
				}
			}
		}
	}
}
