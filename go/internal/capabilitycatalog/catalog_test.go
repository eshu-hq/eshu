package capabilitycatalog

import (
	"testing"
)

func testMatrix() Matrix {
	return Matrix{Capabilities: []MatrixCapability{
		{
			Capability: "code_search.exact_symbol",
			Tools:      []string{"find_code"},
			Profiles: map[string]MatrixProfile{
				"local_lightweight": {Status: "supported", MaxTruthLevel: "exact"},
				"production":        {Status: "supported", MaxTruthLevel: "exact", Verification: []MatrixVerification{{Kind: "remote_validation", Ref: "prod-code-search-exact"}}},
			},
		},
		{
			Capability: "platform_impact.cloud_resource_list",
			Tools:      []string{"list_cloud_resources"},
			Profiles: map[string]MatrixProfile{
				"local_lightweight": {Status: "unsupported", MaxTruthLevel: "unsupported"},
				"production":        {Status: "supported", MaxTruthLevel: "exact"},
			},
		},
	}}
}

func TestBuildDerivesEntriesAndSurfaces(t *testing.T) {
	t.Parallel()

	overlay := Overlay{
		Version: "v1",
		Capabilities: []OverlayCapability{
			{
				Capability:   "platform_impact.cloud_resource_list",
				DisplayName:  "Cloud Resource List",
				OwnerPackage: "internal/query",
				Maturity:     MaturityGated,
				Reason:       "public chart support pending",
				KnownGaps:    []string{"no live provider"},
				LinkedIssues: []int{2700},
				Console:      true,
			},
		},
		NonMCPSurfaces: []OverlayNonMCPSurface{
			{Tool: "list_cloud_resources", Kind: SurfaceAPI, Reason: "API route, not an MCP tool"},
		},
	}
	signals := Signals{MCPTools: map[string]bool{"find_code": true}}

	catalog, findings := Build(testMatrix(), overlay, signals)
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	if len(catalog.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(catalog.Entries))
	}

	exact := catalog.Entries[0]
	if exact.Capability != "code_search.exact_symbol" {
		t.Fatalf("entries not sorted: %q", exact.Capability)
	}
	if exact.Maturity != MaturityGeneralAvailability || exact.DerivedMaturity != MaturityGeneralAvailability {
		t.Fatalf("exact maturity = %q/%q", exact.Maturity, exact.DerivedMaturity)
	}
	if exact.DisplayName != "Code Search Exact Symbol" {
		t.Fatalf("default display name = %q", exact.DisplayName)
	}
	if len(exact.Surfaces) != 1 || exact.Surfaces[0].Kind != SurfaceMCP {
		t.Fatalf("surfaces = %+v", exact.Surfaces)
	}
	if len(exact.ProofSignals) != 1 || exact.ProofSignals[0].Ref != "prod-code-search-exact" {
		t.Fatalf("proof = %+v", exact.ProofSignals)
	}

	cloud := catalog.Entries[1]
	if cloud.Maturity != MaturityGated || cloud.DerivedMaturity != MaturityGeneralAvailability {
		t.Fatalf("cloud maturity = %q/%q want gated/general_availability", cloud.Maturity, cloud.DerivedMaturity)
	}
	if cloud.MaturityReason != "public chart support pending" {
		t.Fatalf("cloud reason = %q", cloud.MaturityReason)
	}
	if len(cloud.Surfaces) != 1 || cloud.Surfaces[0].Kind != SurfaceAPI {
		t.Fatalf("cloud surfaces = %+v", cloud.Surfaces)
	}
	if len(cloud.LinkedIssues) != 1 || cloud.LinkedIssues[0] != 2700 {
		t.Fatalf("cloud issues = %v", cloud.LinkedIssues)
	}
}

func TestBuildFlagsOrphanAndUnmatched(t *testing.T) {
	t.Parallel()

	// find_code matched; orphan_tool present in registry but unmapped and not
	// exempt; list_cloud_resources declared by a capability but absent from the
	// registry and not declared as a non-MCP surface.
	signals := Signals{MCPTools: map[string]bool{"find_code": true, "orphan_tool": true}}
	_, findings := Build(testMatrix(), Overlay{}, signals)

	got := map[FindingKind]int{}
	for _, f := range findings {
		got[f.Kind]++
	}
	if got[FindingOrphanMCPTool] != 1 {
		t.Fatalf("orphan findings = %d, want 1 (%+v)", got[FindingOrphanMCPTool], findings)
	}
	if got[FindingUnmatchedSurface] != 1 {
		t.Fatalf("unmatched findings = %d, want 1 (%+v)", got[FindingUnmatchedSurface], findings)
	}
}

func TestBuildFlagsStaleOverlayAndMissingReason(t *testing.T) {
	t.Parallel()

	overlay := Overlay{
		Capabilities: []OverlayCapability{
			{Capability: "does.not.exist", DisplayName: "Ghost"},
			{Capability: "code_search.exact_symbol", Maturity: MaturityGated}, // no reason
		},
		ToolExemptions: []OverlayToolExemption{
			{Tool: "never_registered", Reason: "n/a"},
		},
	}
	signals := Signals{MCPTools: map[string]bool{"find_code": true, "list_cloud_resources": true}}
	_, findings := Build(testMatrix(), overlay, signals)

	got := map[FindingKind]int{}
	for _, f := range findings {
		got[f.Kind]++
	}
	if got[FindingStaleOverlayCapability] != 1 {
		t.Fatalf("stale overlay findings = %d (%+v)", got[FindingStaleOverlayCapability], findings)
	}
	if got[FindingMissingMaturityReason] != 1 {
		t.Fatalf("missing reason findings = %d (%+v)", got[FindingMissingMaturityReason], findings)
	}
	if got[FindingStaleToolExemption] != 1 {
		t.Fatalf("stale exemption findings = %d (%+v)", got[FindingStaleToolExemption], findings)
	}
}

func TestEffectiveStatusInfersFromTruthLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   MatrixProfile
		want string
	}{
		{"explicit status wins", MatrixProfile{Status: "supported", MaxTruthLevel: "unsupported"}, "supported"},
		{"empty status with truth is supported", MatrixProfile{MaxTruthLevel: "exact"}, "supported"},
		{"empty status unsupported truth", MatrixProfile{MaxTruthLevel: "unsupported"}, "unsupported"},
		{"empty status empty truth", MatrixProfile{}, "unsupported"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveStatus(tc.in); got != tc.want {
				t.Fatalf("effectiveStatus(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildFlagsInvalidOverlayMaturityWithoutReasonFinding(t *testing.T) {
	t.Parallel()

	overlay := Overlay{
		Capabilities: []OverlayCapability{
			// general_availability is matrix-derived, not an overlay-only state.
			{Capability: "code_search.exact_symbol", Maturity: MaturityGeneralAvailability},
		},
	}
	signals := Signals{MCPTools: map[string]bool{"find_code": true, "list_cloud_resources": true}}
	_, findings := Build(testMatrix(), overlay, signals)

	got := map[FindingKind]int{}
	for _, f := range findings {
		got[f.Kind]++
	}
	if got[FindingInvalidOverlayMaturity] != 1 {
		t.Fatalf("invalid maturity findings = %d (%+v)", got[FindingInvalidOverlayMaturity], findings)
	}
	if got[FindingMissingMaturityReason] != 0 {
		t.Fatalf("missing reason findings = %d, want 0 for invalid maturity (%+v)", got[FindingMissingMaturityReason], findings)
	}
}
