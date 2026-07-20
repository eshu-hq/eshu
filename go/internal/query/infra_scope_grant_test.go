// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"
)

// TestScopeGrantInlineScalarsDedupesSortsAndDropsEmpty proves the scalar
// builder produces a deterministic, deduplicated, empty-dropped union of the
// repository and scope grant arrays so the predicate and param builders always
// derive an identical ordered slice.
func TestScopeGrantInlineScalarsDedupesSortsAndDropsEmpty(t *testing.T) {
	t.Parallel()

	scalars, capped := scopeGrantInlineScalars(
		[]string{"repo-b", "repo-a", "", "repo-a"},
		[]string{"scope-x", "repo-b", ""},
	)
	if capped {
		t.Fatalf("small grant set must not report capped")
	}
	want := []string{"repo-a", "repo-b", "scope-x"}
	if fmt.Sprint(scalars) != fmt.Sprint(want) {
		t.Fatalf("scalars = %v, want %v (deduped, sorted, empty dropped)", scalars, want)
	}
}

// TestScopeGrantInlineScalarsCapsAtMax proves the O(grant) fan-out is bounded at
// maxScopeGrantInlineTerms and reports capped=true, so a pathological grant set
// cannot produce an unbounded OR-chain.
func TestScopeGrantInlineScalarsCapsAtMax(t *testing.T) {
	t.Parallel()

	repoIDs := make([]string, maxScopeGrantInlineTerms+50)
	for i := range repoIDs {
		// zero-pad so sort order is stable and the first N are deterministic.
		repoIDs[i] = fmt.Sprintf("repo-%04d", i)
	}
	scalars, capped := scopeGrantInlineScalars(repoIDs, nil)
	if !capped {
		t.Fatalf("grant set larger than the cap must report capped=true")
	}
	if len(scalars) != maxScopeGrantInlineTerms {
		t.Fatalf("scalars length = %d, want cap %d", len(scalars), maxScopeGrantInlineTerms)
	}
	if scalars[0] != "repo-0000" {
		t.Fatalf("capped scalars must keep the deterministic sorted prefix; got first %q", scalars[0])
	}
}

// TestScopeGrantInlineMapDisjunctionDirectionAndParams proves the primitive
// emits one inline-map pattern term per scalar with the correct arrow direction
// and the scope_grant_<i> param keys, and returns empty for no scalars.
func TestScopeGrantInlineMapDisjunctionDirectionAndParams(t *testing.T) {
	t.Parallel()

	if got := scopeGrantInlineMapDisjunction("n", scopeHopInbound, "USES", "WorkloadInstance", "repo_id", nil); got != "" {
		t.Fatalf("empty scalars must render empty, got %q", got)
	}

	inbound := scopeGrantInlineMapDisjunction("n", scopeHopInbound, "USES", "WorkloadInstance", "repo_id", []string{"a", "b"})
	wantInbound := "((n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0}) OR (n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_1}))"
	if inbound != wantInbound {
		t.Fatalf("inbound disjunction =\n%s\nwant\n%s", inbound, wantInbound)
	}

	outbound := scopeGrantInlineMapDisjunction("w", scopeHopOutbound, "DEPLOYMENT_SOURCE", "Repository", "id", []string{"a"})
	wantOutbound := "((w)-[:DEPLOYMENT_SOURCE]->(:Repository {id:$scope_grant_0}))"
	if outbound != wantOutbound {
		t.Fatalf("outbound disjunction =\n%s\nwant\n%s", outbound, wantOutbound)
	}
}

// TestBindScopeGrantInlineScalarsBindsIndexedKeys proves the param binder writes
// the scope_grant_<i> keys the disjunction references, and is idempotent.
func TestBindScopeGrantInlineScalarsBindsIndexedKeys(t *testing.T) {
	t.Parallel()

	params := map[string]any{}
	scalars := []string{"repo-a", "repo-b"}
	bindScopeGrantInlineScalars(params, scalars)
	bindScopeGrantInlineScalars(params, scalars) // idempotent re-bind is safe.
	if params["scope_grant_0"] != "repo-a" || params["scope_grant_1"] != "repo-b" {
		t.Fatalf("scope_grant params = %v, want scope_grant_0=repo-a scope_grant_1=repo-b", params)
	}
	if len(params) != 2 {
		t.Fatalf("idempotent re-bind must not add keys, got %v", params)
	}
}

// TestInfraResourceScopePredicateComposesShapeAAndRejectsForbiddenShapes pins
// the composed SHAPE-A predicate: flat direct-ownership + USES inline-map +
// forward DEPLOYMENT_SOURCE EXISTS + DEFINES inline-map, and proves the two
// NornicDB-mis-evaluated shapes (dead n-last bridge, always-true
// backward-EXISTS-with-WHERE) are absent.
func TestInfraResourceScopePredicateComposesShapeAAndRejectsForbiddenShapes(t *testing.T) {
	t.Parallel()

	scalars := []string{"repo-a"}
	pred := infraResourceScopePredicate("n", scalars)

	for _, want := range []string{
		"n.repo_id IN $allowed_repository_ids",
		"n.repo_id IN $allowed_scope_ids",
		"n.id IN $allowed_repository_ids",
		"n.id IN $allowed_scope_ids",
		"(n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0})",
		"EXISTS { MATCH (n)-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository) WHERE (scopeDeployRepo.id IN $allowed_repository_ids OR scopeDeployRepo.id IN $allowed_scope_ids) }",
		"(n)<-[:DEFINES]-(:Repository {id:$scope_grant_0})",
	} {
		if !strings.Contains(pred, want) {
			t.Fatalf("SHAPE-A predicate missing %q:\n%s", want, pred)
		}
	}
	for _, forbidden := range []string{
		"(scopeRepo:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]", // dead n-last bridge
		"(n)<-[:USES]-(scopeInstance",                                    // always-true backward-EXISTS-WHERE
		"WHERE scopeInstance.repo_id IN",                                 // always-true backward-EXISTS-WHERE
	} {
		if strings.Contains(pred, forbidden) {
			t.Fatalf("SHAPE-A predicate must not contain forbidden NornicDB-mis-evaluated shape %q:\n%s", forbidden, pred)
		}
	}
}

// TestInfraResourceScopePredicateUnscopedOmitsInlineMap proves that with no
// grant scalars (the empty-scalar path used before an empty-grant short-circuit,
// and the shape for callers that pass no scalars) the predicate still renders
// the flat and forward disjuncts but no inline-map term.
func TestInfraResourceScopePredicateEmptyScalarsOmitsInlineMap(t *testing.T) {
	t.Parallel()

	pred := infraResourceScopePredicate("n", nil)
	if strings.Contains(pred, "scope_grant_") {
		t.Fatalf("empty scalars must not emit inline-map terms:\n%s", pred)
	}
	if !strings.Contains(pred, "n.repo_id IN $allowed_repository_ids") {
		t.Fatalf("empty scalars must still emit the flat direct-ownership disjuncts:\n%s", pred)
	}
}

// TestInfraResourceScopePredicateCapDegradesFailClosed proves the fail-closed
// cap behavior: past maxScopeGrantInlineTerms the inline-map disjuncts cover
// exactly the cap (first N sorted grants), while the flat array disjuncts remain
// so ALL direct-ownership grants are still admitted (the degradation is missing
// collision/bridge rows, never extra rows).
func TestInfraResourceScopePredicateCapDegradesFailClosed(t *testing.T) {
	t.Parallel()

	repoIDs := make([]string, maxScopeGrantInlineTerms+10)
	for i := range repoIDs {
		repoIDs[i] = fmt.Sprintf("repo-%04d", i)
	}
	scalars, capped := scopeGrantInlineScalars(repoIDs, nil)
	if !capped {
		t.Fatalf("oversized grant set must report capped")
	}
	pred := infraResourceScopePredicate("n", scalars)

	// Flat array disjuncts remain (direct ownership still admitted for ALL grants
	// via the full $allowed_repository_ids array bound elsewhere).
	if !strings.Contains(pred, "n.repo_id IN $allowed_repository_ids") {
		t.Fatalf("capped predicate must retain the flat direct-ownership disjunct:\n%s", pred)
	}
	// The inline-map terms are capped: scope_grant_{N-1} present, scope_grant_{N} absent.
	if !strings.Contains(pred, fmt.Sprintf("$scope_grant_%d}", maxScopeGrantInlineTerms-1)) {
		t.Fatalf("capped predicate must include the last in-cap inline-map term scope_grant_%d", maxScopeGrantInlineTerms-1)
	}
	if strings.Contains(pred, fmt.Sprintf("$scope_grant_%d}", maxScopeGrantInlineTerms)) {
		t.Fatalf("capped predicate must NOT include an inline-map term beyond the cap (scope_grant_%d)", maxScopeGrantInlineTerms)
	}
}

// TestWorkloadScopePredicateComposesShapeA proves the service workload predicate
// admits by flat repo_id plus the DEFINES inline-map (collision-defined
// workloads), and drops the dead n-last DEFINES EXISTS bridge.
func TestWorkloadScopePredicateComposesShapeA(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-a"},
		allowed:              map[string]struct{}{"repo-a": {}},
	}
	pred := workloadScopePredicate("w", access)
	for _, want := range []string{
		"w.repo_id IN $allowed_repository_ids",
		"w.repo_id IN $allowed_scope_ids",
		"(w)<-[:DEFINES]-(:Repository {id:$scope_grant_0})",
	} {
		if !strings.Contains(pred, want) {
			t.Fatalf("workload SHAPE-A predicate missing %q:\n%s", want, pred)
		}
	}
	if strings.Contains(pred, "(scopeRepo:Repository)-[:DEFINES]->(w)") {
		t.Fatalf("workload predicate must not contain the dead n-last DEFINES bridge:\n%s", pred)
	}
}
