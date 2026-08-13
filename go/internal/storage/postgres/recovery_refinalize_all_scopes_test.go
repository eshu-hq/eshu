// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// refinalizeFakeDB returns a fake transaction-capable database primed with the
// two result sets a refinalize reads: the materialized (scope_id,
// generation_id) set, then the scope ids the projector re-enqueue returns.
func refinalizeFakeDB(pairs [][]any, enqueued [][]any) *fakeBeginnerExecQueryer {
	return &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: pairs},
				{rows: enqueued},
			},
		},
	}
}

// TestRecoveryStoreRefinalizeScopeProjectionsAllScopes checks the SQL the
// disaster-recovery rebuild actually sends. The scope predicate has to come out
// of the statement, not be filled with an empty array: `scope_id = ANY('{}')`
// matches nothing, so an all-scopes rebuild would report zero enqueued and look
// like a clean no-op while the graph stayed empty.
func TestRecoveryStoreRefinalizeScopeProjectionsAllScopes(t *testing.T) {
	t.Parallel()

	db := refinalizeFakeDB(
		[][]any{{"scope-1", "gen-1"}, {"scope-2", "gen-2"}},
		[][]any{{"scope-1"}, {"scope-2"}},
	)

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

	if len(db.queries) != 2 {
		t.Fatalf("query count = %d, want 2 (materialize the generations, then enqueue)", len(db.queries))
	}
	selectQuery := db.queries[0].query
	if strings.Contains(selectQuery, "scope.scope_id = ANY(") {
		t.Fatalf("all-scopes refinalize kept the scope predicate, which matches nothing on an empty array: %s", selectQuery)
	}
	if !strings.Contains(selectQuery, "scope.active_generation_id IS NOT NULL") ||
		!strings.Contains(selectQuery, "scope.status = 'active'") {
		t.Fatalf("all-scopes refinalize dropped the active-scope guards: %s", selectQuery)
	}
	if got, want := len(db.queries[0].args), 0; got != want {
		t.Fatalf("all-scopes generation read arg count = %d, want %d", got, want)
	}

	assertRefinalizeBindsOneGenerationSet(t, db, []string{"scope-1", "scope-2"}, []string{"gen-1", "gen-2"})
	if !db.committed {
		t.Fatal("refinalize did not commit its transaction")
	}
}

// TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped is
// the other half of the pair: the explicit-scope path must keep its predicate,
// so a caller naming three scopes never re-projects the whole deployment.
func TestRecoveryStoreRefinalizeScopeProjectionsKeepsScopePredicateWhenScoped(t *testing.T) {
	t.Parallel()

	db := refinalizeFakeDB([][]any{{"scope-1", "gen-1"}}, [][]any{{"scope-1"}})

	store := NewRecoveryStore(db)
	if _, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1"},
	}, time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	selectQuery := db.queries[0].query
	if !strings.Contains(selectQuery, "scope.scope_id = ANY($1)") {
		t.Fatalf("scoped refinalize lost its scope predicate and would re-project every scope: %s", selectQuery)
	}
	if got, want := len(db.queries[0].args), 1; got != want {
		t.Fatalf("scoped generation read arg count = %d, want %d (scope ids only)", got, want)
	}

	assertRefinalizeBindsOneGenerationSet(t, db, []string{"scope-1"}, []string{"gen-1"})
}

// TestRefinalizeReadsIngestionScopesExactlyOnce is the #4594 review finding: a
// refinalize used to re-derive its (scope_id, generation_id) set inside every
// statement. Postgres runs the transaction at READ COMMITTED, so each of those
// four subqueries got its own snapshot. An ingester activating a new generation
// mid-refinalize could therefore have the enqueue re-project G1 while a later
// reset deleted G2's dedup state -- G1 left deduplicated and never rebuilt, G2
// damaged and never replayed.
//
// The fix is one read at the top of the transaction whose rows every later
// statement binds. This asserts the shape that makes that true, because the
// interleaving itself only shows up against a live database under a concurrent
// activation (TestRefinalizeRebuildResetBindsTheGenerationSetItRead).
func TestRefinalizeReadsIngestionScopesExactlyOnce(t *testing.T) {
	t.Parallel()

	db := refinalizeFakeDB(
		[][]any{{"scope-1", "gen-1"}, {"scope-2", "gen-2"}},
		[][]any{{"scope-1"}, {"scope-2"}},
	)

	store := NewRecoveryStore(db)
	if _, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		AllScopes: true,
	}, time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	var readers []string
	for _, statement := range refinalizeStatements(db) {
		if strings.Contains(statement, "ingestion_scopes") {
			readers = append(readers, statement)
		}
	}
	if len(readers) != 1 {
		t.Fatalf("%d of the refinalize's statements read ingestion_scopes, want exactly 1; "+
			"every extra read gets its own READ COMMITTED snapshot and can select a generation "+
			"the other statements never saw:\n%s", len(readers), strings.Join(readers, "\n---\n"))
	}
	if !strings.HasPrefix(strings.TrimSpace(refinalizeStatements(db)[0]), "SELECT") {
		t.Fatalf("the ingestion_scopes read is not the first statement in the transaction, so the "+
			"statements before it bind a different snapshot:\n%s", refinalizeStatements(db)[0])
	}
}

// refinalizeStatements returns every statement a refinalize issued, in order:
// the two queries first, then the three reset execs.
func refinalizeStatements(db *fakeBeginnerExecQueryer) []string {
	statements := make([]string, 0, len(db.queries)+len(db.execs))
	for _, query := range db.queries {
		statements = append(statements, query.query)
	}
	for _, exec := range db.execs {
		statements = append(statements, exec.query)
	}
	return statements
}

// assertRefinalizeBindsOneGenerationSet checks that the projector re-enqueue and
// all three rebuild-reset statements bind exactly the generation set the first
// statement read.
//
// This replaces an earlier "do all four render the same predicate" assertion.
// Identical predicates were never enough: four statements can render the same
// SQL and still select four different row sets, one per snapshot. Identical
// bound arrays cannot.
func assertRefinalizeBindsOneGenerationSet(
	t *testing.T,
	db *fakeBeginnerExecQueryer,
	wantScopeIDs, wantGenerationIDs []string,
) {
	t.Helper()

	if len(db.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(db.queries))
	}
	enqueue := db.queries[1]
	if strings.Contains(enqueue.query, "ingestion_scopes") {
		t.Fatalf("the projector re-enqueue still reads ingestion_scopes instead of binding the "+
			"generation set already read: %s", enqueue.query)
	}
	if len(enqueue.args) != 3 {
		t.Fatalf("projector re-enqueue arg count = %d, want 3 (timestamp, scope ids, generation ids)", len(enqueue.args))
	}
	assertStringSliceArg(t, "projector re-enqueue scope ids", enqueue.args[1], wantScopeIDs)
	assertStringSliceArg(t, "projector re-enqueue generation ids", enqueue.args[2], wantGenerationIDs)

	if len(db.execs) != 3 {
		t.Fatalf("rebuild-reset statement count = %d, want 3", len(db.execs))
	}
	wantTargets := []string{
		"DELETE FROM fact_work_items",
		"UPDATE shared_projection_intents",
		"DELETE FROM graph_projection_phase_state",
	}
	for i, want := range wantTargets {
		if !strings.Contains(db.execs[i].query, want) {
			t.Fatalf("rebuild-reset statement %d does not target %q: %s", i, want, db.execs[i].query)
		}
		if len(db.execs[i].args) != 2 {
			t.Fatalf("rebuild-reset statement %d arg count = %d, want 2 (scope ids, generation ids)",
				i, len(db.execs[i].args))
		}
		assertStringSliceArg(t, fmt.Sprintf("rebuild-reset statement %d scope ids", i), db.execs[i].args[0], wantScopeIDs)
		assertStringSliceArg(t, fmt.Sprintf("rebuild-reset statement %d generation ids", i), db.execs[i].args[1], wantGenerationIDs)
	}
}

// assertStringSliceArg fails unless a bound statement argument is exactly the
// expected string slice.
func assertStringSliceArg(t *testing.T, what string, arg any, want []string) {
	t.Helper()

	got, ok := arg.([]string)
	if !ok {
		t.Fatalf("%s type = %T, want []string", what, arg)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s = %v, want %v; every statement in a refinalize must bind the same generation set", what, got, want)
	}
}
