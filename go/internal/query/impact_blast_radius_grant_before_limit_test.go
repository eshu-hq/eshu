// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// TestBlastRadiusGrantFragmentAddsNoEdgeTokens guards the interaction between
// the #5167 W3 P1 grant push-down and the #5335 edge-materialization gate. The
// grant fragment injected into every blast-radius const (via
// graphPredicateOnProperty / graphConditionOnProperty) is a bound-node property
// predicate (`a.id IN $allowed_repository_ids OR ...`); it must NOT introduce
// any relationship traversal, or the gate's extractRelationshipTypeTokens would
// see an edge type the query "claims" without a coverage disclosure, and the
// affected consts would have to over-declare edges they never traverse. This
// keeps the grant push-down orthogonal to the edge-materialization contract:
// the const templates stay literal BasicLits (gate-parseable) and the grant
// adds property filtering only.
func TestBlastRadiusGrantFragmentAddsNoEdgeTokens(t *testing.T) {
	t.Parallel()

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-a"},
		allowedScopeIDs:      []string{"scope-a"},
		allowed:              map[string]struct{}{"repo-a": {}, "scope-a": {}},
	}
	for _, fragment := range []struct {
		label string
		text  string
	}{
		{"condition", scoped.graphConditionOnProperty("a", "id")},
		{"predicate", scoped.graphPredicateOnProperty("a", "id")},
		{"where", scoped.graphWhereClauseOnProperty("repo", "id")},
	} {
		if tokens := extractRelationshipTypeTokens(fragment.text); len(tokens) != 0 {
			t.Errorf("grant %s fragment %q introduced relationship tokens %v; the grant push-down must add property filtering only, no edge traversal", fragment.label, fragment.text, tokens)
		}
	}
}

// grantBeforeLimitFake is an honest in-memory graph that emulates the exact
// database behavior the #5167 W3 P1 bug depends on: a scoped grant predicate
// that lives in the query WHERE is applied BEFORE the ORDER BY / LIMIT, but a
// grant applied only after the query (post-fetch Go filtering) sees a page
// already truncated by cross-tenant rows. Run:
//  1. filters the corpus to the caller's grant IFF the cypher carries the
//     `IN $allowed_repository_ids` marker (grant pushed into WHERE);
//  2. sorts by (hops, repo) to match every affected const's `ORDER BY hops, repo`;
//  3. truncates to params["limit"].
//
// This is the false-green-resistant harness the W3 review requires: it does not
// trust the production filter helper, it reproduces the database's own
// filter/sort/limit ordering so the test can distinguish grant-in-WHERE from
// grant-after-query.
type grantBeforeLimitFake struct {
	corpus []map[string]any
}

func (g grantBeforeLimitFake) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(g.corpus))
	if strings.Contains(cypher, "IN $allowed_repository_ids") {
		allowed := map[string]struct{}{}
		for _, key := range []string{"allowed_repository_ids", "allowed_scope_ids"} {
			if ids, ok := params[key].([]string); ok {
				for _, id := range ids {
					allowed[id] = struct{}{}
				}
			}
		}
		for _, row := range g.corpus {
			if _, ok := allowed[StringVal(row, "repo_id")]; ok {
				rows = append(rows, row)
			}
		}
	} else {
		rows = append(rows, g.corpus...)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		hi, hj := IntVal(rows[i], "hops"), IntVal(rows[j], "hops")
		if hi != hj {
			return hi < hj
		}
		return StringVal(rows[i], "repo") < StringVal(rows[j], "repo")
	})
	if limit := IntVal(params, "limit"); limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (g grantBeforeLimitFake) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestBlastRadiusGrantBoundBeforeLimit is the #5167 W3 P1 filter-before-limit
// regression. A scoped caller granted only repo-a queries a blast radius whose
// affected set is dominated by cross-tenant repos that sort BEFORE the one
// granted repo. With the grant filtered only after the query (the pre-fix
// shape), the query's own LIMIT keeps the top-N cross-tenant rows and the
// granted row falls off the page, so the post-fetch filter returns nothing —
// the authorized caller sees an empty/incomplete blast radius. Pushing the
// grant into the query WHERE (the fix) makes the LIMIT bound the GRANTED set,
// so the granted row survives.
//
// RED companion (grantAfterQuery) proves the honest fake genuinely loses the
// row without the fix; GREEN (the production blastRadiusAffected builder) proves
// the shipped query keeps it. Same corpus, same grant, same limit.
func TestBlastRadiusGrantBoundBeforeLimit(t *testing.T) {
	t.Parallel()

	const limit = 2
	// Three cross-tenant repos (repo-x*) sort before the single granted repo
	// (repo-a, name "zzz-granted") under ORDER BY hops, repo — all hops=1, so
	// pure name order. Cross-tenant count (3) exceeds limit (2), so a
	// grant-after-query page is entirely cross-tenant.
	corpus := []map[string]any{
		{"repo": "aaa-cross1", "repo_id": "repo-x1", "hops": int64(1)},
		{"repo": "aaa-cross2", "repo_id": "repo-x2", "hops": int64(1)},
		{"repo": "aaa-cross3", "repo_id": "repo-x3", "hops": int64(1)},
		{"repo": "zzz-granted", "repo_id": "repo-a", "hops": int64(1)},
	}
	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-a"},
		allowed:              map[string]struct{}{"repo-a": {}},
	}
	fake := grantBeforeLimitFake{corpus: corpus}
	handler := &ImpactHandler{Neo4j: fake, Profile: ProfileLocalAuthoritative}

	// RED companion: the pre-fix shape ran the affected query with NO grant in
	// WHERE (blastRadiusRepositoryQuery on an all-scopes filter emits the
	// grant-free const) and filtered only afterward. Prove the honest fake drops
	// the granted row so the post-fetch filter yields nothing.
	noGrantQuery := blastRadiusRepositoryQuery(repositoryAccessFilter{allScopes: true})
	if strings.Contains(noGrantQuery, "IN $allowed_repository_ids") {
		t.Fatalf("unscoped blast-radius query unexpectedly carries a grant predicate: %s", noGrantQuery)
	}
	prefixRows, err := fake.Run(context.Background(), noGrantQuery, scoped.graphParams(map[string]any{"target_name": "svc", "limit": limit}))
	if err != nil {
		t.Fatalf("RED companion run: %v", err)
	}
	prefixRows = filterRowsByRepoIDForAccess(mergeBlastRadiusRows(prefixRows), scoped)
	if repoNames(prefixRows)["zzz-granted"] {
		t.Fatalf("RED companion should demonstrate the bug: granted row must be LOST when the grant is filtered after the LIMIT, got %v", repoNames(prefixRows))
	}
	if len(prefixRows) != 0 {
		t.Fatalf("RED companion: pre-fix page should be empty for this corpus (all top-limit rows cross-tenant), got %v", repoNames(prefixRows))
	}

	// GREEN: the production builder pushes the grant into WHERE, so the fake
	// filters to the granted set BEFORE the LIMIT and the granted row survives.
	affected, supported, _, _, err := handler.blastRadiusAffected(context.Background(), "repository", "svc", limit, scoped)
	if err != nil {
		t.Fatalf("GREEN blastRadiusAffected: %v", err)
	}
	if !supported {
		t.Fatalf("repository target_type must be supported")
	}
	affected = filterRowsByRepoIDForAccess(affected, scoped)
	if !repoNames(affected)["zzz-granted"] {
		t.Fatalf("GREEN: granted row must survive when the grant is bound before the LIMIT, got %v", repoNames(affected))
	}
	for _, row := range affected {
		if !scoped.allowsRepositoryID(StringVal(row, "repo_id")) {
			t.Fatalf("GREEN: scoped page leaked a non-granted repo: %v", repoNames(affected))
		}
	}
}

// repoNames returns the set of "repo" values across rows for concise assertions.
func repoNames(rows []map[string]any) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[StringVal(row, "repo")] = true
	}
	return out
}
