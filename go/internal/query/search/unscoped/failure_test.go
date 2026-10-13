// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errInjected is the error a failing store returns.
var errInjected = errors.New("injected failure")

// failingStore fails the transaction begin, or the first statement whose text
// contains failOn, or the row scan of a statement whose text contains
// scanFailOn, with errInjected.
type failingStore struct {
	inner       *fakeDB
	failOnBegin bool
	failOn      string
	scanFailOn  string
}

func (s *failingStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	if s.failOnBegin {
		return nil, errInjected
	}
	tx, err := s.inner.BeginReadOnlySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{ReadTransaction: tx, failOn: s.failOn, scanFailOn: s.scanFailOn}, nil
}

type failingTx struct {
	db.ReadTransaction
	failOn     string
	scanFailOn string
}

func (tx *failingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if tx.failOn != "" && strings.Contains(query, tx.failOn) {
		return nil, errInjected
	}
	rows, err := tx.ReadTransaction.QueryContext(ctx, query, args...)
	if err == nil && tx.scanFailOn != "" && strings.Contains(query, tx.scanFailOn) {
		return failingRows{Rows: rows}, nil
	}
	return rows, err
}

// failingRows fails every row scan with errInjected.
type failingRows struct {
	db.Rows
}

func (failingRows) Scan(...any) error { return errInjected }

// The file route's error texts are part of what callers log and match, so the
// engine refactor keeps each one exactly, with the cause still unwrappable.
func TestFileRouteErrorTexts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		store *failingStore
		want  string
	}{
		{
			name: "begin", store: &failingStore{failOnBegin: true},
			want: "begin unscoped search snapshot: injected failure",
		},
		{
			name: "readiness", store: &failingStore{failOn: "eshu_require_content_substring_indexes_ready"},
			want: "check content substring index readiness: injected failure",
		},
		{
			name: "step", store: &failingStore{failOn: "UNION ALL"},
			want: "search unscoped file content: injected failure",
		},
		{
			name: "scan", store: &failingStore{scanFailOn: "UNION ALL"},
			want: "search unscoped file content: scan unscoped search row: injected failure",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := newFakeClock()
			tc.store.inner = newFakeDB(newCorpus(1000, ms, func(int) bool { return false }), clock)
			searcher := &unscoped.Searcher{Store: tc.store, Now: clock.Now, Logger: quietLogger}
			_, err := searcher.Search(context.Background(), "needle", 10, 0, querycontract.SearchCursor{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Search() error = %v, want %q", err, tc.want)
			}
			if !errors.Is(err, errInjected) {
				t.Fatalf("Search() error %v does not wrap the cause", err)
			}
		})
	}
}

// The entity route's error texts are pinned the same way, so the entity shape
// keeps each one exactly.
func TestEntityRouteErrorTexts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		store *failingStore
		want  string
	}{
		{
			name: "begin", store: &failingStore{failOnBegin: true},
			want: "begin unscoped entity search snapshot: injected failure",
		},
		{
			name: "readiness", store: &failingStore{failOn: "eshu_require_content_substring_indexes_ready"},
			want: "check content substring index readiness: injected failure",
		},
		{
			name: "step", store: &failingStore{failOn: "UNION ALL"},
			want: "search unscoped entity content: injected failure",
		},
		{
			name: "scan", store: &failingStore{scanFailOn: "UNION ALL"},
			want: "search unscoped entity content: scan unscoped entity search row: injected failure",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := newFakeClock()
			tc.store.inner = newFakeDB(newEntityCorpus(1000, ms, func(i int) bool { return i == 3 }), clock)
			searcher := &unscoped.Searcher{Store: tc.store, Now: clock.Now, Logger: quietLogger}
			_, err := searcher.SearchEntities(context.Background(), "needle", 10, 0, querycontract.SearchCursor{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("SearchEntities() error = %v, want %q", err, tc.want)
			}
			if !errors.Is(err, errInjected) {
				t.Fatalf("SearchEntities() error %v does not wrap the cause", err)
			}
		})
	}
}
