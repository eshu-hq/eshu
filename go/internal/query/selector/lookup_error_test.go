// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestResolveExactForAccessWrapsBackendFailureInLookupError proves every
// backing-read failure (the catalog read and both graph reads) arrives as a
// LookupError that still unwraps to the backend sentinel, so
// querycontract.WriteGraphReadError keeps mapping fence and graph-availability
// verdicts first, and that its text never carries the raw selector.
func TestResolveExactForAccessWrapsBackendFailureInLookupError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		graph    querycontract.GraphQuery
		content  querycontract.ContentStore
		sentinel error
	}{
		{"catalog read", nil, failingCatalog{err: fmt.Errorf("read store: %w", db.ErrReaderUnavailable)}, db.ErrReaderUnavailable},
		{"catalog read stale", nil, failingCatalog{err: fmt.Errorf("read store: %w", db.ErrReaderStale)}, db.ErrReaderStale},
		{"ordered graph read", graphRunFailing(fmt.Errorf("graph: %w", querycontract.ErrGraphUnavailable)), nil, querycontract.ErrGraphUnavailable},
		{"fallback graph read", graphFallbackFailing(fmt.Errorf("graph: %w", querycontract.ErrGraphReadDeadline)), nil, querycontract.ErrGraphReadDeadline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ResolveExactForAccess(context.Background(), tc.graph, tc.content, requestTestSelector,
				querycontract.RepositoryAccessFilter{AllScopes: true})
			if !IsLookupFailure(err) {
				t.Fatalf("IsLookupFailure(%v) = false, want true", err)
			}
			var lookup LookupError
			if !errors.As(err, &lookup) || lookup.Err == nil {
				t.Fatalf("errors.As(%v, *LookupError) = false or empty Err", err)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false, want the backend sentinel reachable", err, tc.sentinel)
			}
			if IsNotFound(err) {
				t.Fatalf("IsNotFound(%v) = true for a backend failure", err)
			}
			if !strings.HasPrefix(err.Error(), LookupFailureMessage+": ") {
				t.Fatalf("Error() = %q, want the %q prefix", err.Error(), LookupFailureMessage)
			}
			if strings.Contains(err.Error(), requestTestSelector) {
				t.Fatalf("Error() = %q carries the raw selector", err.Error())
			}
		})
	}
}

// TestSelectorAnswersAreNotLookupFailures proves a selector that resolves to
// nothing, to several repositories, or is refused by an empty grant is a
// selector answer, never a lookup failure, so callers keep 404 and 400 for
// them.
func TestSelectorAnswersAreNotLookupFailures(t *testing.T) {
	t.Parallel()

	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: requestTestSelector},
		{ID: "repository:r_b", Name: requestTestSelector},
	}}
	cases := []struct {
		name    string
		graph   querycontract.GraphQuery
		content querycontract.ContentStore
		access  querycontract.RepositoryAccessFilter
	}{
		{"no match", graphRunFailing(nil), nil, querycontract.RepositoryAccessFilter{AllScopes: true}},
		{"ambiguous catalog match", nil, ambiguous, querycontract.RepositoryAccessFilter{AllScopes: true}},
		{"empty grant refused before the graph", graphRunFailing(errors.New("graph must not run")), nil, querycontract.RepositoryAccessFilter{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ResolveExactForAccess(context.Background(), tc.graph, tc.content, requestTestSelector, tc.access)
			if err == nil {
				t.Fatal("ResolveExactForAccess() error = nil, want a selector answer")
			}
			if IsLookupFailure(err) {
				t.Fatalf("IsLookupFailure(%v) = true for a selector answer", err)
			}
		})
	}
	if IsLookupFailure(nil) {
		t.Fatal("IsLookupFailure(nil) = true")
	}
	if IsLookupFailure(AmbiguousError{Selector: requestTestSelector, Matches: []string{"a", "b"}}) {
		t.Fatal("IsLookupFailure(AmbiguousError) = true")
	}
	if IsLookupFailure(NotFoundError{Selector: requestTestSelector}) {
		t.Fatal("IsLookupFailure(NotFoundError) = true")
	}
}

// TestLookupErrorZeroValueDoesNotPanic proves a LookupError with no backend
// error still renders the fixed message, since IsLookupFailure matches it and
// a caller may log or write it.
func TestLookupErrorZeroValueDoesNotPanic(t *testing.T) {
	t.Parallel()

	var zero LookupError
	if got := zero.Error(); got != LookupFailureMessage {
		t.Fatalf("LookupError{}.Error() = %q, want the bare %q", got, LookupFailureMessage)
	}
	if !IsLookupFailure(zero) {
		t.Fatal("IsLookupFailure(LookupError{}) = false")
	}
}
