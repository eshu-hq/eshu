// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"slices"
	"strings"
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

func TestRealSpecsCapabilityCatalogConsoleSurfaceSetsConsoleFlag(t *testing.T) {
	t.Parallel()

	catalog, _, err := BuildFromSpecs(repoSpecsDir(t), Signals{MCPTools: map[string]bool{"get_capability_catalog": true}})
	if err != nil {
		t.Fatalf("BuildFromSpecs: %v", err)
	}
	entry, ok := entryByCapability(catalog.Entries, "capability_catalog.list")
	if !ok {
		t.Fatal("capability_catalog.list missing from real specs")
	}
	if !entry.Console {
		t.Fatalf("Console = false with console surface: %+v", entry.Surfaces)
	}
	if !entryHasSurface(entry, "CapabilityMatrixPage") {
		t.Fatalf("CapabilityMatrixPage surface missing: %+v", entry.Surfaces)
	}
}

func TestRealSpecsOperationsStatusDeclaresConsoleSurface(t *testing.T) {
	t.Parallel()

	catalog, _, err := BuildFromSpecs(repoSpecsDir(t), Signals{})
	if err != nil {
		t.Fatalf("BuildFromSpecs: %v", err)
	}
	entry, ok := entryByCapability(catalog.Entries, "operations.status")
	if !ok {
		t.Fatal("operations.status missing from real specs")
	}
	if !entry.Console {
		t.Fatalf("operations.status Console = false, want true for the routed Operations page")
	}
}

func entryByCapability(entries []Entry, capability string) (Entry, bool) {
	for _, entry := range entries {
		if entry.Capability == capability {
			return entry, true
		}
	}
	return Entry{}, false
}

func entryHasSurface(entry Entry, tool string) bool {
	for _, surface := range entry.Surfaces {
		if surface.Tool == tool {
			return true
		}
	}
	return false
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

func TestRealSpecsResolveIssue2743ToolMappings(t *testing.T) {
	t.Parallel()

	matrix, err := LoadMatrix(repoSpecsDir(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	overlay, err := LoadOverlay(repoSpecsDir(t) + "/" + OverlayFileName)
	if err != nil {
		t.Fatalf("LoadOverlay: %v", err)
	}

	queryPlaybooks, ok := realSpecCapability(matrix, "query.playbooks")
	if !ok {
		t.Fatal("query.playbooks capability missing from real matrix")
	}
	wantTools := []string{"list_query_playbooks", "resolve_query_playbook"}
	for _, tool := range wantTools {
		if !slices.Contains(queryPlaybooks.Tools, tool) {
			t.Fatalf("query.playbooks tools = %v, want %q declared in matrix", queryPlaybooks.Tools, tool)
		}
		if exemption, ok := realSpecToolExemption(overlay, tool); ok {
			t.Fatalf("%q should be a matrix tool, not overlay exemption: %+v", tool, exemption)
		}
	}

	for _, exemption := range overlay.ToolExemptions {
		if exemption.Issue != 2743 {
			continue
		}
		t.Fatalf("%q still references #2743; completed follow-up exemptions need durable reasons", exemption.Tool)
	}

	for _, exemption := range overlay.ToolExemptions {
		reason := strings.ToLower(exemption.Reason)
		if strings.Contains(reason, "pending capability-matrix row") || strings.Contains(reason, "promote to matrix") {
			t.Fatalf("%q exemption reason is still temporary: %q", exemption.Tool, exemption.Reason)
		}
	}

	resolvedTools := []string{
		"get_repo_story",
		"get_repo_summary",
		"get_service_story",
		"get_workload_context",
		"get_workload_story",
		"investigate_service",
		"derive_visualization_packet",
		"visualize_graph_query",
		"list_query_playbooks",
		"resolve_query_playbook",
	}
	for _, tool := range resolvedTools {
		if realSpecToolDeclared(matrix, tool) {
			continue
		}
		exemption, ok := realSpecToolExemption(overlay, tool)
		if !ok {
			t.Fatalf("%q is neither matrix-declared nor durably exempted", tool)
		}
		if strings.TrimSpace(exemption.Reason) == "" {
			t.Fatalf("%q exemption missing durable reason", tool)
		}
	}
}

// TestNarrativeStoryToolsAreMatrixDeclared locks issue #3028: the narrative,
// story, report, and visualization MCP tools that make Eshu a context graph
// rather than a code-search tool must be first-class matrix surfaces, not
// overlay exemptions. Each tool must be declared by a capability the matrix
// owns and must no longer appear in the overlay tool_exemptions block.
func TestNarrativeStoryToolsAreMatrixDeclared(t *testing.T) {
	t.Parallel()

	matrix, err := LoadMatrix(repoSpecsDir(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	overlay, err := LoadOverlay(repoSpecsDir(t) + "/" + OverlayFileName)
	if err != nil {
		t.Fatalf("LoadOverlay: %v", err)
	}

	narrativeTools := []string{
		"get_repo_story",
		"get_repo_summary",
		"get_service_story",
		"get_service_intelligence_report",
		"get_workload_context",
		"get_workload_story",
		"investigate_service",
		"derive_visualization_packet",
		"visualize_graph_query",
	}
	for _, tool := range narrativeTools {
		tool := tool
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			if !realSpecToolDeclared(matrix, tool) {
				t.Fatalf("%q must be declared by a capability-matrix row, not exempted", tool)
			}
			if exemption, ok := realSpecToolExemption(overlay, tool); ok {
				t.Fatalf("%q must no longer be an overlay exemption: %+v", tool, exemption)
			}
		})
	}

	// The pure visualization-packet derivation tool has no runtime capability of
	// its own (it preserves the source response truth envelope), so issue #3028
	// gives it a dedicated catalog-only capability row.
	deriveCap, ok := realSpecCapability(matrix, "visualization.packet_derivation")
	if !ok {
		t.Fatal("visualization.packet_derivation capability missing from real matrix")
	}
	if !slices.Contains(deriveCap.Tools, "derive_visualization_packet") {
		t.Fatalf("visualization.packet_derivation tools = %v, want derive_visualization_packet", deriveCap.Tools)
	}
}

func realSpecCapability(matrix Matrix, id string) (MatrixCapability, bool) {
	for _, capability := range matrix.Capabilities {
		if capability.Capability == id {
			return capability, true
		}
	}
	return MatrixCapability{}, false
}

func realSpecToolExemption(overlay Overlay, tool string) (OverlayToolExemption, bool) {
	for _, exemption := range overlay.ToolExemptions {
		if exemption.Tool == tool {
			return exemption, true
		}
	}
	return OverlayToolExemption{}, false
}

func realSpecToolDeclared(matrix Matrix, tool string) bool {
	for _, capability := range matrix.Capabilities {
		if slices.Contains(capability.Tools, tool) {
			return true
		}
	}
	return false
}

// TestRealSpecsReachabilityRows pins the issue #3021 per-ecosystem reachability
// capability rows: their tools, the maturity each row derives or overrides to,
// the owning package, and the cross-linked doc. The reachability envelope is not
// a standalone tool, so every reachability row shares the supply-chain impact
// surfaces that actually carry it.
func TestRealSpecsReachabilityRows(t *testing.T) {
	t.Parallel()

	catalog, findings, err := BuildFromSpecs(repoSpecsDir(t), Signals{MCPTools: map[string]bool{
		"list_supply_chain_impact_findings": true,
		"explain_supply_chain_impact":       true,
	}})
	if err != nil {
		t.Fatalf("BuildFromSpecs: %v", err)
	}
	for _, finding := range findings {
		switch finding.Kind {
		case FindingInvalidOverlayMaturity, FindingMissingMaturityReason, FindingStaleOverlayCapability:
			if strings.HasPrefix(finding.Subject, "reachability.") {
				t.Fatalf("reachability overlay finding: %+v", finding)
			}
		}
	}

	byID := map[string]Entry{}
	for _, entry := range catalog.Entries {
		byID[entry.Capability] = entry
	}

	const reachabilityDoc = "reference/vulnerability-scanner-confidence.md"
	wantSurfaces := []string{"explain_supply_chain_impact", "list_supply_chain_impact_findings"}
	cases := []struct {
		id           string
		maturity     Maturity
		ownerPackage string
	}{
		{"reachability.go.govulncheck", MaturityGeneralAvailability, "internal/collector/vulnerabilityintelligence"},
		{"reachability.python.value_flow", MaturityGated, "internal/parser/python/pydataflow"},
		{"reachability.typescript.value_flow", MaturityGated, "internal/parser/javascript/jsdataflow"},
		{"reachability.javascript.value_flow", MaturityGated, "internal/parser/javascript/jsdataflow"},
		{"reachability.java.value_flow", MaturityGated, "internal/parser/java"},
		{"reachability.csharp.value_flow", MaturityGated, "internal/parser/csharp"},
		{"reachability.jvm.bounded", MaturityPreview, "internal/reducer"},
	}
	for _, tc := range cases {
		entry, ok := byID[tc.id]
		if !ok {
			t.Fatalf("capability %q missing from catalog", tc.id)
		}
		if entry.Maturity != tc.maturity {
			t.Errorf("%s maturity = %q, want %q", tc.id, entry.Maturity, tc.maturity)
		}
		if entry.OwnerPackage != tc.ownerPackage {
			t.Errorf("%s owner_package = %q, want %q", tc.id, entry.OwnerPackage, tc.ownerPackage)
		}
		if !slices.Contains(entry.Docs, reachabilityDoc) {
			t.Errorf("%s docs = %v, want to contain %q", tc.id, entry.Docs, reachabilityDoc)
		}
		if !slices.Contains(entry.LinkedIssues, 3021) {
			t.Errorf("%s linked_issues = %v, want to contain 3021", tc.id, entry.LinkedIssues)
		}
		surfaces := make([]string, 0, len(entry.Surfaces))
		for _, surface := range entry.Surfaces {
			if surface.Kind != SurfaceMCP {
				t.Errorf("%s surface %q kind = %q, want mcp", tc.id, surface.Tool, surface.Kind)
			}
			surfaces = append(surfaces, surface.Tool)
		}
		if !slices.Equal(surfaces, wantSurfaces) {
			t.Errorf("%s surfaces = %v, want %v", tc.id, surfaces, wantSurfaces)
		}
	}

	// Gated value_flow rows must be unsupported in every profile until the
	// operator opts in; the gate state cannot be expressed by the matrix.
	for _, id := range []string{"reachability.python.value_flow", "reachability.typescript.value_flow", "reachability.javascript.value_flow", "reachability.java.value_flow", "reachability.csharp.value_flow"} {
		entry := byID[id]
		if entry.DerivedMaturity != MaturityNotImplemented {
			t.Errorf("%s derived_maturity = %q, want not_implemented (gated off by default)", id, entry.DerivedMaturity)
		}
		if !strings.Contains(strings.ToUpper(entry.MaturityReason), "ESHU_EMIT_DATAFLOW") {
			t.Errorf("%s maturity_reason must name the gate env var, got %q", id, entry.MaturityReason)
		}
	}

	// Go reachability is always on and derives to GA from the matrix alone.
	if got := byID["reachability.go.govulncheck"].DerivedMaturity; got != MaturityGeneralAvailability {
		t.Errorf("reachability.go.govulncheck derived_maturity = %q, want general_availability", got)
	}
}
