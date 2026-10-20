// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stringsRows yields one text column per row.
type stringsRows struct {
	values []string
	pos    int
}

func (r *stringsRows) Next() bool {
	if r.pos >= len(r.values) {
		return false
	}
	r.pos++
	return true
}

func (r *stringsRows) Scan(dest ...any) error {
	ptr, ok := dest[0].(*string)
	if !ok {
		return errors.New("stringsRows scans only into *string")
	}
	*ptr = r.values[r.pos-1]
	return nil
}

func (r *stringsRows) Err() error { return nil }

func (r *stringsRows) Close() error { return nil }

// stringsQueryer answers every query with the canned ids and records the
// call.
type stringsQueryer struct {
	values []string
	calls  int
}

func (q *stringsQueryer) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	q.calls++
	return &stringsRows{values: q.values}, nil
}

// TestSkippedReposPassesThrough pins the walk filter's plumbing: it returns
// the skipped ids the statement reports, asks nothing when there is nothing
// to check, and reports a query failure. The predicate itself (failure count
// and extractor version) is proved live, where the real planner runs it.
func TestSkippedReposPassesThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	req := SkippedRequest{ExtractorVersion: 1, SkipAfterFailures: 5}

	skipped, err := SkippedRepos(ctx, &stringsQueryer{values: []string{"repo-a", "repo-b"}}, req,
		[]string{"repo-a", "repo-b", "repo-c"})
	if err != nil {
		t.Fatalf("SkippedRepos() error = %v", err)
	}
	if !reflect.DeepEqual(skipped, []string{"repo-a", "repo-b"}) {
		t.Fatalf("SkippedRepos() = %v, want [repo-a repo-b]", skipped)
	}

	queryer := &stringsQueryer{}
	skipped, err = SkippedRepos(ctx, queryer, req, nil)
	if err != nil {
		t.Fatalf("SkippedRepos(nil ids) error = %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("SkippedRepos(nil ids) = %v, want empty", skipped)
	}
	if queryer.calls != 0 {
		t.Fatalf("SkippedRepos(nil ids) issued %d statements, want none", queryer.calls)
	}

	boom := errors.New("connection refused")
	if _, err := SkippedRepos(ctx, errQueryer{err: boom}, req, []string{"repo-a"}); !errors.Is(err, boom) {
		t.Fatalf("SkippedRepos(query error) = %v, want the query error", err)
	}
	if _, err := SkippedRepos(ctx, errQueryer{err: &pgconn.PgError{Code: "42P01"}}, req,
		[]string{"repo-a"}); err == nil {
		t.Fatal("SkippedRepos(missing table) = nil, want an error: unlike the marker check, a missing table here is a broken install, not a state to report")
	}
}
