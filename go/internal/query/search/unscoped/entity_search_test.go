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

// runEntities searches the entity corpus with the default 800 ms budget and a
// fake clock.
func runEntities(
	t *testing.T,
	corpus *fakeCorpus,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (unscoped.EntityPage, *fakeDB, *fakeClock, error) {
	t.Helper()
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	return runEntitiesOn(t, fdb, clock, unscoped.DefaultBudget, limit, offset, cursor)
}

func runEntitiesOn(
	t *testing.T,
	fdb *fakeDB,
	clock *fakeClock,
	budget time.Duration,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (unscoped.EntityPage, *fakeDB, *fakeClock, error) {
	t.Helper()
	searcher := &unscoped.Searcher{Store: fdb, Budget: budget, Now: clock.Now, Logger: quietLogger}
	page, err := searcher.SearchEntities(context.Background(), "needle", limit, offset, cursor)
	return page, fdb, clock, err
}

func entityKeys(hits []querycontract.EntitySearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Entity.EntityID)
	}
	return out
}

func matchEntityKeys(corpus *fakeCorpus) []string {
	var out []string
	for _, row := range corpus.entities {
		if row.match {
			out = append(out, row.id)
		}
	}
	return out
}

// An exact first page carries the same rows the old single statement returned:
// the ordered match prefix with the full payload (source and raw metadata),
// not just the keys.
func TestEntitySearchExactFirstPageCarriesFullPayload(t *testing.T) {
	t.Parallel()

	corpus := newEntityCorpus(600, 50*time.Microsecond, func(i int) bool { return i%7 == 0 })
	page, _, _, err := runEntities(t, corpus, 10, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("SearchEntities() error = %v", err)
	}
	if page.Partial != nil {
		t.Fatalf("Partial = %+v, want nil", page.Partial)
	}
	if got, want := entityKeys(page.Hits), matchEntityKeys(corpus)[:10]; !equalKeys(got, want) {
		t.Fatalf("entities = %v, want %v", got, want)
	}
	for _, h := range page.Hits {
		if h.Entity.SourceCache == "" {
			t.Fatalf("hit %s carries no source_cache", h.Entity.EntityID)
		}
		if len(h.MetadataRaw) == 0 {
			t.Fatalf("hit %s carries no raw metadata", h.Entity.EntityID)
		}
		if h.Entity.Metadata != nil {
			t.Fatalf("hit %s has decoded metadata: the query layer owns the one decode", h.Entity.EntityID)
		}
	}
}

// A budget that ends mid-walk returns the reason-coded partial envelope, not
// an error and not a page that looks complete: truncated rows, the budget
// reason, the progressed flag, and a four-part resume cursor.
func TestEntitySearchBudgetStopReturnsReasonCodedPartial(t *testing.T) {
	t.Parallel()

	// Matches start past the probe and every row costs 2 ms: the probe alone
	// costs 400 ms, continuation cannot afford a step, and the tail (3 s) is
	// cancelled, so the walk ends partial with the probe's cursor.
	corpus := newEntityCorpus(3000, 2*time.Millisecond, func(i int) bool { return i >= 1500 && i%11 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	page, fdb, _, err := runEntities(t, corpus, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("SearchEntities() error = %v", err)
	}
	partial := page.Partial
	if partial == nil {
		t.Fatal("Partial = nil, want a reason-coded partial envelope")
	}
	if partial.Reason != querycontract.SearchPartialCandidateBudgetExceeded {
		t.Errorf("reason = %q, want %q", partial.Reason, querycontract.SearchPartialCandidateBudgetExceeded)
	}
	if !partial.Progressed {
		t.Error("progressed = false, want true: the probe completed")
	}
	if partial.Cursor.IsZero() {
		t.Error("cursor is zero: resuming must continue after the visited rows")
	}
	if partial.Cursor.StartLine == 0 || partial.Cursor.EntityID == "" {
		t.Errorf("cursor = %+v, want the full four-part key: entities share files", partial.Cursor)
	}
	if partial.RowsScannedInOrder == 0 {
		t.Error("rows_scanned_in_order = 0, want the visited window rows")
	}
	last := corpus.entities[partial.RowsScannedInOrder-1]
	if partial.Cursor.RepoID != last.repo || partial.Cursor.RelativePath != last.path ||
		partial.Cursor.StartLine != last.start || partial.Cursor.EntityID != last.id {
		t.Errorf("cursor = %+v, want the key of visited row %d", partial.Cursor, partial.RowsScannedInOrder)
	}
	if partial.RowsMatched != len(page.Hits) {
		t.Errorf("rows_matched = %d, want %d", partial.RowsMatched, len(page.Hits))
	}
	if fdb.tx.cancels != 1 {
		t.Errorf("cancelled statements = %d, want exactly the tail", fdb.tx.cancels)
	}
	// The fake refuses every statement in an aborted transaction except
	// ROLLBACK TO, so a RELEASE after the cancel proves the savepoint.
	labels := fdb.tx.labels(t, "entity_step", "entity_tail", "rollback_to", "release")
	tailAt := -1
	for i, l := range labels {
		if l == "entity_tail" {
			tailAt = i
		}
	}
	if tailAt < 0 || tailAt+2 >= len(labels) || labels[tailAt+1] != "rollback_to" || labels[tailAt+2] != "release" {
		t.Errorf("statements = %v, want the tail cancelled, rolled back, then released", labels)
	}
	if fdb.tx.indexOff {
		t.Error("enable_indexscan still off after the cancelled tail")
	}
	if partial.ElapsedMS < 700 || partial.ElapsedMS > 850 {
		t.Errorf("elapsed_ms = %d, want about the budget", partial.ElapsedMS)
	}
	if partial.OverrunMS != 0 {
		t.Errorf("overrun_ms = %d, want 0 without a lagging cancel", partial.OverrunMS)
	}
	if partial.BudgetMS != unscoped.DefaultBudget.Milliseconds() {
		t.Errorf("budget_ms = %d, want %d", partial.BudgetMS, unscoped.DefaultBudget.Milliseconds())
	}
	if partial.Hint != querycontract.SearchPartialHint {
		t.Errorf("hint = %q, want the scoping hint", partial.Hint)
	}
}

// A budget that completes no window reports no progress: resuming from its
// cursor repeats the identical request, so the hint urges scoping instead.
func TestEntitySearchNoProgressPartialUrgesScoping(t *testing.T) {
	t.Parallel()

	corpus := newEntityCorpus(3000, 2*time.Millisecond, func(i int) bool { return i >= 1500 })
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	// Starve the probe: 200 rows at 2 ms cost 400 ms, over a 100 ms budget.
	page, _, _, err := runEntitiesOn(t, fdb, clock, 100*time.Millisecond, 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("SearchEntities() error = %v", err)
	}
	partial := page.Partial
	if partial == nil {
		t.Fatal("Partial = nil, want a no-progress partial envelope")
	}
	if partial.Progressed {
		t.Error("progressed = true, want false: no window completed")
	}
	if partial.Hint != querycontract.SearchPartialNoProgressHint {
		t.Errorf("hint = %q, want the no-progress hint", partial.Hint)
	}
}

// Resuming from a partial cursor returns every later match exactly once: a
// resume chain gathers the ordered match list with no gap and no duplicate.
func TestEntitySearchCursorResumeHasNoGapAndNoDuplicate(t *testing.T) {
	t.Parallel()

	for _, offset := range []int{0, 20} {
		corpus := newEntityCorpus(6000, 500*time.Microsecond, func(i int) bool { return i%40 == 7 })
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
			page, _, _, err := runEntities(t, corpus, 200, remainingOffset, cursor)
			if err != nil {
				t.Fatalf("SearchEntities() error = %v", err)
			}
			gathered = append(gathered, entityKeys(page.Hits)...)
			if page.Partial == nil {
				break
			}
			cursor = page.Partial.Cursor
			remainingOffset -= page.Partial.RowsMatched
			if remainingOffset < 0 {
				remainingOffset = 0
			}
		}
		want := matchEntityKeys(corpus)[offset:]
		if !equalKeys(gathered, want) {
			t.Fatalf("offset %d: gathered %d keys, want %d", offset, len(gathered), len(want))
		}
		if calls < 2 {
			t.Fatalf("offset %d: finished in %d call, the fixture must force partial pages", offset, calls)
		}
	}
}

// Three entities share one file, so a resume cursor must name the entity, not
// the file: resuming mid-file starts strictly after the cursor entity. The
// cursor entity itself must not return (no >=) and neither must its earlier
// filemates (no file re-read).
func TestEntitySearchResumeStartsStrictlyAfterCursorEntity(t *testing.T) {
	t.Parallel()

	corpus := newEntityCorpus(9, 50*time.Microsecond, func(int) bool { return true })
	// Rows 3, 4, 5 share the second file; resume after its middle entity.
	cursor := querycontract.SearchCursor{
		RepoID:       corpus.entities[4].repo,
		RelativePath: corpus.entities[4].path,
		StartLine:    corpus.entities[4].start,
		EntityID:     corpus.entities[4].id,
	}
	page, _, _, err := runEntities(t, corpus, 10, 0, cursor)
	if err != nil {
		t.Fatalf("SearchEntities() error = %v", err)
	}
	if page.Partial != nil {
		t.Fatalf("Partial = %+v, want nil", page.Partial)
	}
	got := entityKeys(page.Hits)
	want := []string{corpus.entities[5].id, corpus.entities[6].id, corpus.entities[7].id, corpus.entities[8].id}
	if !equalKeys(got, want) {
		t.Fatalf("entities = %v, want %v", got, want)
	}
}
