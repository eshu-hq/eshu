// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbenchrun

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// fakeBackend serves canned candidates and errors keyed by query id.
type fakeBackend struct {
	byQuery map[string][]searchretrieval.Candidate
	errs    map[string]error
	calls   int
}

func (f *fakeBackend) Search(_ context.Context, req searchretrieval.Request) ([]searchretrieval.Candidate, error) {
	f.calls++
	if f.errs != nil {
		if err := f.errs[req.QueryID]; err != nil {
			return nil, err
		}
	}
	return f.byQuery[req.QueryID], nil
}

func derivedDoc(id string, handles ...searchdocs.GraphHandle) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		GraphHandles: handles,
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

// hitSuite builds a valid suite of n queries each expecting a single entity
// handle, plus a fake backend that returns exactly that matching document.
func hitSuite(n int) (searchbench.QuerySuite, *fakeBackend) {
	queries := make([]searchbench.Query, 0, n)
	backend := &fakeBackend{byQuery: make(map[string][]searchretrieval.Candidate, n)}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("q-%02d", i)
		handle := searchdocs.GraphHandle{Kind: "entity", ID: id}
		queries = append(queries, searchbench.Query{
			ID:              id,
			Text:            "find " + id,
			RepoID:          "repo-1",
			Mode:            searchbench.ModeKeyword,
			Limit:           10,
			ExpectedHandles: []string{"entity:" + id},
		})
		backend.byQuery[id] = []searchretrieval.Candidate{{
			Document: derivedDoc("searchdoc:"+id, handle),
			Score:    1,
		}}
	}
	return searchbench.QuerySuite{Version: searchbench.QuerySuiteVersion, Queries: queries}, backend
}

func postgresDescriptor() BackendDescriptor {
	return BackendDescriptor{
		Backend:              searchbench.BackendPostgresContentSearch,
		BackendCommit:        "abc123",
		MemoryHighWaterBytes: 64 << 20,
		RebuildBehavior:      "none",
		QueryTimeout:         time.Second,
	}
}

func TestRunSuitePerfectHits(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	out, err := RunSuite(context.Background(), suite, backend, postgresDescriptor())
	if err != nil {
		t.Fatalf("RunSuite returned error: %v", err)
	}
	if backend.calls != searchbench.MinimumQuerySuiteSize {
		t.Fatalf("backend calls = %d, want %d", backend.calls, searchbench.MinimumQuerySuiteSize)
	}
	if out.Run.Backend != searchbench.BackendPostgresContentSearch {
		t.Errorf("backend = %q, want postgres_content_search", out.Run.Backend)
	}
	if out.Run.Mode != searchbench.ModeKeyword {
		t.Errorf("mode = %q, want keyword", out.Run.Mode)
	}
	if out.Run.QueryCount != searchbench.MinimumQuerySuiteSize {
		t.Errorf("query count = %d, want %d", out.Run.QueryCount, searchbench.MinimumQuerySuiteSize)
	}
	if got := out.Score.Metrics.Recall; got != 1 {
		t.Errorf("recall = %v, want 1", got)
	}
	if got := out.Score.Metrics.Precision; got != 1 {
		t.Errorf("precision = %v, want 1", got)
	}
	if got := out.Score.Metrics.NDCG; got != 1 {
		t.Errorf("ndcg = %v, want 1", got)
	}
	if out.Run.Metrics.FalseCanonicalClaimCount == nil || *out.Run.Metrics.FalseCanonicalClaimCount != 0 {
		t.Errorf("false canonical count = %v, want 0", out.Run.Metrics.FalseCanonicalClaimCount)
	}
	if out.Run.Latency.P50 <= 0 || out.Run.Latency.P95 <= 0 {
		t.Errorf("latency must be positive: p50=%v p95=%v", out.Run.Latency.P50, out.Run.Latency.P95)
	}
	if out.Run.Latency.P95 < out.Run.Latency.P50 {
		t.Errorf("p95 %v < p50 %v", out.Run.Latency.P95, out.Run.Latency.P50)
	}
	if out.Run.BackendCommit != "abc123" || out.Run.MemoryHighWaterBytes != 64<<20 || out.Run.RebuildBehavior != "none" {
		t.Errorf("descriptor metadata not merged: %+v", out.Run)
	}
	if len(out.Observations) != searchbench.MinimumQuerySuiteSize {
		t.Errorf("observations = %d, want %d", len(out.Observations), searchbench.MinimumQuerySuiteSize)
	}
}

func TestRunSuitePerQueryErrorIsRecordedNotFatal(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	// Fail exactly one query; its recall contribution is zero but the run
	// completes and scores the rest.
	backend.errs = map[string]error{"q-00": errors.New("backend boom")}
	out, err := RunSuite(context.Background(), suite, backend, postgresDescriptor())
	if err != nil {
		t.Fatalf("RunSuite returned error: %v", err)
	}
	want := float64(searchbench.MinimumQuerySuiteSize-1) / float64(searchbench.MinimumQuerySuiteSize)
	if got := out.Score.Metrics.Recall; got != want {
		t.Errorf("recall = %v, want %v", got, want)
	}
}

func TestRunSuiteRejectsInvalidSuite(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize - 1)
	if _, err := RunSuite(context.Background(), suite, backend, postgresDescriptor()); err == nil {
		t.Fatal("expected error for undersized suite")
	}
}

func TestRunSuiteRejectsNilBackend(t *testing.T) {
	suite, _ := hitSuite(searchbench.MinimumQuerySuiteSize)
	if _, err := RunSuite(context.Background(), suite, nil, postgresDescriptor()); err == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestRunSuiteRejectsZeroTimeout(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	desc := postgresDescriptor()
	desc.QueryTimeout = 0
	if _, err := RunSuite(context.Background(), suite, backend, desc); err == nil {
		t.Fatal("expected error for zero query timeout")
	}
}

func TestRunSuiteRejectsUnknownBackend(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	desc := postgresDescriptor()
	desc.Backend = searchbench.Backend("mystery")
	if _, err := RunSuite(context.Background(), suite, backend, desc); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestRunSuiteAbortsOnParentCancellation(t *testing.T) {
	suite, backend := hitSuite(searchbench.MinimumQuerySuiteSize)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunSuite(ctx, suite, backend, postgresDescriptor()); err == nil {
		t.Fatal("expected error when parent context is canceled")
	}
}

func TestModeForBackend(t *testing.T) {
	cases := map[searchbench.Backend]searchbench.Mode{
		searchbench.BackendPostgresContentSearch: searchbench.ModeKeyword,
		searchbench.BackendNornicDBBM25:          searchbench.ModeKeyword,
		searchbench.BackendNornicDBVector:        searchbench.ModeSemantic,
		searchbench.BackendNornicDBHybrid:        searchbench.ModeHybrid,
		searchbench.Backend("mystery"):           searchbench.Mode(""),
	}
	for backend, want := range cases {
		if got := modeForBackend(backend); got != want {
			t.Errorf("modeForBackend(%q) = %q, want %q", backend, got, want)
		}
	}
}

func TestPercentile(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	durations := []time.Duration{ms(50), ms(10), ms(40), ms(20), ms(30)} // sorted: 10,20,30,40,50
	if got := percentile(durations, 50); got != ms(30) {
		t.Errorf("p50 = %v, want 30ms", got)
	}
	if got := percentile(durations, 95); got != ms(50) {
		t.Errorf("p95 = %v, want 50ms", got)
	}
	if got := percentile(durations, 0); got != ms(10) {
		t.Errorf("p0 = %v, want 10ms", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("empty p50 = %v, want 0", got)
	}
}
