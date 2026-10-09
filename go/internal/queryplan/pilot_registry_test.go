// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"slices"
	"testing"
)

func TestPilotRegistryMatchesProductionFamilies(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"QP-CLOUD-RESOURCE-IDENTITY-PAGE",
		"QP-CODE-IMPORT-CROSS-MODULE-CALLS",
		"QP-CODE-IMPORT-CYCLE-EDGES",
		"QP-CODE-IMPORT-PACKAGES",
		"QP-CODE-IMPORT-ROWS-REPOSITORY",
		"QP-CODE-IMPORT-SOURCE-MODULE-FILES",
		"QP-CODE-IMPORT-SOURCE-MODULE-ROWS",
		"QP-CODE-IMPORT-TARGET-MODULE-FILES",
	}
	got := slices.Clone(manifest.PilotRequiredIDs)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("pilot membership = %v, want %v", got, want)
	}
	if err := ValidatePilotContracts(manifest); err != nil {
		t.Fatalf("pilot contracts: %v", err)
	}
	cypher, sql := make(map[string]struct{}), make(map[string]struct{})
	for _, id := range want {
		entry, ok := pilotEntry(manifest, id)
		if !ok || entry.Contract == nil {
			t.Fatalf("required pilot entry %s missing", id)
		}
		for _, candidate := range entry.Contract.RequiredCases {
			if entry.QueryKind == queryKindSQLReadModel {
				sql[candidate.VariantID] = struct{}{}
			} else {
				cypher[candidate.VariantID] = struct{}{}
			}
		}
	}
	if len(sql) != 64 || len(cypher) != 280 {
		t.Fatalf("pilot variants: SQL %d/64, Cypher %d/280", len(sql), len(cypher))
	}
}
