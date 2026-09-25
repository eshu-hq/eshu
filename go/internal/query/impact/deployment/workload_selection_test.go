// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestResolveWorkloadSelectorRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return nil, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			if !strings.Contains(cypher, "ORDER BY w.id") {
				t.Fatalf("name selector query = %q, want deterministic ambiguity ordering", cypher)
			}
			return []map[string]any{
				{"id": "workload:orders-a", "name": "orders"},
				{"id": "workload:orders-b", "name": "orders"},
			}, nil
		default:
			t.Fatalf("unexpected query: %s", cypher)
			return nil, nil
		}
	}}

	_, err := ResolveWorkloadSelector(t.Context(), reader, "orders", nil, nil)
	if !errors.Is(err, errAmbiguousWorkloadSelector) {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want ambiguity", err)
	}
}

// TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting is the
// #5720 P2-3 handler-level half of the overflow fix, retargeted in round 2
// (P1-2): the first draft of this boundary rejected an absurd max_depth with
// 400, but that broke the wire contract every sibling max_depth-bearing
// route keeps (impact/resource_investigation.go,
// impact/change_surface_investigation.go, impact/change_surface_legacy.go
// all normalize rather than reject) and silently changed observable
// behavior for existing callers, including the MCP dispatch route which
// forwards an explicit max_depth straight through. A negative max_depth, and
// a max_depth large enough that boundedTraceEnrichmentLimit's `maxDepth *
// 10` would overflow int64 if it ever reached that function unclamped, must
// both 200 rather than reject. This drives the real request through the real
// handler and observes the wire-visible limit param the
// provisioning-candidates Cypher query (deployment_trace_support_helpers.go)
// actually receives.
//
// #5720 round-4 P2: this HTTP-level assertion alone cannot distinguish
// whether normalizeTraceDeploymentChainMaxDepth (impact/trace_deployment.go)
// actually ran, because boundedTraceEnrichmentLimit maps every int input --
// including both cases below -- into (0, maxIndirectEvidenceSearchLimit] on
// its own; deleting the handler clamp yields the identical wantTraceLimit for
// both. What this test genuinely proves is the response-code half of the
// contract: out-of-range max_depth 200s instead of rejecting. See
// TestNormalizeTraceDeploymentChainMaxDepth for the clamp's own boundary
// behavior, proven directly against the extracted pure function.

func TestResolveWorkloadSelectorPreservesExactIDLookup(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "w.id = $service_name") {
			t.Fatalf("first query = %q, want exact id lookup", cypher)
		}
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		return []map[string]any{{"id": "workload:orders"}}, nil
	}}

	got, err := ResolveWorkloadSelector(t.Context(), reader, "workload:orders", nil, nil)
	if err != nil || got != "workload:orders" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want exact workload id", got, err)
	}
}

// TestResolveWorkloadSelectorIDRowMismatchIsNotTrusted is the #6786
// review follow-up (F3): a row whose own id differs from the requested
// selector must never be trusted as an answer, even if the row itself would
// otherwise be grant-admitted -- it falls through to the name lookup
// (finding nothing here) rather than returning a different workload's id.
func TestResolveWorkloadSelectorIDRowMismatchIsNotTrusted(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return []map[string]any{{"id": "workload:different", "repo_id": "repo-a"}}, nil
		default:
			return nil, nil
		}
	}}

	got, err := ResolveWorkloadSelector(t.Context(), reader, "workload:requested", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found for a row whose id does not match the selector", got)
	}
}

// TestResolveWorkloadSelectorNameRowMismatchIsNotTrusted is the
// name-lookup half of F3: a name-query row whose own name differs from the
// selector must be dropped even if it is grant-admitted.
func TestResolveWorkloadSelectorNameRowMismatchIsNotTrusted(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return nil, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			return []map[string]any{{"id": "workload:mismatch", "name": "not-orders", "repo_id": "repo-a"}}, nil
		default:
			return nil, nil
		}
	}}

	got, err := ResolveWorkloadSelector(t.Context(), reader, "orders", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found for a name-query row whose name does not match the selector", got)
	}
}

// scopedAuthContext returns a context carrying a scoped AuthContext granted
// only allowedRepositoryIDs, the same shape production request middleware
// installs for a scoped caller.
func scopedAuthContext(allowedRepositoryIDs ...string) context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	})
}

// TestResolveWorkloadSelectorScopedOutOfGrantIDReturnsNotFound is the
// #6786 regression: a scoped caller's exact-id selector for a workload it has
// no grant to must resolve to "" (not found), never to a different,
// unrelated workload the caller happens to be granted -- the failure this
// package's retired Cypher-embedded grant predicate produced on NornicDB
// v1.3.3.
func TestResolveWorkloadSelectorScopedOutOfGrantIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{
				"id": "workload:out-of-grant", "repo_id": "repo-b", "defining": []string{},
			}}, nil
		}
		return nil, nil
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), reader, "workload:out-of-grant", nil, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found for an ungranted workload id", got)
	}
}

// TestResolveWorkloadSelectorScopedDirectGrantAdmits proves the direct
// admission route: the workload's own materialized repo_id is granted.
func TestResolveWorkloadSelectorScopedDirectGrantAdmits(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{
				"id": "workload:in-grant", "repo_id": "repo-a", "defining": []string{},
			}}, nil
		}
		return nil, nil
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), reader, "workload:in-grant", nil, nil)
	if err != nil || got != "workload:in-grant" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want workload:in-grant", got, err)
	}
}

// TestResolveWorkloadSelectorScopedDefinesGrantAdmits proves the
// name-collision admission route: the workload's own repo_id names an
// ungranted repository, but a granted repository DEFINES it.
func TestResolveWorkloadSelectorScopedDefinesGrantAdmits(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{
				"id": "workload:collision", "repo_id": "repo-other", "defining": []string{"repo-z", "repo-a"},
			}}, nil
		}
		return nil, nil
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), reader, "workload:collision", nil, nil)
	if err != nil || got != "workload:collision" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want workload:collision (DEFINES-admitted)", got, err)
	}
}

// TestResolveWorkloadSelectorCandidateBoundFailsClosed proves the
// fail-closed behavior documented on workloadSelectorCandidateBound: a
// name-lookup page that reaches the bound is reported as an error rather than
// silently deciding admission/ambiguity from a possibly-truncated page.
func TestResolveWorkloadSelectorCandidateBoundFailsClosed(t *testing.T) {
	t.Parallel()

	overBound := make([]map[string]any, workloadSelectorCandidateBound+1)
	for i := range overBound {
		overBound[i] = map[string]any{"id": "workload:dup", "repo_id": "repo-a", "defining": []string{}}
	}
	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		return overBound, nil
	}}

	_, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), reader, "orders", nil, nil)
	if !errors.Is(err, querycontract.ErrWorkloadSelectorCandidatesExceedBound) {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want candidate-bound error", err)
	}
}

// TestNormalizeTraceDeploymentChainMaxDepth is the #5720 round-4 P2 direct
// unit proof for normalizeTraceDeploymentChainMaxDepth
// (impact/trace_deployment.go), extracted from the inline handler clamp so
// its boundary behavior has coverage independent of
// boundedTraceEnrichmentLimit's own saturation (see the reworded doc comment
// on TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting above for
// why the HTTP-level test alone cannot prove this). Mutation-proof: neutering
// the clamp body to `return maxDepth` fails 5 of the 7 cases below;
// maxDepth=0 and maxDepth=1000 are identity cases (the clamp is a no-op at
// those inputs even before mutation) and survive the mutant by construction.
