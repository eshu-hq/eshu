// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestListFactsBoundsGenerationFetchStatementTimeout pins #11432: the
// projector's generation-fact fetch must scope a statement_timeout before it
// reads fact_records, mirroring the holder fan-out guard
// (supplyChainImpactFanOutReadTimeout). An unbounded multi-GB generation fetch
// that later dies at the 5-minute reducer-admission gate burns a full
// DataFileRead scan per retry and re-reads the generation from zero.
func TestListFactsBoundsGenerationFetchStatementTimeout(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{}, // per-page statement_timeout: returned row closed unscanned.
		{}, // generation fact fetch: zero rows.
		{}, // delta-union target: no row, single-generation behavior.
	}}
	store := NewFactStore(db)

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}

	timeoutAt, fetchAt := -1, -1
	for i, q := range db.queries {
		switch {
		case strings.Contains(q.query, "set_config('statement_timeout'"):
			if timeoutAt < 0 {
				timeoutAt = i
			}
		case strings.Contains(q.query, "FROM fact_records"):
			if fetchAt < 0 {
				fetchAt = i
			}
		}
	}
	if fetchAt < 0 {
		t.Fatalf("recorded %d queries, want a fact_records generation fetch among them", len(db.queries))
	}
	if timeoutAt < 0 {
		t.Fatalf("no statement_timeout guard precedes the generation fetch (%d queries recorded)", len(db.queries))
	}
	if timeoutAt > fetchAt {
		t.Fatalf("statement_timeout at query %d runs after the generation fetch at %d, want guard first", timeoutAt, fetchAt)
	}
}

// generationFetchBoundRow builds one 16-column fact_records page row with a
// cursor ordered by (observed_at, fact_id), matching deltaUnionFactRow's shape.
func generationFetchBoundRow(scopeID, generationID string, index int, base time.Time) []any {
	return []any{
		fmt.Sprintf("fact-%06d", index),
		scopeID,
		generationID,
		"file",
		fmt.Sprintf("file:repo-1:f%06d.go", index),
		"1.0.0",
		"git",
		int64(0),
		"observed",
		"git",
		"fact-key",
		"file:///repo/path",
		"record-1",
		base.Add(time.Duration(index) * time.Second),
		false,
		[]byte(fmt.Sprintf(`{"n":%d}`, index)),
	}
}

// generationFetchPageQueries returns the recorded fact_records page reads:
// the keyset-chunked generation fetch statements carrying a LIMIT.
func generationFetchPageQueries(t *testing.T, db *fakeExecQueryer) []fakeQueryCall {
	t.Helper()
	var pages []fakeQueryCall
	for _, q := range db.queries {
		if strings.Contains(q.query, "FROM fact_records") && strings.Contains(q.query, "LIMIT") {
			pages = append(pages, q)
		}
	}
	return pages
}

// TestListFactsGenerationFetchesInKeysetPages pins the #11432 (b) read shape:
// one read-only repeatable-read transaction per listFactsGeneration call, a
// per-page statement_timeout set before the first page fetch, and keyset pages
// ordered by (observed_at, fact_id) where each page resumes after the previous
// page's last row. A generation larger than one page must arrive whole.
func TestListFactsGenerationFetchesInKeysetPages(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.November, 1, 8, 0, 0, 0, time.UTC)
	var first, second [][]any
	for i := 0; i < generationFetchPageSize; i++ {
		first = append(first, generationFetchBoundRow("scope-1", "gen-1", i, base))
	}
	second = append(second, generationFetchBoundRow("scope-1", "gen-1", generationFetchPageSize, base))
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{}, // per-page statement_timeout: returned row closed unscanned.
		{rows: first},
		{rows: second},
		{rows: [][]any{{false, "", ""}}},
	}}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), generationFetchPageSize+1; got != want {
		t.Fatalf("ListFacts() len = %d, want %d (two keyset pages)", got, want)
	}
	if got := db.beginReadOnlyRepeatableReadCalls; got != 1 {
		t.Fatalf("begin read-only repeatable-read calls = %d, want 1 per generation fetch", got)
	}
	if got := db.transactionCommitCalls; got != 1 {
		t.Fatalf("transaction commits = %d, want 1", got)
	}
	if got := db.transactionRollbackCalls; got != 0 {
		t.Fatalf("transaction rollbacks = %d, want 0 on success", got)
	}

	pages := generationFetchPageQueries(t, db)
	if got, want := len(pages), 2; got != want {
		t.Fatalf("page queries = %d, want 2", got)
	}
	for _, page := range pages {
		if !strings.Contains(page.query, "ORDER BY observed_at ASC, fact_id ASC") {
			t.Fatalf("page query = %q, want ORDER BY observed_at ASC, fact_id ASC", page.query)
		}
	}
	// Split statements on purpose (#6154): the first page carries no cursor
	// predicate, the cursor pages carry the row comparison as a plain
	// predicate so it stays an index qual under a generic plan. A folded
	// `OR cursor IS NULL` shape demotes it to a filter and rescans.
	if strings.Contains(pages[0].query, "(observed_at, fact_id) > (") {
		t.Fatalf("first page query = %q, want no keyset predicate on the first page", pages[0].query)
	}
	if got, want := len(pages[0].args), 3; got != want {
		t.Fatalf("first page args = %d, want scope, generation, limit", got)
	}
	if !strings.Contains(pages[1].query, "(observed_at, fact_id) > (") {
		t.Fatalf("cursor page query = %q, want a (observed_at, fact_id) keyset predicate", pages[1].query)
	}
	timeoutAt, firstPageAt := -1, -1
	for i, q := range db.queries {
		switch {
		case strings.Contains(q.query, "set_config('statement_timeout'"):
			if timeoutAt < 0 {
				timeoutAt = i
			}
		case strings.Contains(q.query, "FROM fact_records"):
			if firstPageAt < 0 {
				firstPageAt = i
			}
		}
	}
	if timeoutAt < 0 {
		t.Fatalf("no statement_timeout guard among %d queries", len(db.queries))
	}
	if timeoutAt > firstPageAt {
		t.Fatalf("statement_timeout at query %d runs after the first page fetch at %d", timeoutAt, firstPageAt)
	}
	wantTimeout := fmt.Sprintf("%dms", generationFetchPageTimeout.Milliseconds())
	if got := fmt.Sprintf("%v", db.queries[timeoutAt].args[0]); got != wantTimeout {
		t.Fatalf("statement_timeout arg = %q, want %q", got, wantTimeout)
	}
	last := first[len(first)-1]
	if got := secondPageCursor(pages[1]); got != cursorOf(last) {
		t.Fatalf("second page cursor = %q, want the first page's last row %q", got, cursorOf(last))
	}
}

// cursorOf renders a staged row's (observed_at, fact_id) keyset cursor.
func cursorOf(row []any) string {
	return fmt.Sprintf("%v/%v", row[13], row[0])
}

// secondPageCursor renders a page query's keyset cursor arguments ($3, $4).
func secondPageCursor(page fakeQueryCall) string {
	if len(page.args) < 5 {
		return fmt.Sprintf("only %d args", len(page.args))
	}
	return fmt.Sprintf("%v/%v", page.args[2], page.args[3])
}

// TestListFactsGenerationRunsInOneReadOnlySnapshot pins that a
// single-generation fetch holds one snapshot transaction even when the whole
// generation fits on one page.
func TestListFactsGenerationRunsInOneReadOnlySnapshot(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.November, 1, 8, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{}, // per-page statement_timeout: returned row closed unscanned.
		{rows: [][]any{generationFetchBoundRow("scope-1", "gen-1", 0, base)}},
		{rows: [][]any{{false, "", ""}}},
	}}
	store := NewFactStore(db)

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got := db.beginReadOnlyRepeatableReadCalls; got != 1 {
		t.Fatalf("begin read-only repeatable-read calls = %d, want 1", got)
	}
	if got := db.transactionCommitCalls; got != 1 {
		t.Fatalf("transaction commits = %d, want 1", got)
	}
	if got := db.transactionRollbackCalls; got != 0 {
		t.Fatalf("transaction rollbacks = %d, want 0 on success", got)
	}
	if got, want := len(generationFetchPageQueries(t, db)), 1; got != want {
		t.Fatalf("page queries = %d, want a single short page", got)
	}
}

// TestListFactsGenerationFetchTimeoutClassifies57014 pins that a cancelled
// page (SQLSTATE 57014) or a context deadline fails the fetch with the typed
// fetch-timeout error, mirroring the holder fan-out classifier.
func TestListFactsGenerationFetchTimeoutClassifies57014(t *testing.T) {
	t.Parallel()

	for _, timeoutErr := range []error{
		&pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"},
		context.DeadlineExceeded,
	} {
		db := &fakeExecQueryer{queryResponses: []queueFakeRows{
			{}, // per-page statement_timeout: returned row closed unscanned.
			{err: timeoutErr},
			{rows: [][]any{{false, "", ""}}},
		}}
		store := NewFactStore(db)

		_, err := store.ListFacts(context.Background(), "scope-1", "gen-1")
		if !errors.Is(err, projector.ErrGenerationFetchTimeout) {
			t.Fatalf("ListFacts() error = %v, want %v", err, projector.ErrGenerationFetchTimeout)
		}
		if got := db.transactionRollbackCalls; got != 1 {
			t.Fatalf("transaction rollbacks = %d, want 1 on page timeout", got)
		}
		if got := db.transactionCommitCalls; got != 0 {
			t.Fatalf("transaction commits = %d, want 0 on page timeout", got)
		}
	}
}

// TestListFactsGenerationFetchTimeoutStaysRetryable pins the #11432 P1: a
// 57014/deadline page failure must stay retryable through the normal Fail
// path (failure.IsRetryable), keep its cause detectable (errors.Is/As), and
// take the retry UPDATE rather than the dead-letter triage.
func TestListFactsGenerationFetchTimeoutStaysRetryable(t *testing.T) {
	t.Parallel()

	for _, timeoutErr := range []error{
		&pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"},
		context.DeadlineExceeded,
	} {
		db := &fakeExecQueryer{queryResponses: []queueFakeRows{
			{}, // per-page statement_timeout: returned row closed unscanned.
			{err: timeoutErr},
			{rows: [][]any{{false, "", ""}}},
		}}
		store := NewFactStore(db)

		_, err := store.ListFacts(context.Background(), "scope-1", "gen-1")
		if !errors.Is(err, projector.ErrGenerationFetchTimeout) {
			t.Fatalf("ListFacts() error = %v, want %v", err, projector.ErrGenerationFetchTimeout)
		}
		if !failure.IsRetryable(err) {
			t.Fatalf("ListFacts() error = %v, want retryable through the Fail path", err)
		}
		if timeoutErr == context.DeadlineExceeded {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ListFacts() error = %v, want the deadline cause preserved", err)
			}
		} else {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
				t.Fatalf("ListFacts() error = %v, want the 57014 cause preserved", err)
			}
		}

		qdb := &recordingExecQueryer{}
		queue := NewProjectorQueue(qdb, "projector-1", 30*time.Second)
		queue.RetryDelay = 2 * time.Minute
		queue.MaxAttempts = 3
		work := projector.ScopeGenerationWork{
			Scope:        scope.IngestionScope{ScopeID: "scope-1"},
			Generation:   scope.ScopeGeneration{GenerationID: "gen-1"},
			AttemptCount: 1,
		}
		if ferr := queue.Fail(context.Background(), work, err); ferr != nil {
			t.Fatalf("Fail() error = %v, want nil", ferr)
		}
		if got, want := len(qdb.execs), 1; got != want {
			t.Fatalf("Fail() exec count = %d, want %d (single retry UPDATE, no dead-letter)", got, want)
		}
		query := qdb.execs[0].query
		for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'"} {
			if !strings.Contains(query, want) {
				t.Fatalf("Fail() retry query missing %q:\n%s", want, query)
			}
		}
		// No FailureClass method on the timeout error: the retry path keeps
		// the fallback class, pinning the no-typed-class contract.
		if got, want := qdb.execs[0].args[1], "projection_retryable"; got != want {
			t.Fatalf("failure class = %v, want %v", got, want)
		}
	}
}

// TestListFactsGenerationPartialPageIsNotSuccess pins the ruling's closed
// world: a page that dies partway is a failed fetch, never a short success
// with the rows read so far.
func TestListFactsGenerationPartialPageIsNotSuccess(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.November, 1, 8, 0, 0, 0, time.UTC)
	var first [][]any
	for i := 0; i < generationFetchPageSize; i++ {
		first = append(first, generationFetchBoundRow("scope-1", "gen-1", i, base))
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{}, // per-page statement_timeout: returned row closed unscanned.
		{rows: first},
		{err: &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}},
		{rows: [][]any{{false, "", ""}}},
	}}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-1")
	if !errors.Is(err, projector.ErrGenerationFetchTimeout) {
		t.Fatalf("ListFacts() error = %v, want %v", err, projector.ErrGenerationFetchTimeout)
	}
	if loaded != nil {
		t.Fatalf("ListFacts() returned %d rows with a failed page, want no partial success", len(loaded))
	}
}
