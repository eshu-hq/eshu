// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"strings"
	"testing"
)

// TestSchemaPerformanceIndexesCoverTerraformStateResource proves the #5443
// P1 review finding's fix: TerraformStateResource joined every query path
// TerraformResource's property indexes back (infra_resource_aggregates.go's
// per-label aggregate branches, entity_map_resolver.go's terraform candidate
// lookups) with the SAME filter clauses TerraformResource's six sibling
// indexes serve, but only received a uid uniqueness constraint
// (uidConstraintLabels), leaving those TerraformStateResource query branches
// as unindexed full-label scans. Only resource_type and name are ever
// actually written onto a TerraformStateResource node
// (canonicalTerraformStateResourceUpsertCypher in
// internal/storage/cypher/tfstate_canonical_writer.go), so those are the
// only two of TerraformResource's six indexed properties that apply here --
// provider/environment/resource_service/resource_category are config-only
// concepts no TerraformStateResource node ever carries.
func TestSchemaPerformanceIndexesCoverTerraformStateResource(t *testing.T) {
	t.Parallel()

	want := []string{
		"CREATE INDEX tf_state_resource_type IF NOT EXISTS FOR (r:TerraformStateResource) ON (r.resource_type)",
		"CREATE INDEX tf_state_resource_name IF NOT EXISTS FOR (r:TerraformStateResource) ON (r.name)",
	}
	for _, stmt := range want {
		found := false
		for _, candidate := range schemaPerformanceIndexes {
			if candidate == stmt {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("schemaPerformanceIndexes missing %q", stmt)
		}
	}
}

// TestSchemaFulltextInfraSearchIndexIncludesTerraformStateResource proves the
// #5443 P1 review finding's fix: TerraformStateResource is in allInfraLabels
// (internal/query/infra.go) and infraCategoryLabels["terraform"], but the
// infra_search_index fulltext label set omitted it entirely -- fulltext infra
// searches silently missed every state-observed Terraform resource. Both the
// procedure-based primary form and the CREATE FULLTEXT INDEX fallback must
// carry the label, since EnsureSchema falls back to the second form when the
// first is unsupported (schema.go), and a label present in only one form
// would silently vanish from search on whichever backend takes that path.
func TestSchemaFulltextInfraSearchIndexIncludesTerraformStateResource(t *testing.T) {
	t.Parallel()

	var infraIndex *fulltextIndex
	for i := range schemaFulltextIndexes {
		if strings.Contains(schemaFulltextIndexes[i].primary, "infra_search_index") {
			infraIndex = &schemaFulltextIndexes[i]
			break
		}
	}
	if infraIndex == nil {
		t.Fatal("schemaFulltextIndexes has no infra_search_index entry")
	}
	if !strings.Contains(infraIndex.primary, "'TerraformStateResource'") {
		t.Errorf("infra_search_index primary form missing 'TerraformStateResource': %s", infraIndex.primary)
	}
	if !strings.Contains(infraIndex.fallback, "TerraformStateResource") {
		t.Errorf("infra_search_index fallback form missing TerraformStateResource: %s", infraIndex.fallback)
	}
}
