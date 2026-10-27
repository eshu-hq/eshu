// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

func TestDecodeFindingRowSelectedEPSSProvenance(t *testing.T) {
	t.Parallel()
	row, err := DecodeFindingRow("finding-1", "observed", []byte(`{
		"cve_id":"CVE-2026-0001",
		"epss_probability":"0.78",
		"provenance":{"selected_epss_score_date":"2026-05-16","selected_epss_model_version":"v4"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if row.Provenance == nil || row.Provenance.SelectedEPSSScoreDate != "2026-05-16" || row.Provenance.SelectedEPSSModelVersion != "v4" {
		t.Fatalf("provenance = %#v, want selected EPSS date and model version", row.Provenance)
	}
	result := BuildFindingResult(&row)
	if result.Provenance == nil || result.Provenance.SelectedEPSSScoreDate != "2026-05-16" || result.Provenance.SelectedEPSSModelVersion != "v4" {
		t.Fatalf("API provenance = %#v, want selected EPSS date and model version", result.Provenance)
	}
}
