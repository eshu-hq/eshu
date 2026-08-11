// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestBuildKubernetesRuntimeWorkloadQueryGatesOwnerAndEdgeIndependently(t *testing.T) {
	t.Parallel()

	query, args := buildKubernetesRuntimeWorkloadQuery([]KubernetesRuntimeCandidate{{
		WorkloadUID: "workload-1", Digest: "sha256:abc", EdgeScopeID: "edge-scope", EdgeGenerationID: "edge-generation",
	}}, false, []string{"repository:allowed"}, []string{"scope:allowed"})

	for _, want := range []string{
		"UNNEST(",
		"owner.uid = candidate.workload_uid",
		"owner_scope.active_generation_id = owner_fact.generation_id",
		"owner_generation.status = 'active'",
		"owner_fact.is_tombstone = FALSE",
		"edge_scope.scope_id = candidate.edge_scope_id",
		"edge_scope.active_generation_id = candidate.edge_generation_id",
		"edge_generation.status = 'active'",
		"owner_scope.source_key = ANY(",
		"edge_scope.source_key = ANY(",
		"LIMIT",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "owner_fact.scope_id = candidate.edge_scope_id") ||
		strings.Contains(query, "owner_fact.generation_id = candidate.edge_generation_id") {
		t.Fatalf("query incorrectly requires owner and edge provenance equality:\n%s", query)
	}
	if len(args) != 7 {
		t.Fatalf("argument count = %d, want 7", len(args))
	}
	if got, ok := args[4].(int); !ok || got != supplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("candidate limit = %#v, want %d", args[4], supplyChainKubernetesRuntimeProbeMaxResults)
	}
}

func TestBuildKubernetesRuntimeWorkloadQueryAllScopesOmitsAuthorizationPredicates(t *testing.T) {
	t.Parallel()

	query, args := buildKubernetesRuntimeWorkloadQuery([]KubernetesRuntimeCandidate{{
		WorkloadUID: "workload-1", Digest: "sha256:abc", EdgeScopeID: "edge-scope", EdgeGenerationID: "edge-generation",
	}}, true, nil, nil)
	if strings.Contains(query, "source_key = ANY(") {
		t.Fatalf("all-scopes query unexpectedly includes authorization predicates:\n%s", query)
	}
	if len(args) != 5 {
		t.Fatalf("argument count = %d, want 5", len(args))
	}
	if got, ok := args[4].(int); !ok || got != supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("candidate limit = %#v, want %d", args[4], supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
}
