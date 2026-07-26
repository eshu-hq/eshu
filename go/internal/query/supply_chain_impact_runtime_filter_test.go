// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

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
			query:          listSupplyChainImpactFindingsQuery,
			repositoryExpr: "fact.payload->>'repository_id'",
		},
		{
			name:           "winners_list",
			query:          listSupplyChainImpactFindingsFromWinnersQuery,
			repositoryExpr: "w.repository_id",
		},
		{
			name:           "aggregate",
			query:          supplyChainImpactAggregateCanonicalFactsCTE,
			repositoryExpr: "fact.payload->>'repository_id'",
		},
		{
			name:           "explain",
			query:          explainSupplyChainImpactFindingQuery,
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

func TestSupplyChainImpactRuntimeFilterMirrorsRuntimeContextTruthRules(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"runtime_fact.payload->'entity_keys'",
		"LIKE 'workload:%'",
		"runtime_fact.payload->>'workload_id'",
		"runtime_fact.payload->>'service_id'",
		"runtime_fact.payload->>'environment'",
		"BTRIM(COALESCE(runtime_fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')",
		"COALESCE(runtime_fact.payload->'provenance_only', 'false'::jsonb) <> 'true'::jsonb",
		"runtime_scope.active_generation_id = runtime_fact.generation_id",
		"runtime_generation.status = 'active'",
		"runtime_fact.is_tombstone = FALSE",
	} {
		if !strings.Contains(supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10"), want) {
			t.Fatalf("runtime filter CTE missing truth-rule marker %q:\n%s", want, supplyChainImpactRuntimeFilterCTE("$8", "$9", "$10"))
		}
	}
}

func TestSupplyChainImpactInventoryQueryPreservesRuntimeScopePatterns(t *testing.T) {
	t.Parallel()

	query := supplyChainImpactInventoryQuery("fact.payload->>'impact_status'")
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
