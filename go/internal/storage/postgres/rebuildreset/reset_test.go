// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rebuildreset

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// TestAffectedGenerationsQueryAllScopesDropsTheClauseEntirely pins the #4594
// invariant that costs the most if it breaks silently: the disaster-recovery
// path must drop the scope clause, never render it against an empty array.
//
// `scope_id = ANY('{}')` is valid SQL that matches no rows. A rebuild that
// rendered it would read no generations, delete nothing, reopen nothing,
// re-enqueue nothing, and report success -- an operator would watch a clean run
// finish and be left with an empty graph. Asserting "no args" is the part that
// matters: a predicate with an empty slice argument is exactly the failure mode.
func TestAffectedGenerationsQueryAllScopesDropsTheClauseEntirely(t *testing.T) {
	t.Parallel()

	query, args := AffectedGenerationsQuery(recovery.RefinalizeFilter{AllScopes: true})

	if strings.Contains(query, "scope.scope_id = ANY(") {
		t.Fatalf("AffectedGenerationsQuery(AllScopes) kept the scope clause; an all-scopes rebuild "+
			"must drop it, because scope_id = ANY('{}') matches no rows and would report success "+
			"over an empty graph:\n%s", query)
	}
	if len(args) != 0 {
		t.Fatalf("AffectedGenerationsQuery(AllScopes) args = %v, want none; "+
			"passing an empty array is the silent no-op this guard exists to prevent", args)
	}
	for _, want := range []string{
		"scope.active_generation_id IS NOT NULL",
		"scope.status = 'active'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("AffectedGenerationsQuery(AllScopes) dropped %q, so it would re-drive retired "+
				"or generation-less scopes:\n%s", want, query)
		}
	}
}

// TestAffectedGenerationsQueryExplicitScopesBindsTheScopeList proves the other
// half: a caller naming scopes must get a filtered read, or a scoped recovery
// would re-project the whole deployment.
func TestAffectedGenerationsQueryExplicitScopesBindsTheScopeList(t *testing.T) {
	t.Parallel()

	scopes := []string{"scope-a", "scope-b"}

	query, args := AffectedGenerationsQuery(recovery.RefinalizeFilter{ScopeIDs: scopes})

	if !strings.Contains(query, "AND scope.scope_id = ANY($1)") {
		t.Fatalf("AffectedGenerationsQuery lost its scope predicate:\n%s", query)
	}
	if len(args) != 1 {
		t.Fatalf("AffectedGenerationsQuery args = %v, want exactly the scope slice", args)
	}
	got, ok := args[0].([]string)
	if !ok {
		t.Fatalf("AffectedGenerationsQuery arg type = %T, want []string", args[0])
	}
	if strings.Join(got, ",") != strings.Join(scopes, ",") {
		t.Fatalf("AffectedGenerationsQuery arg = %v, want %v", got, scopes)
	}
	if strings.Contains(query, "%!s(MISSING)") || strings.Contains(query, "%s") {
		t.Fatalf("AffectedGenerationsQuery has an unrendered format verb:\n%s", query)
	}
}

// TestResetQueriesBindTheSameGenerationSet is the cross-statement agreement
// proof. All three resets must act on exactly the generations the caller read,
// and on the same ones as each other; if one drifted, a rebuild could delete a
// domain's work without reopening the intents that rebuild it, and the graph
// would come back short in a way no single statement's test would catch.
//
// They agree by construction now: none of them selects a row set, they all bind
// the arrays the caller passes. That is what this asserts -- an embedded
// re-selection would reintroduce the per-statement READ COMMITTED snapshot the
// arrays exist to eliminate.
func TestResetQueriesBindTheSameGenerationSet(t *testing.T) {
	t.Parallel()

	generations := Generations{}
	generations.Append("scope-a", "gen-a")
	generations.Append("scope-b", "gen-b")

	for name, template := range map[string]string{
		"delete succeeded reducer work":    deleteSucceededReducerWorkTemplate,
		"reopen shared projection intents": reopenSharedIntentsTemplate,
		"clear readiness phase state":      clearReadinessPhaseStateTemplate,
		"retire resolution generations":    retireResolutionGenerationsTemplate,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			query, args := buildResetQuery(template, generations)

			if strings.Contains(query, "ingestion_scopes") {
				t.Fatalf("%s re-reads ingestion_scopes; under READ COMMITTED that is a fresh "+
					"snapshot and can select a generation the enqueue never saw\n%s", name, query)
			}
			if !strings.Contains(query, "unnest($1::text[], $2::text[])") {
				t.Fatalf("%s does not bind the materialized generation arrays\n%s", name, query)
			}
			// relationship_generations names its scope column `scope`, every other
			// reset target names it `scope_id`: both spellings carry the same
			// pairing, so both are accepted.
			if !strings.Contains(query, "(scope_id, generation_id) IN (") &&
				!strings.Contains(query, "(scope, generation_id) IN (") {
				t.Fatalf("%s lost its (scope, generation_id) pairing, so it could match a "+
					"generation belonging to another scope\n%s", name, query)
			}
			if strings.Contains(query, "%!s(MISSING)") || strings.Contains(query, "%s") {
				t.Fatalf("%s query has an unrendered format verb:\n%s", name, query)
			}

			if len(args) != 2 {
				t.Fatalf("%s args = %v, want the two aligned arrays", name, args)
			}
			assertStringSlice(t, name+" scope ids", args[0], generations.ScopeIDs)
			assertStringSlice(t, name+" generation ids", args[1], generations.GenerationIDs)
		})
	}
}

// assertStringSlice fails unless a bound argument is exactly the expected slice.
func assertStringSlice(t *testing.T, what string, arg any, want []string) {
	t.Helper()

	got, ok := arg.([]string)
	if !ok {
		t.Fatalf("%s type = %T, want []string", what, arg)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// TestGenerationsAppendKeepsTheArraysAligned guards the one thing the type does
// not enforce. unnest pairs the two arrays by position, so a scope id without
// its generation id silently shifts every later pair onto the wrong generation:
// scope A's rebuild would reset scope B's state.
func TestGenerationsAppendKeepsTheArraysAligned(t *testing.T) {
	t.Parallel()

	var generations Generations
	generations.Append("scope-a", "gen-a")
	generations.Append("scope-b", "gen-b")

	if got, want := generations.Len(), 2; got != want {
		t.Fatalf("Generations.Len() = %d, want %d", got, want)
	}
	if len(generations.ScopeIDs) != len(generations.GenerationIDs) {
		t.Fatalf("Generations arrays are misaligned: %v vs %v", generations.ScopeIDs, generations.GenerationIDs)
	}
	args := generations.Args()
	if len(args) != 2 {
		t.Fatalf("Generations.Args() = %v, want two arrays", args)
	}
	assertStringSlice(t, "Args()[0]", args[0], []string{"scope-a", "scope-b"})
	assertStringSlice(t, "Args()[1]", args[1], []string{"gen-a", "gen-b"})
}

// TestResetQueriesTouchOnlyTerminalState guards the concurrency contract. The
// reducer delete must stay scoped to 'succeeded': claimed and running rows hold
// live leases, and deleting one lets a second worker claim the same conflict key
// while the first is still executing. The shared-intent reopen must stay an
// UPDATE, because the payload is the drain's input and deleting it would lose
// the edges outright.
func TestResetQueriesTouchOnlyTerminalState(t *testing.T) {
	t.Parallel()

	var generations Generations
	generations.Append("scope-a", "gen-a")

	reducerDelete, _ := buildResetQuery(deleteSucceededReducerWorkTemplate, generations)
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

	sharedReopen, _ := buildResetQuery(reopenSharedIntentsTemplate, generations)
	if !strings.HasPrefix(strings.TrimSpace(sharedReopen), "UPDATE shared_projection_intents") {
		t.Fatalf("shared intents must be reopened with UPDATE, not deleted; the payload is "+
			"the drain's input\n%s", sharedReopen)
	}
	if !strings.Contains(sharedReopen, "completed_at IS NOT NULL") {
		t.Fatalf("shared intent reopen dropped its completed_at IS NOT NULL guard, so its "+
			"reported count would mean 'matched' rather than 'reopened'\n%s", sharedReopen)
	}
}

// TestResetRetiresResolutionGenerations is the #6184 regression: the phase
// wipe alone leaves relationship_generations active, so the re-projection's
// by-repos resolved read serves the prior wave's rows as current truth and
// deployable-unit correlation succeeds reduced. Retirement must stay scoped
// to active rows of the refinalized pairs: superseding anything else would
// strand generations this refinalize never re-resolves.
func TestResetRetiresResolutionGenerations(t *testing.T) {
	t.Parallel()

	var generations Generations
	generations.Append("scope-a", "gen-a")

	retire, _ := buildResetQuery(retireResolutionGenerationsTemplate, generations)
	if !strings.HasPrefix(strings.TrimSpace(retire), "UPDATE relationship_generations") {
		t.Fatalf("generations must be retired with UPDATE, not deleted; resolution "+
			"re-activates the same pair by upsert\n%s", retire)
	}
	if !strings.Contains(retire, "status = 'active'") {
		t.Fatalf("generation retirement is not scoped to active rows\n%s", retire)
	}
	if !strings.Contains(retire, "SET status = 'superseded'") {
		t.Fatalf("generation retirement does not supersede\n%s", retire)
	}
}

// TestResetSparesBackwardEvidencePhases is the #6184 companion: the spared
// keyspace attests Postgres evidence-fact completeness, which a graph-only
// rebuild preserves. Wiping it strands re-resolution in deferral because the
// ingestion fan-in that publishes it never re-runs on a projection-only
// rebuild, while the wiped graph-write phases are republished as each phase
// commits.
func TestResetSparesBackwardEvidencePhases(t *testing.T) {
	t.Parallel()

	var generations Generations
	generations.Append("scope-a", "gen-a")

	clear, _ := buildResetQuery(clearReadinessPhaseStateTemplate, generations)
	// Pinned to the constant, not a copied literal: a drift makes this gate
	// silently stop sparing, which is why the exclusion is asserted against
	// gpphase rather than re-stating the string.
	if !strings.Contains(clear, "keyspace <> '"+string(gpphase.KeyspaceCrossRepoEvidence)+"'") {
		t.Fatalf("phase wipe does not spare the backward-evidence keyspace %q; the "+
			"re-projection's resolution would defer forever on phases nothing "+
			"republishes", string(gpphase.KeyspaceCrossRepoEvidence))
	}
}

// TestResetRetirementGuardsAgainstLiveReducerLeases is the Codex #6184 P1
// hermetic pin for the atomic half of the in-flight reducer fence. Retirement
// must commit only when no reducer row holds a live lease on the refinalized
// pairs, in the same statement: a drain-wait poll alone leaves the
// poll-to-commit window open for a claim landing between the last poll and the
// UPDATE. Asserted against the shipped constant, not a copied literal, and the
// arg reuse ($1/$2 re-unnested) is asserted too: a guard binding a different
// set than the outer IN retires would fence the wrong generations.
func TestResetRetirementGuardsAgainstLiveReducerLeases(t *testing.T) {
	t.Parallel()

	var generations Generations
	generations.Append("scope-a", "gen-a")

	retire, args := buildResetQuery(retireResolutionGenerationsTemplate, generations)
	if !strings.Contains(retire, "NOT EXISTS") {
		t.Fatalf("generation retirement lost its live-lease guard; a resolver claimed before "+
			"refinalize would get its generation retired mid-flight, then re-activate it stale\n%s", retire)
	}
	for _, want := range []string{
		"fact_work_items",
		"stage = 'reducer'",
		"'claimed'",
		"'running'",
		"claim_until > now()",
		"unnest($1::text[], $2::text[])",
	} {
		if !strings.Contains(retire, want) {
			t.Fatalf("generation retirement guard does not pin %q; the fence would admit "+
				"stale in-flight resolution\n%s", want, retire)
		}
	}
	if len(args) != 2 {
		t.Fatalf("retirement binds %d args, want 2: the guard must reuse the outer "+
			"generation arrays, not bind its own set", len(args))
	}
}
