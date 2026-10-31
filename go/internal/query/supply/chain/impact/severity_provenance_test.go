// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

// TestDecodeFindingRowSeverityScoreSource pins that a vector-derived score
// reaches the API marked as derived, on the selected severity and on an
// alternate (#9693), and that a source-reported score carries no marker.
func TestDecodeFindingRowSeverityScoreSource(t *testing.T) {
	t.Parallel()
	row, err := DecodeFindingRow("finding-1", "observed", []byte(`{
		"cve_id":"CVE-2026-0001",
		"cvss_score":9.8,
		"provenance":{
			"selected_severity_source":"ghsa",
			"selected_severity_score":9.8,
			"selected_severity_score_source":"derived_from_vector",
			"selected_severity_vector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
			"alternate_severities":[
				{"source":"nvd","score":7.5,"score_source":"derived_from_vector","vector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N"},
				{"source":"osv","score":5.0}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeFindingRow() error = %v", err)
	}
	if row.Provenance == nil {
		t.Fatal("Provenance = nil, want decoded provenance")
	}
	if got := row.Provenance.SelectedSeverityScoreSource; got != "derived_from_vector" {
		t.Errorf("SelectedSeverityScoreSource = %q, want derived_from_vector", got)
	}
	alternates := row.Provenance.AlternateSeverities
	if len(alternates) != 2 || alternates[0].ScoreSource != "derived_from_vector" || alternates[1].ScoreSource != "" {
		t.Errorf("AlternateSeverities = %+v, want the first marked derived and the second unmarked", alternates)
	}
}

func TestDecodeFindingRowLabelOnlyProvenanceIsNotEmpty(t *testing.T) {
	t.Parallel()
	row, err := DecodeFindingRow("finding-2", "observed", []byte(`{
		"cve_id":"CVE-2026-0002",
		"provenance":{"selected_severity_source":"ghsa","selected_severity_label":"MODERATE"}
	}`))
	if err != nil {
		t.Fatalf("DecodeFindingRow() error = %v", err)
	}
	if row.Provenance == nil || row.Provenance.SelectedSeverityLabel != "MODERATE" {
		t.Fatalf("Provenance = %+v, want the label-only severity kept", row.Provenance)
	}
}
