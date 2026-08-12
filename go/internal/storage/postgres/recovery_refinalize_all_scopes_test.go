// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestRecoveryStoreRefinalizeScopeProjectionsAllScopes checks the SQL the
// disaster-recovery rebuild actually sends. The scope predicate has to come out
// of the statement, not be filled with an empty array: `scope_id = ANY('{}')`
// matches nothing, so an all-scopes rebuild would report zero enqueued and look
// like a clean no-op while the graph stayed empty.
func TestRecoveryStoreRefinalizeScopeProjectionsAllScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope-1"}, {"scope-2"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		AllScopes: true,
	}, now)
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections(AllScopes) error = %v, want nil", err)
	}
	if got, want := result.Enqueued, 2; got != want {
		t.Fatalf("result.Enqueued = %d, want %d", got, want)
	}

	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	query := db.queries[0].query
	if strings.Contains(query, "scope.scope_id = ANY(") {
		t.Fatalf("all-scopes refinalize kept the scope predicate, which matches nothing on an empty array: %s", query)
	}
	if !strings.Contains(query, "scope.active_generation_id IS NOT NULL") ||
		!strings.Contains(query, "scope.status = 'active'") {
		t.Fatalf("all-scopes refinalize dropped the active-scope guards: %s", query)
	}
	if got, want := len(db.queries[0].args), 1; got != want {
		t.Fatalf("all-scopes refinalize arg count = %d, want %d (timestamp only)", got, want)
	}
}

// TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped is
// the other half of the pair: the explicit-scope path must keep its predicate,
// so a caller naming three scopes never re-projects the whole deployment.
func TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope-1"}}},
		},
	}

	store := NewRecoveryStore(db)
	if _, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1"},
	}, time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "scope.scope_id = ANY(") {
		t.Fatalf("scoped refinalize lost its scope predicate and would re-project every scope: %s", query)
	}
	if got, want := len(db.queries[0].args), 2; got != want {
		t.Fatalf("scoped refinalize arg count = %d, want %d (timestamp + scope ids)", got, want)
	}
}
