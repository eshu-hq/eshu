// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// resumeSearchCall is one budgeted unscoped search call that resumes from the
// given cursor. The live test binds it to a real Searcher; the hermetic tests
// bind it to a scripted fake.
type resumeSearchCall func(cursor querycontract.SearchCursor) (querycontract.FileSearchPage, error)

// resumeRun reports what one cursor-resume run saw.
type resumeRun struct {
	// progressed counts partial pages that advanced the cursor. Only such a
	// page shows the budget cut the search short after real work.
	progressed int
	// stalls counts partial pages that scanned nothing inside the budget.
	stalls int
	// abandoned is true when the run gave up after maxConsecutiveStalls
	// stalled calls in a row, so the rung proved nothing about exactness.
	abandoned bool
}

const (
	// maxConsecutiveStalls bounds how often the same request is re-issued
	// after a call that scanned nothing before the rung is abandoned.
	maxConsecutiveStalls = 5
	// stallPause lets a loaded host drain before the same request is re-issued.
	stallPause = 50 * time.Millisecond
)

// resumeNoTrigramSearch pages one query through cursor resume and returns what
// it saw. A partial page that made progress must move the cursor. A partial
// page that scanned nothing is legal on a loaded host: it must keep the
// request cursor, carry no rows, and name the no-progress hint, and the run
// re-issues the same request. The run fails the test unless the rows gathered
// from a complete run equal the old statement's rows exactly, in order, with
// no gap and no duplicate.
func resumeNoTrigramSearch(t *testing.T, budget time.Duration, search resumeSearchCall, want []string) resumeRun {
	t.Helper()
	var run resumeRun
	var gathered []string
	cursor := querycontract.SearchCursor{}
	consecutiveStalls := 0
	for call := 0; call < 400; call++ {
		page, err := search(cursor)
		if err != nil {
			t.Fatalf("budget %d ms call %d: %v", budget.Milliseconds(), call, err)
		}
		if page.Partial == nil {
			gathered = append(gathered, pageKeys(page.Files)...)
			if strings.Join(gathered, ",") != strings.Join(want, ",") {
				t.Fatalf("budget %d ms: gathered %v, want %v (after %d progressed partial pages)", budget.Milliseconds(), gathered, want, run.progressed)
			}
			return run
		}
		if !page.Partial.Progressed {
			assertNoProgressPartial(t, budget, call, cursor, page)
			run.stalls++
			consecutiveStalls++
			if consecutiveStalls >= maxConsecutiveStalls {
				run.abandoned = true
				return run
			}
			time.Sleep(stallPause)
			continue
		}
		consecutiveStalls = 0
		gathered = append(gathered, pageKeys(page.Files)...)
		run.progressed++
		if page.Partial.Cursor == cursor {
			t.Fatalf("budget %d ms call %d: progressed partial page did not advance the cursor: %+v", budget.Milliseconds(), call, page.Partial)
		}
		cursor = page.Partial.Cursor
	}
	t.Fatalf("budget %d ms: no convergence in 400 calls; gathered %v", budget.Milliseconds(), gathered)
	return run
}

// assertNoProgressPartial checks the contract of a partial page that scanned
// nothing: the cursor is the request cursor, no rows were scanned or matched,
// the page carries no rows, and the hint tells the client not to resume.
func assertNoProgressPartial(t *testing.T, budget time.Duration, call int, request querycontract.SearchCursor, page querycontract.FileSearchPage) {
	t.Helper()
	p := page.Partial
	switch {
	case p.Cursor != request:
		t.Fatalf("budget %d ms call %d: no-progress partial moved the cursor from %+v: %+v", budget.Milliseconds(), call, request, p)
	case p.RowsScannedInOrder != 0 || p.RowsMatched != 0:
		t.Fatalf("budget %d ms call %d: no-progress partial reports scanned or matched rows: %+v", budget.Milliseconds(), call, p)
	case len(page.Files) != 0:
		t.Fatalf("budget %d ms call %d: no-progress partial carries rows %v", budget.Milliseconds(), call, pageKeys(page.Files))
	case p.Hint != querycontract.SearchPartialNoProgressHint:
		t.Fatalf("budget %d ms call %d: no-progress partial hint = %q, want the no-progress hint", budget.Milliseconds(), call, p.Hint)
	}
}

// scriptedResume returns a resumeSearchCall that serves the pages in order and
// fails the test when the run asks for more calls than the script holds.
func scriptedResume(t *testing.T, pages ...querycontract.FileSearchPage) resumeSearchCall {
	t.Helper()
	next := 0
	return func(querycontract.SearchCursor) (querycontract.FileSearchPage, error) {
		if next >= len(pages) {
			t.Fatalf("scripted search asked for call %d, script has %d pages", next, len(pages))
		}
		page := pages[next]
		next++
		return page, nil
	}
}

func scriptedFile(repo, path string) querycontract.FileContent {
	return querycontract.FileContent{RepoID: repo, RelativePath: path}
}

func stalledPartial(cursor querycontract.SearchCursor) querycontract.FileSearchPage {
	return querycontract.FileSearchPage{Partial: &querycontract.SearchPartial{
		Reason:     querycontract.SearchPartialCandidateBudgetExceeded,
		Cursor:     cursor,
		Progressed: false,
		Hint:       querycontract.SearchPartialNoProgressHint,
	}}
}

// TestResumeAcceptsNoProgressPartialAndReissues pins the contract that a loaded
// host may answer a call with a partial page that scanned nothing: the resume
// run must treat it as a stall, re-issue the same request, and still gather
// exactly the old rows. The blanket rule that every partial page advances the
// cursor contradicts the shipped zero-progress contract and failed here before.
func TestResumeAcceptsNoProgressPartialAndReissues(t *testing.T) {
	a := scriptedFile("r", "a")
	b := scriptedFile("r", "b")
	c := scriptedFile("r", "c")
	atA := querycontract.SearchCursor{RepoID: "r", RelativePath: "a"}
	search := scriptedResume(t,
		stalledPartial(querycontract.SearchCursor{}),
		querycontract.FileSearchPage{Files: []querycontract.FileContent{a}, Partial: &querycontract.SearchPartial{
			Reason: querycontract.SearchPartialCandidateBudgetExceeded, RowsScannedInOrder: 1, RowsMatched: 1,
			Cursor: atA, Progressed: true, Hint: querycontract.SearchPartialHint,
		}},
		stalledPartial(atA),
		querycontract.FileSearchPage{Files: []querycontract.FileContent{b, c}},
	)
	run := resumeNoTrigramSearch(t, 100*time.Millisecond, search, []string{"r/a", "r/b", "r/c"})
	if run.progressed != 1 || run.stalls != 2 || run.abandoned {
		t.Fatalf("run = %+v, want 1 progressed partial, 2 stalls, not abandoned", run)
	}
}

// TestResumeAbandonsARungThatNeverProgresses pins that a rung whose every call
// scans nothing proves nothing, is abandoned after maxConsecutiveStalls, and
// reports no progressed partial, so the live ladder can fail (never skip) when
// no rung cuts the tail after real work.
func TestResumeAbandonsARungThatNeverProgresses(t *testing.T) {
	pages := make([]querycontract.FileSearchPage, maxConsecutiveStalls)
	for i := range pages {
		pages[i] = stalledPartial(querycontract.SearchCursor{})
	}
	run := resumeNoTrigramSearch(t, 15*time.Millisecond, scriptedResume(t, pages...), []string{"r/a"})
	if run.progressed != 0 || run.stalls != maxConsecutiveStalls || !run.abandoned {
		t.Fatalf("run = %+v, want 0 progressed, %d stalls, abandoned", run, maxConsecutiveStalls)
	}
}
