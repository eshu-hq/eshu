// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"os"
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

func TestPilotPostgresRegistryMatchesSource(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := DiscoverPostgresCallsites("../query")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePostgresCoverage(manifest, discovered); err != nil {
		t.Fatal(err)
	}
	if len(manifest.PilotPostgresFiles) != 1 {
		t.Fatal("PostgreSQL pilot scope changed without required coverage")
	}
}

func TestPilotCoverageMatrixArtifact(t *testing.T) {
	path := os.Getenv("ESHU_QUERY_METHODOLOGY_COVERAGE")
	if path == "" {
		t.Skip("runner supplies coverage artifact path")
	}
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := DiscoverPostgresCallsites("../query")
	if err != nil {
		t.Fatal(err)
	}
	type family struct {
		EntryID  string    `json:"entry_id"`
		Backend  string    `json:"backend"`
		Status   string    `json:"status"`
		Variants int       `json:"variants"`
		Cases    int       `json:"cases"`
		Source   SourceRef `json:"source"`
	}
	rows := make([]family, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		row := family{EntryID: entry.ID, Backend: entry.Backend, Status: "legacy", Source: entry.Source}
		if entry.Contract != nil {
			row.Backend, row.Status = entry.Contract.Environment.Engine, "pilot"
			names := make(map[string]bool)
			for _, candidate := range entry.Contract.RequiredCases {
				names[candidate.VariantID] = true
			}
			row.Variants, row.Cases = len(names), len(entry.Contract.RequiredCases)
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b family) int {
		if a.EntryID < b.EntryID {
			return -1
		}
		if a.EntryID > b.EntryID {
			return 1
		}
		return 0
	})
	data, err := json.MarshalIndent(struct {
		Version  int                   `json:"version"`
		Families []family              `json:"families"`
		Postgres []PostgresCoverageRow `json:"postgres_execution_candidates"`
	}{Version: 1, Families: rows, Postgres: PostgresCoverageMatrix(manifest, discovered)}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
