// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchretrieval

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func BenchmarkBuildResponse(b *testing.B) {
	req := Request{
		Query:   "payment refund token",
		Scope:   Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   20,
		Timeout: time.Second,
	}
	candidates := makeBenchCandidates(100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildResponse(req, candidates)
	}
}

func BenchmarkBuildResponseLarge(b *testing.B) {
	req := Request{
		Query:   "payment refund token",
		Scope:   Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   50,
		Timeout: time.Second,
	}
	candidates := makeBenchCandidates(1000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildResponse(req, candidates)
	}
}

func BenchmarkValidateRequest(b *testing.B) {
	req := Request{
		Query:   "payment refund",
		Scope:   Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   10,
		Timeout: time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateRequest(req)
	}
}

func BenchmarkRunnerRetrieve(b *testing.B) {
	req := Request{
		QueryID: "q-1",
		Query:   "payment refund token",
		Scope:   Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   20,
		Timeout: time.Second,
	}
	candidates := makeBenchCandidates(100)
	backend := &fakeRetrievalBackend{candidates: candidates}
	runner := Runner{Backend: backend}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = runner.Retrieve(context.Background(), req)
	}
}

func BenchmarkRunnerRetrieveWithObserver(b *testing.B) {
	req := Request{
		QueryID: "q-1",
		Query:   "payment refund token",
		Scope:   Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeHybrid,
		Limit:   20,
		Timeout: time.Second,
	}
	candidates := makeBenchCandidates(100)
	backend := &fakeRetrievalBackend{candidates: candidates}
	observer := &recordingRetrievalObserver{}
	runner := Runner{Backend: backend, Observer: observer}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = runner.Retrieve(context.Background(), req)
	}
}

func makeBenchCandidates(n int) []Candidate {
	candidates := make([]Candidate, n)
	for i := range candidates {
		candidates[i] = Candidate{
			Document: searchdocs.Document{
				ID:          "d-" + strconv.Itoa(i),
				RepoID:      "repo-1",
				Title:       "doc " + strconv.Itoa(i),
				ContextText: "sample content for document " + strconv.Itoa(i),
				SourceKind:  searchdocs.SourceKindCodeEntity,
				TruthScope:  searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
				Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
				GraphHandles: []searchdocs.GraphHandle{
					{Kind: "repository", ID: "repo-1"},
					{Kind: "content_entity", ID: "d-" + strconv.Itoa(i)},
				},
			},
			Score: float64(n - i),
		}
	}
	return candidates
}
