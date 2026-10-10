// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
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

func TestPilotRegistryDeclaresMeasuredWorkloadShape(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Entries {
		if entry.Contract == nil {
			continue
		}
		data, err := json.Marshal(entry.Contract.Workload)
		if err != nil {
			t.Fatal(err)
		}
		var workload struct {
			Selectivity           string `json:"selectivity"`
			MaxResultPayloadBytes int    `json:"max_result_payload_bytes"`
		}
		if err := json.Unmarshal(data, &workload); err != nil {
			t.Fatal(err)
		}
		if workload.Selectivity == "" || workload.MaxResultPayloadBytes <= 0 {
			t.Errorf("%s: selectivity and measured payload bytes required: %+v", entry.ID, workload)
		}
	}
}

func TestPilotEvidenceRejectsOversizeResultPayload(t *testing.T) {
	manifest, evidence := pilotEvidenceFixture()
	if err := ValidatePilotEvidence(manifest, &evidence); err != nil {
		t.Fatalf("valid fixture evidence: %v", err)
	}
	manifest.Entries[0].Contract.Workload.MaxResultPayloadBytes = 22
	evidence.Entries[0].ContractSHA256 = PilotContractSHA256(*manifest.Entries[0].Contract)
	if err := ValidatePilotEvidence(manifest, &evidence); err == nil || !strings.Contains(err.Error(), "exceeds declared") {
		t.Fatalf("oversize result payload accepted: %v", err)
	}
}

func TestPilotWorkloadRequiredFieldsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PilotWorkload)
		want   string
	}{
		{"missing selectivity", func(workload *PilotWorkload) { workload.Selectivity = "" }, "selectivity"},
		{"missing payload size", func(workload *PilotWorkload) { workload.MaxResultPayloadBytes = 0 }, "max_result_payload_bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, _ := pilotEvidenceFixture()
			tc.change(&manifest.Entries[0].Contract.Workload)
			if err := ValidatePilotContracts(manifest); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid workload accepted: %v", err)
			}
		})
	}
}

func TestPilotRegistryDesignPatternsAreFamilySpecific(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, entry := range manifest.Entries {
		if entry.Contract == nil {
			continue
		}
		pattern := strings.Join(entry.Contract.Patterns, "|") + "|" + entry.Contract.Rationale
		if earlier, ok := seen[pattern]; ok {
			t.Errorf("%s reuses %s design pattern and rationale", entry.ID, earlier)
		}
		seen[pattern] = entry.ID
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
