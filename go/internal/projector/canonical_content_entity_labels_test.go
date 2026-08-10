// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "testing"

// TestContentEntityTypesReachTheGraph is the #5954 regression.
//
// content/shape and the collector twin both declare these five buckets, and
// content/shape's own tests exercise them as live parser output. They never
// reached the graph: extractEntities calls EntityTypeLabel and silently
// `continue`s when it returns false — no error, no dead letter, no counter — so
// the rows materialized into the content store and simply had no node.
//
// Both input spellings are asserted on purpose. EntityTypeLabel accepts a
// snake_case map key OR a PascalCase label (entityTypeLabelValues, built from
// the map's values at init), and the collector assigns EntityType from the
// bucket LABEL, so the PascalCase form is the one production actually takes.
// A snake_case-only entry would look right in the map and still drop every real
// row.
func TestContentEntityTypesReachTheGraph(t *testing.T) {
	t.Parallel()

	cases := []struct {
		entityType string
		wantLabel  string
	}{
		// snake_case keys, as spelled by query/entity_content_types.go — the
		// authority for these. Note cloudformation_import/export, NOT
		// cloudformation_cross_stack_import/export: the bucket names carry
		// "cross_stack" but the entity type does not, and keying off the bucket
		// name would leave the entry inert.
		{"terraform_block", "TerraformBlock"},
		{"cloudformation_condition", "CloudFormationCondition"},
		{"cloudformation_import", "CloudFormationImport"},
		{"cloudformation_export", "CloudFormationExport"},
		{"pagerduty_declaration", "PagerDutyDeclaration"},

		// PascalCase labels, the form the collector emits.
		{"TerraformBlock", "TerraformBlock"},
		{"CloudFormationCondition", "CloudFormationCondition"},
		{"CloudFormationImport", "CloudFormationImport"},
		{"CloudFormationExport", "CloudFormationExport"},
		{"PagerDutyDeclaration", "PagerDutyDeclaration"},
	}

	for _, tc := range cases {
		label, ok := EntityTypeLabel(tc.entityType)
		if !ok {
			t.Errorf("EntityTypeLabel(%q) not recognised: extractEntities drops this row silently, "+
				"so the content entity never becomes a graph node", tc.entityType)
			continue
		}
		if label != tc.wantLabel {
			t.Errorf("EntityTypeLabel(%q) = %q, want %q", tc.entityType, label, tc.wantLabel)
		}
	}
}
