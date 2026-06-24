// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"path/filepath"
	"testing"
)

func freshnessCatalog() Catalog {
	return Catalog{Entries: []Entry{
		{Capability: "component_extensions.diagnostics", Maturity: MaturityGeneralAvailability},
		{Capability: "platform_impact.cloud_resource_list", Maturity: MaturityGated, MaturityReason: "chart pending"},
	}}
}

func TestParseDocClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# Title\n\nProse.\n<!-- capability-state: id=component_extensions.diagnostics state=ga -->\nmore\n<!-- capability-state: id=platform_impact.cloud_resource_list state=gated issue=2700 -->\n")
	writeFile(t, filepath.Join(dir, "nested", "b.md"), "<!-- capability-state: id=code_search.exact_symbol state=general_availability -->\n")
	writeFile(t, filepath.Join(dir, "ignore.txt"), "<!-- capability-state: id=should.not.parse state=ga -->\n")

	claims, err := ParseDocClaims(dir)
	if err != nil {
		t.Fatalf("ParseDocClaims: %v", err)
	}
	if len(claims) != 3 {
		t.Fatalf("claims = %d, want 3: %+v", len(claims), claims)
	}
	// Deterministic order: by path then line.
	if claims[0].Capability != "component_extensions.diagnostics" || claims[0].State != MaturityGeneralAvailability {
		t.Fatalf("claim0 = %+v", claims[0])
	}
	if claims[1].Capability != "platform_impact.cloud_resource_list" || claims[1].Issue != 2700 {
		t.Fatalf("claim1 = %+v", claims[1])
	}
	if claims[2].Path != filepath.Join("nested", "b.md") {
		t.Fatalf("claim2 path = %q", claims[2].Path)
	}
}

func TestParseDocClaimsSkipsCodeFences(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "example.md"),
		"# Example\n\n"+
			"```markdown\n"+
			"<!-- capability-state: id=hypothetical.future state=preview -->\n"+
			"```\n\n"+
			"<!-- capability-state: id=real.capability state=ga -->\n"+
			"~~~\n"+
			"<!-- capability-state: id=fenced.tilde state=gated -->\n"+
			"~~~\n")

	claims, err := ParseDocClaims(dir)
	if err != nil {
		t.Fatalf("ParseDocClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1 (fenced markers must be skipped): %+v", len(claims), claims)
	}
	if claims[0].Capability != "real.capability" {
		t.Fatalf("claim = %+v, want real.capability", claims[0])
	}
}

func TestMalformedMarkerIsReportedNotDropped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// One marker missing state, one missing id — both must surface as findings,
	// not be silently dropped (a typo must not bypass the gate).
	writeFile(t, filepath.Join(dir, "bad.md"),
		"<!-- capability-state: id=component_extensions.diagnostics -->\n"+
			"<!-- capability-state: state=ga -->\n")

	claims, err := ParseDocClaims(dir)
	if err != nil {
		t.Fatalf("ParseDocClaims: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2 (malformed markers retained): %+v", len(claims), claims)
	}
	for _, claim := range claims {
		if !claim.Malformed {
			t.Fatalf("claim should be malformed: %+v", claim)
		}
	}

	findings := CheckDocFreshness(freshnessCatalog(), claims)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Reason != "malformed capability-state marker: missing id or state" {
			t.Fatalf("unexpected reason: %q", f.Reason)
		}
	}
}

func TestCheckDocFreshness(t *testing.T) {
	t.Parallel()

	catalog := freshnessCatalog()
	claims := []DocClaim{
		{Path: "ok.md", Line: 1, Capability: "component_extensions.diagnostics", State: MaturityGeneralAvailability},
		{Path: "gated.md", Line: 1, Capability: "platform_impact.cloud_resource_list", State: MaturityGated},
		{Path: "stale-missing.md", Line: 2, Capability: "component_extensions.diagnostics", State: MaturityNotImplemented},
		{Path: "over-claim.md", Line: 3, Capability: "platform_impact.cloud_resource_list", State: MaturityGeneralAvailability},
		{Path: "unknown.md", Line: 4, Capability: "does.not.exist", State: MaturityGeneralAvailability},
		{Path: "bad-state.md", Line: 5, Capability: "component_extensions.diagnostics", State: Maturity("totally-bogus")},
	}

	findings := CheckDocFreshness(catalog, claims)
	byPath := map[string]DocFinding{}
	for _, f := range findings {
		byPath[f.Path] = f
	}
	if _, ok := byPath["ok.md"]; ok {
		t.Fatal("ok.md should not produce a finding")
	}
	if _, ok := byPath["gated.md"]; ok {
		t.Fatal("gated.md (accurate) should not produce a finding")
	}
	if f := byPath["stale-missing.md"]; f.Expected != MaturityGeneralAvailability || f.Claimed != MaturityNotImplemented {
		t.Fatalf("stale-missing finding = %+v", f)
	}
	if f := byPath["over-claim.md"]; f.Expected != MaturityGated {
		t.Fatalf("over-claim finding = %+v", f)
	}
	if f, ok := byPath["unknown.md"]; !ok || f.Reason == "" {
		t.Fatalf("unknown.md finding missing: %+v", f)
	}
	if _, ok := byPath["bad-state.md"]; !ok {
		t.Fatal("bad-state.md should produce a finding")
	}
}
