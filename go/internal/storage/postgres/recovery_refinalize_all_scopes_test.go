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

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{"scope-1"}, {"scope-2"}}},
			},
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

	// The three rebuild-reset statements ride the same transaction and must carry
	// the same all-scopes shape. A reset that kept an empty-array predicate would
	// match nothing and quietly leave the rebuild short.
	assertRefinalizeResetStatements(t, db.execs, "")
	if !db.committed {
		t.Fatal("refinalize did not commit its transaction")
	}
}

// assertRefinalizeResetStatements checks that a refinalize issued exactly the
// three rebuild-reset statements, each carrying the given scope predicate. It
// exists so both the all-scopes and the scoped test assert the same contract
// against the same code, rather than each spelling out its own near-copy.
func assertRefinalizeResetStatements(t *testing.T, execs []fakeExecCall, wantPredicate string) {
	t.Helper()

	if len(execs) != 3 {
		t.Fatalf("rebuild-reset statement count = %d, want 3", len(execs))
	}
	wantTargets := []string{
		"DELETE FROM fact_work_items",
		"UPDATE shared_projection_intents",
		"DELETE FROM graph_projection_phase_state",
	}
	for i, want := range wantTargets {
		if !strings.Contains(execs[i].query, want) {
			t.Fatalf("rebuild-reset statement %d does not target %q: %s", i, want, execs[i].query)
		}
		if !strings.Contains(execs[i].query, "FROM ingestion_scopes AS scope") {
			t.Fatalf("rebuild-reset statement %d lost the shared affected-generation subquery: %s", i, execs[i].query)
		}
		if wantPredicate == "" {
			if strings.Contains(execs[i].query, "scope.scope_id = ANY(") {
				t.Fatalf("all-scopes rebuild-reset statement %d kept a scope predicate: %s", i, execs[i].query)
			}
			continue
		}
		if !strings.Contains(execs[i].query, wantPredicate) {
			t.Fatalf("rebuild-reset statement %d lost its scope predicate %q: %s", i, wantPredicate, execs[i].query)
		}
	}
}

// TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped is
// the other half of the pair: the explicit-scope path must keep its predicate,
// so a caller naming three scopes never re-projects the whole deployment.
func TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{"scope-1"}}},
			},
		},
	}

	store := NewRecoveryStore(db)
	if _, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1"},
	}, time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "scope.scope_id = ANY($2)") {
		t.Fatalf("scoped refinalize lost its scope predicate and would re-project every scope: %s", query)
	}
	if got, want := len(db.queries[0].args), 2; got != want {
		t.Fatalf("scoped refinalize arg count = %d, want %d (timestamp + scope ids)", got, want)
	}

	// The resets take no timestamp, so their predicate is $1 rather than $2. That
	// difference is the one place these four statements are allowed to diverge,
	// and getting it wrong would either error or silently select the wrong scopes.
	assertRefinalizeResetStatements(t, db.execs, "scope.scope_id = ANY($1)")
	for i, exec := range db.execs {
		if got, want := len(exec.args), 1; got != want {
			t.Fatalf("rebuild-reset statement %d arg count = %d, want %d (scope ids only)", i, got, want)
		}
	}
}
