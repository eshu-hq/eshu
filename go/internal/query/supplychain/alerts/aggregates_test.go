// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

func TestSecurityAlertReconciliationAggregateQueriesUseCurrentProviderAlertRows(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"total":     aggregateTotalQuery,
		"group":     aggregateGroupQueryTemplate,
		"inventory": inventoryQueryTemplate,
	} {
		query := query
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, want := range []string{
				"ROW_NUMBER() OVER (",
				"PARTITION BY",
				"security_alert_current_rank",
				"COALESCE(NULLIF(fact.payload->>'provider_alert_id', ''),",
				"COALESCE(NULLIF(fact.payload->>'provider_repository_id', ''),",
				"COALESCE(NULLIF(fact.payload->'cve_ids', 'null'::jsonb), '[]'::jsonb)",
				"COALESCE(NULLIF(fact.payload->'ghsa_ids', 'null'::jsonb), '[]'::jsonb)",
				"COALESCE(cardinality($1::text[]), 0) = 0",
				"fact.payload->>'repository_id' = ANY($1::text[])",
				"fact.payload->>'provider_repository_id' = ANY($1::text[])",
				"fact.payload->>'scope_id' = ANY($1::text[])",
				"COALESCE(cardinality($8::text[]), 0) = 0",
			} {
				if !strings.Contains(query, want) {
					t.Fatalf("%s aggregate query missing %q:\n%s", name, want, query)
				}
			}
			currentRank := strings.Index(query, "security_alert_current_rank = 1")
			if currentRank < 0 {
				t.Fatalf("%s aggregate query missing current-rank filter:\n%s", name, query)
			}
			for _, filter := range []string{
				"current_fact.payload->>'provider_state' = $6",
				"current_fact.payload->>'reconciliation_status' = $7",
			} {
				filterIndex := strings.Index(query, filter)
				if filterIndex < currentRank {
					t.Fatalf("%s aggregate filter %q must apply after current-rank selection:\n%s", name, filter, query)
				}
			}
		})
	}
}

func TestSecurityAlertReconciliationAggregateSourceFreshnessUsesCurrentFactAlias(t *testing.T) {
	t.Parallel()

	if strings.Contains(sourceFreshnessGroupExpr, "NULLIF(fact.payload") ||
		strings.Contains(sourceFreshnessGroupExpr, "WHEN fact.payload") {
		t.Fatalf("source freshness expression must use the current_fact alias after the CTE:\n%s", sourceFreshnessGroupExpr)
	}
	if !strings.Contains(sourceFreshnessGroupExpr, "current_fact.payload") {
		t.Fatalf("source freshness expression missing current_fact alias:\n%s", sourceFreshnessGroupExpr)
	}
}

func TestSecurityAlertReconciliationInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []supplychain.SecurityAlertReconciliationInventoryDimension{
		supplychain.SecurityAlertReconciliationInventoryByStatus,
		supplychain.SecurityAlertReconciliationInventoryByProvider,
		supplychain.SecurityAlertReconciliationInventoryByProviderState,
		supplychain.SecurityAlertReconciliationInventoryByRepository,
		supplychain.SecurityAlertReconciliationInventoryByPackage,
	}
	for _, dim := range cases {
		if _, err := inventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := inventoryGroupExpression("ecosystem"); err == nil {
		t.Fatal("inventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
