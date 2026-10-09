// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// The shipped SQL carries the exactness invariants the fake cannot assume: a
// strict keyset operator (the cursor row was already read), and an edge row at
// the window's last position (OFFSET size-1). These tests pin the text so a
// changed operator or off-by-one offset fails before any database runs.
func TestShippedStatementsCarryStrictKeysetAndLastRowEdge(t *testing.T) {
	t.Parallel()

	statements := unscoped.Statements()
	const stepKeyset = "(repo_id, relative_path) > ($4::text, $5::text)"
	if got := strings.Count(statements.StepAfter, stepKeyset); got != 2 {
		t.Errorf("StepAfter has %d strict keyset predicates, want 2 (window and edge branch)", got)
	}
	const tailKeyset = "(repo_id, relative_path) > ($3::text, $4::text)"
	if got := strings.Count(statements.TailAfter, tailKeyset); got != 1 {
		t.Errorf("TailAfter has %d strict keyset predicates, want 1", got)
	}
	for name, sql := range map[string]string{
		"StepFirst": statements.StepFirst, "StepAfter": statements.StepAfter,
		"TailFirst": statements.TailFirst, "TailAfter": statements.TailAfter,
	} {
		if strings.Contains(sql, ">=") || strings.Contains(sql, "<") {
			t.Errorf("%s uses an inclusive or reversed comparison: %s", name, sql)
		}
		if !strings.Contains(sql, "ORDER BY repo_id, relative_path") {
			t.Errorf("%s lost its key order", name)
		}
	}
	for name, sql := range map[string]string{"StepFirst": statements.StepFirst, "StepAfter": statements.StepAfter} {
		if got := strings.Count(sql, "OFFSET ($2::bigint - 1) LIMIT 1"); got != 1 {
			t.Errorf("%s edge branch must be OFFSET ($2::bigint - 1) LIMIT 1, found %d", name, got)
		}
	}
	for name, sql := range map[string]string{"StepFirst": statements.StepFirst, "TailFirst": statements.TailFirst} {
		if strings.Contains(sql, "(repo_id, relative_path) >") {
			t.Errorf("%s must not carry a keyset predicate", name)
		}
	}
	for name, sql := range map[string]string{"TailFirst": statements.TailFirst, "TailAfter": statements.TailAfter} {
		if !strings.HasSuffix(strings.TrimSpace(sql), "LIMIT $2::bigint") {
			t.Errorf("%s must end with LIMIT $2::bigint (the live oracle derives from it): %s", name, sql)
		}
	}
}

// Matches placed on every window edge row and on the row after it are returned
// exactly once and in order: a duplicate (inclusive keyset) or a gap (edge
// offset past the window) changes the list. The fake takes the operator and the
// OFFSET from the statement text, so this fails on a wrong shipped statement.
func TestSearchEdgeRowMatchesAppearExactlyOnce(t *testing.T) {
	t.Parallel()

	edgeRows := map[int]bool{199: true, 200: true, 699: true, 700: true, 1199: true, 1200: true, 1400: true}
	corpus := newCorpus(1500, 50*time.Microsecond, func(i int) bool { return edgeRows[i] })
	page, _, _, err := run(t, corpus, 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial != nil {
		t.Fatalf("Partial = %+v, want an exact walk", page.Partial)
	}
	if got, want := keys(page.Files), matchKeys(corpus); !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

// The same invariant across the step-to-tail handoff: the tail starts after the
// last completed window, whose edge row matches, and must not return it again.
func TestSearchTailDoesNotRepeatTheCursorRow(t *testing.T) {
	t.Parallel()

	rows := map[int]bool{199: true, 200: true, 2199: true, 2200: true, 15000: true}
	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return rows[i] })
	corpus.tailCost = func(int) time.Duration { return 334 * ms }
	page, fdb, _, err := run(t, corpus, 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := fdb.tx.labels(t, "tail"); len(got) != 1 {
		t.Fatalf("tail statements = %v, want the tail to run once", got)
	}
	if got, want := keys(page.Files), matchKeys(corpus); !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

// A page the tail fills exactly (it returns every row it was asked for, the
// look-ahead row included) is not the end of the answer: More must be true.
// A tail asked for one row too few would return a short list that the walk
// reads as "the key space ended", reporting a page that looks complete.
func TestSearchTailFilledPageReportsMore(t *testing.T) {
	t.Parallel()

	rows := map[int]bool{15000: true, 15001: true, 15002: true}
	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return rows[i] })
	corpus.tailCost = func(int) time.Duration { return 334 * ms }
	page, fdb, _, err := run(t, corpus, 2, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := fdb.tx.labels(t, "tail"); len(got) != 1 {
		t.Fatalf("tail statements = %v, want the tail to fill the page", got)
	}
	if got, want := keys(page.Files), matchKeys(corpus)[:2]; !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !page.More || page.Partial != nil {
		t.Fatalf("More=%v Partial=%+v, want a complete page with a further match", page.More, page.Partial)
	}
}
