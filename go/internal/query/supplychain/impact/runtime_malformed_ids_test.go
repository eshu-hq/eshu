// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

func TestAddSupplyChainRuntimeContextFactRejectsNonStringDirectIDs(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFactForRepository(
		out,
		WorkloadIdentityFactKindQuery,
		"repository:r_malformed",
		map[string]any{"workload_id": map[string]any{"workload:object": true}},
	)
	addSupplyChainRuntimeContextFactForRepository(
		out,
		serviceCatalogCorrelationFactKind,
		"repository:r_malformed",
		map[string]any{
			"service_id":  []any{"service:array"},
			"workload_id": true,
			"outcome":     "exact",
		},
	)
	addSupplyChainRuntimeContextFactForRepository(
		out,
		cicdRunCorrelationFactKind,
		"repository:r_malformed",
		map[string]any{
			"environment": 5747,
			"outcome":     "exact",
		},
	)
	addSupplyChainRuntimeContextFactForRepository(
		out,
		serviceCatalogCorrelationFactKind,
		"repository:r_malformed",
		map[string]any{
			"service_id": "service:invalid-outcome",
			"outcome":    map[string]any{"exact": true},
		},
	)

	context := out["repository:r_malformed"]
	if len(context.WorkloadIDs) != 0 {
		t.Fatalf("WorkloadIDs = %v, want non-string direct IDs rejected", context.WorkloadIDs)
	}
	if len(context.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %v, want non-string direct IDs rejected", context.ServiceIDs)
	}
	if len(context.Environments) != 0 {
		t.Fatalf("Environments = %v, want non-string direct IDs rejected", context.Environments)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDRejectsNonStringDirectAnchors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "repository_object",
			payload: map[string]any{
				"repository_id": map[string]any{"repository:decoy": true},
			},
		},
		{
			name: "repo_array",
			payload: map[string]any{
				"repo_id": []any{"repository:decoy"},
			},
		},
		{
			name: "scope_boolean",
			payload: map[string]any{
				"scope_id": true,
			},
		},
		{
			name: "scope_number",
			payload: map[string]any{
				"scope_id": 5747,
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := supplyChainRuntimeContextRepositoryID(
				tc.payload,
				"git-repository-scope:repository:r_fallback",
			)
			if got != "repository:r_fallback" {
				t.Fatalf("repository ID = %q, want envelope-scope fallback", got)
			}
		})
	}
}
