// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestGoldenSnapshotPinsSelectedEPSSObservation(t *testing.T) {
	t.Parallel()
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	committed := snapshot.QueryShapes.MCP["list_supply_chain_impact_findings"]
	if committed.Arguments["cve_id"] != "CVE-2026-00010" || committed.MinimumResults != 1 || committed.MaximumResults != 1 {
		t.Fatalf("EPSS finding shape is not bound to the one CVE finding: %+v", committed)
	}
	want := map[string]any{
		"findings[].provenance.selected_epss_score_date": "2026-06-24",
		"findings[].epss_probability":                    "0.87345",
		"findings[].epss_percentile":                     "0.99012",
	}
	for path, value := range want {
		if got := committed.RequiredJSONValues[path]; got != value {
			t.Errorf("committed EPSS pin %q = %#v, want %#v", path, got, value)
		}
	}
	shape := QueryShape{
		RequiredResponseFields: []string{"findings"},
		ResultsField:           "findings",
		MinimumResults:         1,
		MaximumResults:         1,
		RequiredJSONValues:     want,
	}
	for _, tc := range []struct {
		name   string
		body   string
		wantOK bool
	}{
		{"selected observation", `{"findings":[{"epss_probability":"0.87345","epss_percentile":"0.99012","provenance":{"selected_epss_score_date":"2026-06-24"}}]}`, true},
		{"older observation", `{"findings":[{"epss_probability":"0.12","epss_percentile":"0.32","provenance":{"selected_epss_score_date":"2026-06-23"}}]}`, false},
		{"wrong date, same score", `{"findings":[{"epss_probability":"0.87345","epss_percentile":"0.99012","provenance":{"selected_epss_score_date":"2026-06-23"}}]}`, false},
		{"missing date", `{"findings":[{"epss_probability":"0.87345","epss_percentile":"0.99012"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finding := EvaluateQueryShape("list_supply_chain_impact_findings", shape, []byte(tc.body))
			if finding.OK != tc.wantOK {
				t.Fatalf("query shape OK = %t, want %t: %s", finding.OK, tc.wantOK, finding.Detail)
			}
		})
	}
}
