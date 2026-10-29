// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// A pattern with no three-character alphanumeric run yields no trigram for
// pg_trgm to extract, so the tail statement can only plan a sequential scan
// (measured on the QA environment read replica at 3,930 ms warm for `db_` against a
// 1000 ms tail cap, #8002). The walk must skip that tail, spend the whole
// remaining budget in key order, and state the limit in the partial reason
// instead of burning the cap on a statement it always cancels.
func TestSearchSkipsTailWhenPatternHasNoTrigram(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 500*time.Microsecond, func(i int) bool { return i%40 == 7 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	searcher := &unscoped.Searcher{Store: fdb, Now: clock.Now, Logger: quietLogger}
	page, err := searcher.Search(context.Background(), "db_", 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, l := range fdb.tx.log {
		if l == "tail" {
			t.Fatalf("tail ran for a pattern with no extractable trigram: %v", fdb.tx.log)
		}
	}
	if page.Partial == nil {
		t.Fatal("Partial = nil, want partial: the fixture budget cannot finish in key order")
	}
	if page.Partial.Reason != querycontract.SearchPartialPatternHasNoTrigram {
		t.Fatalf("Reason = %q, want %q", page.Partial.Reason, querycontract.SearchPartialPatternHasNoTrigram)
	}
	if !page.Partial.Progressed {
		t.Fatal("Progressed = false, want true: key-ordered windows completed")
	}
}

// The same corpus with a trigram-bearing pattern keeps today's behavior: the
// tail runs, is cancelled over its cap, and the partial keeps the budget
// reason. This pins the skip to the no-trigram class only.
func TestSearchKeepsTailWhenPatternHasTrigram(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 500*time.Microsecond, func(i int) bool { return i%40 == 7 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	searcher := &unscoped.Searcher{Store: fdb, Now: clock.Now, Logger: quietLogger}
	page, err := searcher.Search(context.Background(), "needle", 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	tailRan := false
	for _, l := range fdb.tx.log {
		if l == "tail" {
			tailRan = true
		}
	}
	if !tailRan {
		t.Fatalf("tail did not run for a trigram-bearing pattern: %v", fdb.tx.log)
	}
	if page.Partial == nil {
		t.Fatal("Partial = nil, want partial: the tail exceeds its cap")
	}
	if page.Partial.Reason != querycontract.SearchPartialCandidateBudgetExceeded {
		t.Fatalf("Reason = %q, want %q", page.Partial.Reason, querycontract.SearchPartialCandidateBudgetExceeded)
	}
}
