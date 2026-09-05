// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

func TestSupplyChainImpactRuntimeFiltersResolveCurrentRepositoryContext(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		query          string
		repositoryExpr string
	}{
		{
			name:           "legacy_list",
			query:          ListSupplyChainImpactFindingsQuery,
			repositoryExpr: "fact.payload->>'repository_id'",
		},
		{
			name:           "winners_list",
			query:          ListSupplyChainImpactFindingsFromWinnersQuery,
			repositoryExpr: "w.repository_id",
		},
		{
			name:           "aggregate",
			query:          SupplyChainImpactAggregateCanonicalFactsCTE,
			repositoryExpr: "fact.payload->>'repository_id'",
		},
		{
			name:           "explain",
			query:          ExplainSupplyChainImpactFindingQuery,
			repositoryExpr: "fact.payload->>'repository_id'",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{
				"runtime_filter_repositories AS MATERIALIZED",
				"reducer_workload_identity",
				"reducer_service_catalog_correlation",
				"reducer_ci_cd_run_correlation",
				"runtime_filter.repository_id = " + tc.repositoryExpr,
			} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("query missing current runtime-filter marker %q:\n%s", want, tc.query)
				}
			}
		})
	}
}

func TestSupplyChainImpactRuntimeFiltersNeverUseStaleBakedMembership(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		query     string
		forbidden []string
	}{
		{
			name:  "legacy_list",
			query: ListSupplyChainImpactFindingsQuery,
			forbidden: []string{
				"OR fact.payload->'service_ids' ?",
				"OR fact.payload->'workload_ids' ?",
				"OR fact.payload->'environments' ?",
			},
		},
		{
			name:  "winners_list",
			query: ListSupplyChainImpactFindingsFromWinnersQuery,
			forbidden: []string{
				"OR w.service_ids ?",
				"OR w.workload_ids ?",
				"OR w.environments ?",
			},
		},
		{
			name:  "aggregate",
			query: SupplyChainImpactAggregateCanonicalFactsCTE,
			forbidden: []string{
				"OR fact.payload->'service_ids' ?",
				"OR fact.payload->'workload_ids' ?",
				"OR fact.payload->'environments' ?",
			},
		},
		{
			name:  "explain",
			query: ExplainSupplyChainImpactFindingQuery,
			forbidden: []string{
				"OR fact.payload->'service_ids' ?",
				"OR fact.payload->'workload_ids' ?",
				"OR fact.payload->'environments' ?",
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, forbidden := range tc.forbidden {
				if strings.Contains(tc.query, forbidden) {
					t.Fatalf("query retains stale baked runtime membership %q:\n%s", forbidden, tc.query)
				}
			}
		})
	}
}

func TestSupplyChainImpactRuntimeFilterMirrorsRuntimeContextTruthRules(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"runtime_workload_identity_matches AS MATERIALIZED",
		"runtime_service_matches AS MATERIALIZED",
		"runtime_environment_matches AS MATERIALIZED",
		"payload->'entity_keys'",
		"jsonb_typeof(payload->'entity_keys') IN ('array', 'string')",
		"jsonb_typeof(payload->'workload_id') = 'string'",
		"jsonb_typeof(payload->'service_id') = 'string'",
		"jsonb_typeof(payload->'environment') = 'string'",
		"jsonb_typeof(payload->'entity_keys') = 'string'",
		"LIKE 'workload:%'",
		"BTRIM(payload->>'workload_id')",
		"BTRIM(payload->>'service_id')",
		"BTRIM(payload->>'environment')",
		"BTRIM(COALESCE(payload->>'outcome', '')) IN ('', 'exact', 'derived')",
		"LOWER(BTRIM(COALESCE(payload->>'provenance_only', ''))) <> 'true'",
		"runtime_scope.active_generation_id = runtime_fact.generation_id",
		"runtime_generation.status = 'active'",
		"is_tombstone = FALSE",
	} {
		query := supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10", "$18", "$19")
		if !strings.Contains(query, want) {
			t.Fatalf("runtime filter CTE missing truth-rule marker %q:\n%s", want, query)
		}
	}
}

func TestSupplyChainImpactRuntimeFiltersApplyCallerGrantBeforeMembership(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		query             string
		repositoriesParam string
		scopesParam       string
	}{
		{
			name:              "legacy_list",
			query:             ListSupplyChainImpactFindingsQuery,
			repositoriesParam: "$22",
			scopesParam:       "$23",
		},
		{
			name:              "winners_list",
			query:             ListSupplyChainImpactFindingsFromWinnersQuery,
			repositoriesParam: "$22",
			scopesParam:       "$23",
		},
		{
			name:              "aggregate",
			query:             SupplyChainImpactAggregateCanonicalFactsCTE,
			repositoriesParam: "$18",
			scopesParam:       "$19",
		},
		{
			name:              "explain",
			query:             ExplainSupplyChainImpactFindingQuery,
			repositoriesParam: "$11",
			scopesParam:       "$12",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{
				"runtime_filter_repository_candidates.scope_id = ANY(" + tc.scopesParam + "::text[])",
				"runtime_filter_repository_candidates.repository_id = ANY(" + tc.repositoriesParam + "::text[])",
			} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("query missing runtime-filter authorization marker %q:\n%s", want, tc.query)
				}
			}
		})
	}
}

func TestSupplyChainImpactInventoryQueryPreservesRuntimeScopePatterns(t *testing.T) {
	t.Parallel()

	query := SupplyChainImpactInventoryQuery("fact.payload->>'impact_status'")
	for _, want := range []string{
		"SELECT fact.payload->>'impact_status' AS bucket",
		"LIKE 'repository:%'",
		"LIKE 'git-repository-scope:%'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("inventory query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "%!") {
		t.Fatalf("inventory query contains fmt corruption:\n%s", query)
	}
}

func TestSupplyChainImpactInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []SupplyChainImpactInventoryDimension{
		SupplyChainImpactInventoryByImpactStatus,
		SupplyChainImpactInventoryByPriorityBucket,
		SupplyChainImpactInventoryBySeverity,
		SupplyChainImpactInventoryByRepository,
		SupplyChainImpactInventoryByEcosystem,
	}
	for _, dim := range cases {
		if _, err := supplyChainImpactInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := supplyChainImpactInventoryGroupExpression("language"); err == nil {
		t.Fatal("supplyChainImpactInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
