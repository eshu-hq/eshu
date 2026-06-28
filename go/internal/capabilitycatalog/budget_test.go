// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import "testing"

func TestBuildCarriesProfileBudgetsIntoCatalog(t *testing.T) {
	t.Parallel()

	p95 := 800
	matrix := Matrix{Capabilities: []MatrixCapability{
		{
			Capability: "code_search.exact_symbol",
			Tools:      []string{"find_code"},
			Profiles: map[string]MatrixProfile{
				"production": {
					Status:          "supported",
					MaxTruthLevel:   "exact",
					RequiredRuntime: "deployed_services",
					P95LatencyMS:    &p95,
					MaxScopeSize:    "multi_repo_platform",
				},
			},
		},
	}}

	catalog, findings := Build(matrix, Overlay{}, Signals{MCPTools: map[string]bool{"find_code": true}})
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}

	profile := catalog.Entries[0].Profiles["production"]
	if profile.P95LatencyMS == nil || *profile.P95LatencyMS != 800 {
		t.Fatalf("catalog production p95 latency = %v, want 800", profile.P95LatencyMS)
	}
	if profile.MaxScopeSize != "multi_repo_platform" {
		t.Fatalf("catalog production max scope = %q, want multi_repo_platform", profile.MaxScopeSize)
	}
}
