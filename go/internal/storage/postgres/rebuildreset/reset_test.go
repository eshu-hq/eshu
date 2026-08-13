// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rebuildreset

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestScopePredicateAllScopesDropsTheClauseEntirely pins the #4594 invariant that
// costs the most if it breaks silently: the disaster-recovery path must drop the
// scope clause, never render it against an empty array.
//
// `scope_id = ANY('{}')` is valid SQL that matches no rows. A rebuild that
// rendered it would delete nothing, reopen nothing, re-enqueue nothing, and
// report success -- an operator would watch a clean run finish and be left with
// an empty graph. Asserting "no args" is the part that matters: a predicate with
// an empty slice argument is exactly the failure mode.
func TestScopePredicateAllScopesDropsTheClauseEntirely(t *testing.T) {
	t.Parallel()

	predicate, args := ScopePredicate(recovery.RefinalizeFilter{AllScopes: true}, 1)

	if predicate != "" {
		t.Fatalf("ScopePredicate(AllScopes) predicate = %q, want empty; "+
			"an all-scopes rebuild must drop the clause, because scope_id = ANY('{}') "+
			"matches no rows and would report success over an empty graph", predicate)
	}
	if len(args) != 0 {
		t.Fatalf("ScopePredicate(AllScopes) args = %v, want none; "+
			"passing an empty array is the silent no-op this guard exists to prevent", args)
	}
}

// TestScopePredicateExplicitScopesBindsRequestedPlaceholder proves the predicate
// tracks the caller's placeholder number. The projector re-enqueue carries a
// leading timestamp at $1 so its predicate starts at $2, while the three reset
// statements take no timestamp and start at $1. A hard-coded $1 would bind the
// timestamp as the scope array on the enqueue path.
func TestScopePredicateExplicitScopesBindsRequestedPlaceholder(t *testing.T) {
	t.Parallel()

	scopes := []string{"scope-a", "scope-b"}

	for _, tc := range []struct {
		name        string
		placeholder int
		want        string
	}{
		{"reset statements start at 1", 1, "AND scope.scope_id = ANY($1)"},
		{"projector insert starts at 2", 2, "AND scope.scope_id = ANY($2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			predicate, args := ScopePredicate(recovery.RefinalizeFilter{ScopeIDs: scopes}, tc.placeholder)

			if predicate != tc.want {
				t.Fatalf("ScopePredicate(..., %d) = %q, want %q", tc.placeholder, predicate, tc.want)
			}
			if len(args) != 1 {
				t.Fatalf("ScopePredicate(..., %d) args = %v, want exactly the scope slice", tc.placeholder, args)
			}
			got, ok := args[0].([]string)
			if !ok {
				t.Fatalf("ScopePredicate arg type = %T, want []string", args[0])
			}
			if strings.Join(got, ",") != strings.Join(scopes, ",") {
				t.Fatalf("ScopePredicate arg = %v, want %v", got, scopes)
			}
		})
	}
}

// TestResetQueriesShareTheAffectedGenerationsSubquery is the cross-statement
// agreement proof. All three resets must select the same (scope_id,
// generation_id) set; if one drifted, a rebuild could delete a domain's work
// without reopening the intents that rebuild it, and the graph would come back
// short in a way no single statement's test would catch.
func TestResetQueriesShareTheAffectedGenerationsSubquery(t *testing.T) {
	t.Parallel()

	filter := recovery.RefinalizeFilter{ScopeIDs: []string{"scope-a"}}

	for name, template := range map[string]string{
		"delete succeeded reducer work":    deleteSucceededReducerWorkTemplate,
		"reopen shared projection intents": reopenSharedIntentsTemplate,
		"clear readiness phase state":      clearReadinessPhaseStateTemplate,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			query, args := buildResetQuery(template, filter)

			for _, want := range []string{
				"scope.active_generation_id IS NOT NULL",
				"scope.status = 'active'",
				"AND scope.scope_id = ANY($1)",
			} {
				if !strings.Contains(query, want) {
					t.Fatalf("%s query is missing %q; the three resets must select the same "+
						"affected generations or a rebuild half-applies\n%s", name, want, query)
				}
			}
			if len(args) != 1 {
				t.Fatalf("%s args = %v, want exactly the scope slice", name, args)
			}
			if strings.Contains(query, "%!s(MISSING)") || strings.Contains(query, "%s") {
				t.Fatalf("%s query has an unrendered format verb:\n%s", name, query)
			}
		})
	}
}

// TestResetQueriesTouchOnlyTerminalState guards the concurrency contract. The
// reducer delete must stay scoped to 'succeeded': claimed and running rows hold
// live leases, and deleting one lets a second worker claim the same conflict key
// while the first is still executing. The shared-intent reopen must stay an
// UPDATE, because the payload is the drain's input and deleting it would lose
// the edges outright.
func TestResetQueriesTouchOnlyTerminalState(t *testing.T) {
	t.Parallel()

	filter := recovery.RefinalizeFilter{AllScopes: true}

	reducerDelete, _ := buildResetQuery(deleteSucceededReducerWorkTemplate, filter)
	if !strings.Contains(reducerDelete, "status = 'succeeded'") {
		t.Fatalf("reducer reset is not scoped to succeeded rows; claimed and running rows "+
			"hold live leases a rebuild must not yank\n%s", reducerDelete)
	}
	for _, forbidden := range []string{"'claimed'", "'running'", "'dead_letter'", "'failed'"} {
		if strings.Contains(reducerDelete, forbidden) {
			t.Fatalf("reducer reset references %s; it must touch succeeded rows only\n%s",
				forbidden, reducerDelete)
		}
	}

	sharedReopen, _ := buildResetQuery(reopenSharedIntentsTemplate, filter)
	if !strings.HasPrefix(strings.TrimSpace(sharedReopen), "UPDATE shared_projection_intents") {
		t.Fatalf("shared intents must be reopened with UPDATE, not deleted; the payload is "+
			"the drain's input\n%s", sharedReopen)
	}
	if !strings.Contains(sharedReopen, "completed_at IS NOT NULL") {
		t.Fatalf("shared intent reopen dropped its completed_at IS NOT NULL guard, so its "+
			"reported count would mean 'matched' rather than 'reopened'\n%s", sharedReopen)
	}
}
