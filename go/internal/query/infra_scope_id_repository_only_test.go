// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"
)

// neo4jRepositoryIDTerm is the Neo4j list dialect's id-equality disjunct: both
// id tests sit under one Repository label test.
func neo4jRepositoryIDTerm(alias string) string {
	return "(" + alias + ":Repository AND (" + alias + ".id IN $allowed_repository_ids OR " +
		alias + ".id IN $allowed_scope_ids))"
}

// shapeARepositoryIDTerm is one SHAPE-A id-equality disjunct over the named
// grant list: the id when the node is a Repository, null otherwise.
func shapeARepositoryIDTerm(alias, list string) string {
	return "(CASE WHEN 'Repository' IN labels(" + alias + ") THEN " + alias + ".id END) IN $allowed_" + list + "_ids"
}

// unguardedIDGrantTerms returns each `<alias>.id IN $allowed_*` comparison left
// in predicate once the two accepted Repository-guarded spellings are removed.
// #7220: an unguarded term admits any label whose id spells a granted id. The
// DEPLOYMENT_SOURCE and DEFINES families compare `scopeDeployRepo.id` and
// `scopeDefiningRepo.id`, which their patterns bind as :Repository, so they use
// other aliases and never match a query alias here.
func unguardedIDGrantTerms(predicate, alias string) []string {
	stripped := strings.ReplaceAll(predicate, neo4jRepositoryIDTerm(alias), "")
	for _, list := range []string{"repository", "scope"} {
		stripped = strings.ReplaceAll(stripped, shapeARepositoryIDTerm(alias, list), "")
	}
	return regexp.MustCompile(regexp.QuoteMeta(alias)+`\.id IN \$allowed_(?:repository|scope)_ids`).FindAllString(stripped, -1)
}

// TestInfraScopePredicatesAdmitIDEqualityOnlyForRepository pins #7220: both
// dialects of the scoped infra grant predicate admit a node by `id` equality
// only when it is a Repository. Every other label is admitted through repo_id
// or the ownership edges, never because its id spells a granted id.
func TestInfraScopePredicatesAdmitIDEqualityOnlyForRepository(t *testing.T) {
	t.Parallel()

	scalars := []string{"repo-00", "scope-00"}
	for _, alias := range []string{"n", "target", "source", "s", "t"} {
		alias := alias
		// labelTerm predicates spell the guard as one label test (Neo4j, and
		// SHAPE-A for a single-node MATCH); the others use the CASE operand
		// (SHAPE-A for an alias bound by a relationship pattern).
		cases := []struct {
			name      string
			predicate string
			labelTerm bool
		}{
			{"neo4j_list", infraResourceScopeListPredicate(alias), true},
			{"shape_a_node", infraResourceScopeNodePredicate(alias, scalars), true},
			{"shape_a_core", strings.Join(infraResourceScopeCoreDisjuncts(alias, scalars), " OR "), false},
			{"shape_a_full", infraResourceScopePredicate(alias, scalars), false},
			{"shape_a_endpoint", relationshipEndpointScopePredicate(alias, scalars), false},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name+"/"+alias, func(t *testing.T) {
				t.Parallel()
				if tc.labelTerm {
					if n := strings.Count(tc.predicate, neo4jRepositoryIDTerm(alias)); n != 1 {
						t.Fatalf("guarded id term on %q occurs %d times, want 1:\n%s", alias, n, tc.predicate)
					}
				} else {
					for _, list := range []string{"repository", "scope"} {
						if n := strings.Count(tc.predicate, shapeARepositoryIDTerm(alias, list)); n != 1 {
							t.Fatalf("guarded %s-id term on %q occurs %d times, want 1:\n%s", list, alias, n, tc.predicate)
						}
					}
				}
				if bad := unguardedIDGrantTerms(tc.predicate, alias); len(bad) > 0 {
					t.Fatalf("id equality admits any label; unguarded terms %v:\n%s", bad, tc.predicate)
				}
			})
		}
	}
}

// TestUnguardedIDGrantTermDetectorSeededViolation is the guard's RED/GREEN
// pair: the pre-#7220 text and a half-guarded variant must be reported, and
// both guarded spellings must pass.
func TestUnguardedIDGrantTermDetectorSeededViolation(t *testing.T) {
	t.Parallel()

	red := map[string]string{
		"pre-7220 bare terms": "(n.repo_id IN $allowed_repository_ids OR n.id IN $allowed_repository_ids OR n.id IN $allowed_scope_ids)",
		"one term unguarded":  "(" + shapeARepositoryIDTerm("n", "repository") + " OR n.id IN $allowed_scope_ids)",
		"wrong label guard":   "(n:Workload AND (n.id IN $allowed_repository_ids OR n.id IN $allowed_scope_ids))",
	}
	for name, predicate := range red {
		if got := unguardedIDGrantTerms(predicate, "n"); len(got) == 0 {
			t.Errorf("seeded RED %q: no unguarded term reported:\n%s", name, predicate)
		}
	}
	green := []string{
		"(n.repo_id IN $allowed_repository_ids OR " + neo4jRepositoryIDTerm("n") + ")",
		"(" + shapeARepositoryIDTerm("n", "repository") + " OR " + shapeARepositoryIDTerm("n", "scope") + ")",
	}
	for _, predicate := range green {
		if got := unguardedIDGrantTerms(predicate, "n"); len(got) != 0 {
			t.Errorf("seeded GREEN predicate reported %v:\n%s", got, predicate)
		}
	}
}
