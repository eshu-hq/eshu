// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestImportsTargetIDDistinguishesModuleLanguages covers the #6102 review
// finding that IMPORTS lost the ability to name its target.
//
// Canonical import Modules carry no id and no uid, so the default
// coalesce(t.id, t.uid, t.name, t.path) projection resolved to the module name.
// Once Module identity became (name, lang), Module{name:"time", lang:"go"} and
// Module{name:"time", lang:"python"} are two graph nodes that both projected
// target_id "time", and a caller could not tell which edge pointed where.
func TestImportsTargetIDDistinguishesModuleLanguages(t *testing.T) {
	t.Parallel()

	entry, ok := relationshipVerbByName["IMPORTS"]
	if !ok {
		t.Fatal("IMPORTS missing from the relationship verb catalog")
	}

	got := targetIdentityCoalesce(entry)
	want := "coalesce(t.id, t.uid, CASE WHEN t.name IS NULL THEN NULL ELSE t.name + '@' + coalesce(t.lang, '') END, t.path)"
	if got != want {
		t.Fatalf("IMPORTS target_id projection =\n  %s\nwant\n  %s", got, want)
	}

	// The language must reach the projected value, not merely be mentioned.
	if !strings.Contains(got, "t.lang") {
		t.Fatalf("IMPORTS target_id does not read the target's language, so two same-named "+
			"modules still project one id:\n%s", got)
	}

	// And it must survive into both emitted statements. The query-plan gate
	// pins QP-RELATIONSHIPS-EDGES against the CALLS representative of this
	// 20-verb family (see queryplan_legacy_production_binding_test.go), and
	// CALLS keeps the default projection, so a verb-specific override is
	// invisible to that gate. This is what covers it instead.
	access := repositoryAccessFilter{AllScopes: true}
	for name, cypher := range map[string]string{
		"relationshipEdgesCypher":         relationshipEdgesCypher(entry, access),
		"relationshipEdgesCypherFiltered": relationshipEdgesCypherFiltered(entry, access),
	} {
		if !strings.Contains(cypher, want+" AS target_id") {
			t.Fatalf("%s does not project the language-qualified target_id:\n%s", name, cypher)
		}
	}
}

// TestImportsTargetIDGuardsAgainstANullName pins the two things the pinned
// backend forced into this expression.
//
// On eshu-nornicdb-pr290:3722b483c02c, string concatenation does not propagate
// null: `t.name + '@' + t.lang` on a node with no lang returns the literal
// "time@<nil>", and on a node with no name it returns "<nil>@" instead of
// falling through the coalesce to t.path. So the language goes through its own
// coalesce, and the whole concat sits behind a CASE that yields NULL when the
// name is absent, which is what lets the coalesce reach t.path.
func TestImportsTargetIDGuardsAgainstANullName(t *testing.T) {
	t.Parallel()

	got := targetIdentityCoalesce(relationshipVerbByName["IMPORTS"])
	for _, want := range []string{
		"CASE WHEN t.name IS NULL THEN NULL ELSE",
		"coalesce(t.lang, '')",
		", t.path)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("IMPORTS target_id missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "+ t.lang") {
		t.Fatalf("IMPORTS target_id concatenates t.lang unguarded; on the pinned backend a module "+
			"with no lang would project the literal \"<nil>\":\n%s", got)
	}
}

// TestNonImportVerbsKeepTheDefaultTargetIDProjection holds the blast radius
// down. Only IMPORTS gained an expression, so every other catalog entry must
// still emit the byte-identical coalesce the query-plan gate has pinned, and
// MANAGES must keep the property reordering it already had.
func TestNonImportVerbsKeepTheDefaultTargetIDProjection(t *testing.T) {
	t.Parallel()

	for _, entry := range relationshipVerbCatalog {
		if entry.verb == "IMPORTS" {
			continue
		}
		got := targetIdentityCoalesce(entry)
		want := "coalesce(t.id, t.uid, t.name, t.path)"
		if entry.verb == "MANAGES" {
			want = "coalesce(t.path, t.id, t.uid, t.name)"
		}
		if got != want {
			t.Fatalf("%s target_id projection = %q, want %q", entry.verb, got, want)
		}
	}
}

// TestImportsOrderByTiebreakerIsUnchanged records a deliberate non-change.
//
// Two IMPORTS edges out of one File to same-named modules in different
// languages tie on the coalesce(t.id, t.uid) tie-breaker, because neither node
// carries either property. Adding the language-qualified expression there looks
// like the fix, and on the pinned backend it is decorative: ORDER BY over a
// CASE expression is not honoured (verified through the Bolt driver -- four
// seeded edges came back in insertion order, not sorted by the expression), the
// same way ORDER BY over a RETURN alias is not. Changing it would move the
// query-plan gate's pinned hash for a sort that does not happen.
func TestImportsOrderByTiebreakerIsUnchanged(t *testing.T) {
	t.Parallel()

	got := targetOrderTiebreaker(relationshipVerbByName["IMPORTS"])
	if want := "coalesce(t.id, t.uid)"; got != want {
		t.Fatalf("IMPORTS ORDER BY tie-breaker = %q, want the unchanged %q", got, want)
	}
}
