// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestAtlantisEntityTypesResolveConsistentlyAcrossGraphAndContent proves the
// #5369 entity-lookup registration: AtlantisProject/AtlantisWorkflow are
// content-backed (materialize_tables.go registers them under the
// terraform_backend shape), so "atlantis_project"/"atlantis_workflow" must
// resolve to the same PascalCase label whether the caller reaches the graph
// first (resolveGraphEntityType, backed by graphFirstContentBackedEntityTypes)
// or the content-store name-search fallback
// (contentEntityTypeForResolve, backed by resolveContentBackedEntityTypes).
//
// terragrunt_config is the cautionary example this test guards against
// repeating: it is registered only in graphFirstContentBackedEntityTypes, so
// contentEntityTypeForResolve("terragrunt_config") falls through every map
// and returns the raw snake_case string instead of "TerragruntConfig" -- a
// label mismatch between the two resolution paths. Registering atlantis
// entities in both maps (not just graphFirstContentBackedEntityTypes) avoids
// that mismatch.
func TestAtlantisEntityTypesResolveConsistentlyAcrossGraphAndContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeName string
		want     string
	}{
		{typeName: "atlantis_project", want: "AtlantisProject"},
		{typeName: "atlantis_workflow", want: "AtlantisWorkflow"},
	}

	for _, tt := range tests {
		graphLabel, _, _, ok := resolveGraphEntityType(tt.typeName)
		if !ok {
			t.Fatalf("resolveGraphEntityType(%q) not found, want ok=true", tt.typeName)
		}
		if graphLabel != tt.want {
			t.Fatalf("resolveGraphEntityType(%q) = %q, want %q", tt.typeName, graphLabel, tt.want)
		}

		if got := contentEntityTypeForResolve(tt.typeName); got != tt.want {
			t.Fatalf("contentEntityTypeForResolve(%q) = %q, want %q (must match resolveGraphEntityType so graph-first and content-fallback paths agree)", tt.typeName, got, tt.want)
		}
	}
}

// TestTerragruntConfigDemonstratesTheGraphFirstOnlyTrap documents (rather than
// endorses) the pre-existing asymmetric registration that #5369's atlantis
// entries deliberately avoid: terragrunt_config is graphFirst-only, so its
// content-resolve path silently returns the raw type name instead of the
// PascalCase graph label. This is a regression guard: if a future change
// "fixes" this by adding terragrunt_config to resolveContentBackedEntityTypes,
// this test should be updated (not deleted) to assert the corrected mapping.
func TestTerragruntConfigDemonstratesTheGraphFirstOnlyTrap(t *testing.T) {
	t.Parallel()

	graphLabel, _, _, ok := resolveGraphEntityType("terragrunt_config")
	if !ok || graphLabel != "TerragruntConfig" {
		t.Fatalf("resolveGraphEntityType(%q) = (%q, %v), want (%q, true)", "terragrunt_config", graphLabel, ok, "TerragruntConfig")
	}
	if got := contentEntityTypeForResolve("terragrunt_config"); got != "terragrunt_config" {
		t.Fatalf("contentEntityTypeForResolve(%q) = %q, want raw fallthrough %q (documents the asymmetry, not a claim it is correct)", "terragrunt_config", got, "terragrunt_config")
	}
}
