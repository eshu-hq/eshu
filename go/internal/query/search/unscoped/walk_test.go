// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

const ms = time.Millisecond

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// run searches the corpus with the default 800 ms budget and a fake clock.
func run(
	t *testing.T,
	corpus *fakeCorpus,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (querycontract.FileSearchPage, *fakeDB, *fakeClock, error) {
	t.Helper()
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	return runOn(t, fdb, clock, unscoped.DefaultBudget, limit, offset, cursor)
}

func runOn(
	t *testing.T,
	fdb *fakeDB,
	clock *fakeClock,
	budget time.Duration,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (querycontract.FileSearchPage, *fakeDB, *fakeClock, error) {
	t.Helper()
	searcher := &unscoped.Searcher{Store: fdb, Budget: budget, Now: clock.Now, Logger: quietLogger}
	page, err := searcher.Search(context.Background(), "needle", limit, offset, cursor)
	return page, fdb, clock, err
}

func keys(files []querycontract.FileContent) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.RepoID+"/"+f.RelativePath)
	}
	return out
}

func matchKeys(corpus *fakeCorpus) []string {
	var out []string
	for _, row := range corpus.rows {
		if row.match {
			out = append(out, row.repo+"/"+row.path)
		}
	}
	return out
}

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A pattern with early matches is answered by the probe alone: no continuation,
// no tail, and the page equals the first rows of the ordered match list.
func TestSearchAnswersFromProbeWhenMatchesAreEarly(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(5000, 50*time.Microsecond, func(i int) bool { return i == 3 || i == 17 || i == 40 })
	page, fdb, _, err := run(t, corpus, 2, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got, want := keys(page.Files), matchKeys(corpus)[:2]; !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !page.More {
		t.Fatal("More = false, want true: a third match exists")
	}
	if page.Partial != nil {
		t.Fatalf("Partial = %+v, want nil", page.Partial)
	}
	if got := fdb.tx.labels(t, "step", "tail"); len(got) != 1 || got[0] != "step" {
		t.Fatalf("statements = %v, want one step and no tail", got)
	}
	if fdb.tx.log[0] != "readiness" {
		t.Fatalf("first statement = %q, want readiness", fdb.tx.log[0])
	}
}

// offset is served by reading offset+limit+1 matches and slicing, so the page
// equals the old OFFSET/LIMIT page and More says whether a row follows it.
func TestSearchServesOffsetByReadingPastIt(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(5000, 50*time.Microsecond, func(i int) bool { return i%10 == 3 })
	page, _, _, err := run(t, corpus, 2, 2, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got, want := keys(page.Files), matchKeys(corpus)[2:4]; !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if !page.More {
		t.Fatal("More = false, want true")
	}
}

// A rare token runs the probe, a capped continuation, then the trigram tail
// with index scans off inside a savepoint; the tail finishes, so the answer is
// complete without any partial marker.
func TestSearchRareTokenFinishesInTheTail(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i == 15000 })
	corpus.tailCost = func(int) time.Duration { return 334 * ms }
	page, fdb, clock, err := run(t, corpus, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got, want := keys(page.Files), matchKeys(corpus); !equalKeys(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if page.More || page.Partial != nil {
		t.Fatalf("More=%v Partial=%+v, want a complete exact page", page.More, page.Partial)
	}
	steps := fdb.tx.labels(t, "step", "tail")
	if len(steps) != 6 || steps[5] != "tail" {
		t.Fatalf("phase order = %v, want probe, four capped steps, then the tail", steps)
	}
	if elapsed := clock.Now().Sub(time.Unix(1_700_000_000, 0)); elapsed > unscoped.DefaultBudget {
		t.Fatalf("elapsed = %v, want within the %v budget", elapsed, unscoped.DefaultBudget)
	}
	assertTailShape(t, fdb.tx, "400ms")
}

// assertTailShape checks the tail ran inside a savepoint with index scans off,
// under the expected statement timeout, and that the settings were restored.
func assertTailShape(t *testing.T, tx *fakeTx, wantTimeout string) {
	t.Helper()
	tail := -1
	for i, l := range tx.log {
		if l == "tail" {
			tail = i
		}
	}
	if tail < 3 {
		t.Fatalf("tail not found in %v", tx.log)
	}
	if tx.log[tail-1] != "index_off" || tx.log[tail-2] != "savepoint" {
		t.Fatalf("statements before tail = %v, want savepoint then index_off", tx.log[tail-3:tail])
	}
	timeoutText := ""
	for i := tail - 1; i >= 0; i-- {
		if tx.log[i] == "set_timeout" {
			timeoutText, _ = tx.args[i][0].(string)
			break
		}
	}
	if timeoutText != wantTimeout {
		t.Fatalf("tail statement_timeout = %q, want %q", timeoutText, wantTimeout)
	}
	if tx.indexOff {
		t.Fatal("enable_indexscan still off after the tail")
	}
}

// A key space shorter than the window is exhausted: the answer is complete
// and exact with no tail, even with zero matches.
func TestSearchZeroMatchesInSmallCorpusIsExactWithoutTail(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(150, 50*time.Microsecond, func(int) bool { return false })
	page, fdb, _, err := run(t, corpus, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(page.Files) != 0 || page.More || page.Partial != nil {
		t.Fatalf("page = %+v, want empty exact page", page)
	}
	if got := fdb.tx.labels(t, "step", "tail"); len(got) != 1 {
		t.Fatalf("statements = %v, want one step", got)
	}
}

// A corpus of exactly one window has no edge in the next step, so the walk
// proves exhaustion on the second step and never runs the tail.
func TestSearchExactWindowBoundaryExhaustsOnNextStep(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(200, 50*time.Microsecond, func(int) bool { return false })
	page, fdb, _, err := run(t, corpus, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial != nil || len(page.Files) != 0 {
		t.Fatalf("page = %+v, want empty exact page", page)
	}
	if got := fdb.tx.labels(t, "step", "tail"); len(got) != 2 || got[1] != "step" {
		t.Fatalf("statements = %v, want two steps and no tail", got)
	}
}

// A dense late class cannot finish in the tail: the server cancels it at its
// timeout, the savepoint keeps the transaction usable, the continuation spends
// the rest of the budget, and the answer is an explicit partial prefix.
func TestSearchDenseLateClassReturnsPartialPrefixWithCursor(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%50 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	page, fdb, clock, err := run(t, corpus, 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial == nil {
		t.Fatalf("Partial = nil, want a partial result; page = %+v", page)
	}
	all := matchKeys(corpus)
	got := keys(page.Files)
	if len(got) == 0 || !equalKeys(got, all[:len(got)]) {
		t.Fatalf("files = %v, want a non-empty prefix of %v", got, all)
	}
	partial := page.Partial
	if partial.Reason != querycontract.SearchPartialCandidateBudgetExceeded {
		t.Fatalf("Reason = %q", partial.Reason)
	}
	if partial.Cursor.IsZero() {
		t.Fatal("Cursor is zero, want the last visited key")
	}
	last := corpus.rows[partial.RowsScannedInOrder-1]
	if partial.Cursor.RepoID != last.repo || partial.Cursor.RelativePath != last.path {
		t.Fatalf("Cursor = %+v, want the key of visited row %d (%s/%s)", partial.Cursor, partial.RowsScannedInOrder, last.repo, last.path)
	}
	if partial.RowsMatched != len(got) {
		t.Fatalf("RowsMatched = %d, want %d", partial.RowsMatched, len(got))
	}
	if partial.BudgetMS != 800 {
		t.Fatalf("BudgetMS = %d, want 800", partial.BudgetMS)
	}
	if partial.Hint != querycontract.SearchPartialHint {
		t.Fatalf("Hint = %q", partial.Hint)
	}
	if fdb.tx.cancels != 1 {
		t.Fatalf("cancelled statements = %d, want exactly the tail", fdb.tx.cancels)
	}
	// The fake refuses every statement in an aborted transaction except
	// ROLLBACK TO, so a continuation after the cancel proves the savepoint.
	labels := fdb.tx.labels(t, "step", "tail", "rollback_to")
	tailAt := -1
	for i, l := range labels {
		if l == "tail" {
			tailAt = i
		}
	}
	if tailAt < 0 || labels[tailAt+1] != "rollback_to" || labels[len(labels)-1] != "step" {
		t.Fatalf("statements = %v, want the tail cancelled, rolled back, then continuation steps", labels)
	}
	if elapsed := clock.Now().Sub(time.Unix(1_700_000_000, 0)); elapsed > unscoped.DefaultBudget+50*ms {
		t.Fatalf("elapsed = %v, want about the %v budget", elapsed, unscoped.DefaultBudget)
	}
	if partial.ElapsedMS < 700 || partial.ElapsedMS > 850 {
		t.Fatalf("ElapsedMS = %d, want about the budget", partial.ElapsedMS)
	}
	if partial.OverrunMS != 0 {
		t.Fatalf("OverrunMS = %d, want 0 without a lagging cancel", partial.OverrunMS)
	}
}

// A cancel that runs past its timeout by more than 100 ms is the uninterruptible
// recheck of a large document; the partial says so and reports the overrun.
func TestSearchReportsOverrunAndNamesLargeDocument(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%50 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	corpus.tailLag = 338 * ms
	page, _, _, err := run(t, corpus, 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial == nil {
		t.Fatal("Partial = nil")
	}
	if page.Partial.OverrunMS != 338 {
		t.Fatalf("OverrunMS = %d, want 338", page.Partial.OverrunMS)
	}
	if page.Partial.Reason != querycontract.SearchPartialBudgetExceededOnLargeDocument {
		t.Fatalf("Reason = %q, want large-document reason", page.Partial.Reason)
	}
}

// When too little budget is left for a useful tail, the walk skips it and
// answers with the partial rather than starting a statement it must cancel.
func TestSearchSkipsTailBelowFloor(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(20000, 50*time.Microsecond, func(int) bool { return false })
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	fdb.readyCost = 560 * ms
	page, fdb, _, err := runOn(t, fdb, clock, unscoped.DefaultBudget, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial == nil {
		t.Fatal("Partial = nil, want partial when the tail is skipped")
	}
	for _, l := range fdb.tx.log {
		if l == "tail" {
			t.Fatalf("tail ran with under the floor left: %v", fdb.tx.log)
		}
	}
}

// Resuming from the returned cursor, with the offset reduced by the matches
// already found, concatenates to exactly the ordered match list: no gap, no
// duplicate across partial pages.
func TestSearchCursorResumeHasNoGapAndNoDuplicate(t *testing.T) {
	t.Parallel()

	for _, offset := range []int{0, 20} {
		corpus := newCorpus(6000, 500*time.Microsecond, func(i int) bool { return i%40 == 7 })
		corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
		var gathered []string
		cursor := querycontract.SearchCursor{}
		remainingOffset := offset
		calls := 0
		for {
			calls++
			if calls > 40 {
				t.Fatalf("offset %d: no convergence after %d calls", offset, calls)
			}
			page, _, _, err := run(t, corpus, 200, remainingOffset, cursor)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			gathered = append(gathered, keys(page.Files)...)
			if page.Partial == nil {
				break
			}
			cursor = page.Partial.Cursor
			remainingOffset -= page.Partial.RowsMatched
			if remainingOffset < 0 {
				remainingOffset = 0
			}
		}
		want := matchKeys(corpus)[offset:]
		if !equalKeys(gathered, want) {
			t.Fatalf("offset %d: gathered %d keys, want %d; first diff in %v vs %v", offset, len(gathered), len(want), gathered, want)
		}
		if calls < 2 {
			t.Fatalf("offset %d: finished in %d call, the fixture must force partial pages", offset, calls)
		}
	}
}

// The readiness statement comes first; when it fails nothing else runs and
// the error reaches the caller unchanged for the 503-class mapping.
func TestSearchStopsOnReadinessFailure(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(500, 50*time.Microsecond, func(int) bool { return true })
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	fdb.readyErr = &pgconn.PgError{Code: "55000", Message: "content substring indexes are not ready"}
	_, fdb, _, err := runOn(t, fdb, clock, unscoped.DefaultBudget, 10, 0, querycontract.SearchCursor{})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("error = %v, want the SQLSTATE 55000 error", err)
	}
	if len(fdb.tx.log) != 1 || !fdb.tx.rolledBack {
		t.Fatalf("log = %v rolledBack=%v, want readiness only and a rollback", fdb.tx.log, fdb.tx.rolledBack)
	}
}

func TestSearchRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	corpus := newCorpus(10, 0, func(int) bool { return true })
	clock := newFakeClock()
	searcher := &unscoped.Searcher{Store: newFakeDB(corpus, clock), Now: clock.Now, Logger: quietLogger}
	cases := []struct {
		name            string
		pattern         string
		limit, offset   int
		wantErrContains string
	}{
		{"empty pattern", "", 10, 0, "pattern"},
		{"zero limit", "x", 0, 0, "limit"},
		{"negative offset", "x", 10, -1, "offset"},
	}
	for _, tc := range cases {
		if _, err := searcher.Search(context.Background(), tc.pattern, tc.limit, tc.offset, querycontract.SearchCursor{}); err == nil {
			t.Errorf("%s: error = nil", tc.name)
		}
	}
}

func TestBudgetFromEnv(t *testing.T) {
	t.Parallel()

	get := func(v string) func(string) string {
		return func(string) string { return v }
	}
	if got, err := unscoped.BudgetFromEnv(get("")); err != nil || got != 800*ms {
		t.Fatalf("unset = (%v, %v), want 800ms", got, err)
	}
	if got, err := unscoped.BudgetFromEnv(get(" 1200 ")); err != nil || got != 1200*ms {
		t.Fatalf("1200 = (%v, %v)", got, err)
	}
	for _, bad := range []string{"abc", "0", "-5", "50", "20000", "1.5"} {
		if _, err := unscoped.BudgetFromEnv(get(bad)); err == nil {
			t.Errorf("BudgetFromEnv(%q) error = nil, want a startup error", bad)
		}
	}
}
