// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestIaCInventorySQLReadsOnlyActiveCurrentFacts(t *testing.T) {
	t.Parallel()

	for _, query := range []string{iacInventorySearchSQL, iacInventorySummarySQL} {
		for _, want := range []string{
			"generation.status = 'active'",
			"fact.fact_kind = 'content_entity'",
			"fact.is_tombstone = FALSE",
			"TerraformResource",
			"TerraformModule",
			"TerraformDataSource",
			"fact.payload->>'repo_id' = ANY",
			"fact.scope_id = ANY",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("query missing %q:\n%s", want, query)
			}
		}
	}
}

func TestIaCInventorySearchSQLIsServerBackedBoundedAndStable(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"current_iac.generation_id",
		"strpos(lower(current_iac.entity_name), lower($6)) > 0",
		"strpos(lower(current_iac.relative_path), lower($6)) > 0",
		"strpos(lower(current_iac.item_type), lower($6)) > 0",
		"strpos(lower(current_iac.provider), lower($6)) > 0",
		"strpos(lower(current_iac.module_name), lower($6)) > 0",
		"strpos(lower(current_iac.repo_id), lower($6)) > 0",
		"strpos(lower(current_iac.entity_type), lower($6)) > 0",
		"ORDER BY current_iac.entity_name, current_iac.entity_id",
		"LIMIT $13",
	} {
		if !strings.Contains(iacInventorySearchSQL, want) {
			t.Fatalf("search SQL missing %q:\n%s", want, iacInventorySearchSQL)
		}
	}
	if strings.Contains(iacInventorySearchSQL, "LIKE '%' || lower($6)") {
		t.Fatalf("search SQL interprets user search as LIKE wildcards:\n%s", iacInventorySearchSQL)
	}
}

func TestIaCInventorySummarySQLBoundsEachFacet(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ROW_NUMBER() OVER (PARTITION BY dimension, kind",
		"facet_rank <= $4 + 1",
		"dimension IN ('kind', 'type', 'provider', 'module', 'repository')",
	} {
		if !strings.Contains(iacInventorySummarySQL, want) {
			t.Fatalf("summary SQL missing %q:\n%s", want, iacInventorySummarySQL)
		}
	}
}

func TestIaCInventoryFacetTruncationKeysUseWireNames(t *testing.T) {
	t.Parallel()

	for dimension, want := range map[string]string{
		"type":       "types",
		"provider":   "providers",
		"module":     "modules",
		"repository": "repositories",
	} {
		if got := iacFacetTruncationKey(dimension); got != want {
			t.Fatalf("iacFacetTruncationKey(%q) = %q, want %q", dimension, got, want)
		}
	}
}
