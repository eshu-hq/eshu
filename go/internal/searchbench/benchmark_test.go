// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func BenchmarkScoreQueryResults(b *testing.B) {
	query := Query{
		ID:              "q-1",
		Text:            "payment refund token",
		Mode:            ModeHybrid,
		Limit:           10,
		RepoID:          "repo-1",
		ExpectedHandles: []string{"content_entity:d-0", "content_entity:d-5", "content_entity:d-9"},
	}
	results := make([]Result, 100)
	for i := range results {
		results[i] = Result{
			Document: searchdocs.Document{
				ID:          "d-" + strconv.Itoa(i),
				RepoID:      "repo-1",
				Title:       "doc " + strconv.Itoa(i),
				ContextText: "sample content",
				SourceKind:  searchdocs.SourceKindCodeEntity,
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
				GraphHandles: []searchdocs.GraphHandle{
					{Kind: "content_entity", ID: "d-" + strconv.Itoa(i)},
				},
			},
			Rank: i + 1,
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ScoreQueryResults(query, results)
	}
}

func BenchmarkScoreQuerySuite(b *testing.B) {
	queries := make([]Query, 30)
	for i := range queries {
		queries[i] = Query{
			ID:              "q-" + strconv.Itoa(i),
			Text:            "query text " + strconv.Itoa(i),
			Mode:            ModeHybrid,
			Limit:           10,
			RepoID:          "repo-1",
			ExpectedHandles: []string{"content_entity:d-" + strconv.Itoa(i)},
		}
	}
	suite := QuerySuite{
		Version: QuerySuiteVersion,
		Queries: queries,
	}
	resultsByQueryID := make(map[string][]Result, len(queries))
	for i, q := range queries {
		resultsByQueryID[q.ID] = []Result{
			{
				Document: searchdocs.Document{
					ID:         "d-" + strconv.Itoa(i),
					RepoID:     "repo-1",
					Title:      "doc " + strconv.Itoa(i),
					SourceKind: searchdocs.SourceKindCodeEntity,
					TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
					Freshness:  searchdocs.Freshness{State: searchdocs.FreshnessFresh},
					GraphHandles: []searchdocs.GraphHandle{
						{Kind: "content_entity", ID: "d-" + strconv.Itoa(i)},
					},
				},
				Rank: 1,
			},
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ScoreQuerySuite(suite, resultsByQueryID)
	}
}
