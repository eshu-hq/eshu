// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import "testing"

// stubResolver resolves any ref present in its set; everything else is missing.
type stubResolver struct{ present map[string]bool }

func (s stubResolver) Resolve(e CoverageEntry) (bool, string) {
	if s.present[e.Ref] {
		return true, "present"
	}
	return false, "missing"
}

func reconcileFixture() Coverage {
	supported := []SupportedSurface{
		{Registry: RegistrySurfaceInventory, Key: "collector:aws"},  // covered (artifact present)
		{Registry: RegistrySurfaceInventory, Key: "collector:git"},  // covered entry but artifact missing -> unresolved
		{Registry: RegistryParserLedger, Key: "parser:hcl"},         // uncovered (no manifest entry)
		{Registry: RegistryCapabilityMatrix, Key: "capability:c.x"}, // exempt
	}
	m := Manifest{
		Coverage: []CoverageEntry{
			{Surface: "collector:aws", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "present-ref"},
			{Surface: "collector:git", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "missing-ref"},
			{Surface: "collector:ghost", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "present-ref"}, // stale: no such surface
		},
		Exemptions: []Exemption{
			{Surface: "capability:c.x", Reason: "design-only"},
			{Surface: "parser:ghost", Reason: "no such parser"}, // stale exemption
		},
	}
	r := stubResolver{present: map[string]bool{"present-ref": true}}
	return Reconcile(supported, m, r)
}

func depthReconcileFixture() Coverage {
	supported := []SupportedSurface{
		{Registry: RegistrySurfaceInventory, Key: "collector:aws"},
		{Registry: RegistrySurfaceInventory, Key: "collector:git"},
	}
	m := Manifest{
		Coverage: []CoverageEntry{
			{Surface: "collector:aws", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "present-ref"},
			{Surface: "collector:git", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeBaseline, Ref: "present-ref"},
			{Surface: "collector:git", Scenario: ScenarioCassette, ScenarioType: ScenarioTypeFault, Ref: "missing-ref"},
		},
		Requirements: []ScenarioRequirement{
			{Surface: "collector:aws", ScenarioTypes: []DepthScenarioType{ScenarioTypeBaseline, ScenarioTypeFault}},
			{Surface: "collector:git", ScenarioTypes: []DepthScenarioType{ScenarioTypeBaseline, ScenarioTypeFault}},
		},
	}
	r := stubResolver{present: map[string]bool{"present-ref": true}}
	return Reconcile(supported, m, r)
}

func TestReconcileStatuses(t *testing.T) {
	c := reconcileFixture()
	got := map[string]Status{}
	for _, sc := range c.Surfaces {
		got[sc.Surface.Key] = sc.Status
	}
	want := map[string]Status{
		"collector:aws":  StatusCovered,
		"collector:git":  StatusUnresolved,
		"parser:hcl":     StatusUncovered,
		"capability:c.x": StatusExempt,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%q status = %q, want %q", k, got[k], w)
		}
	}
}

func TestReconcileReportsStaleManifestEntries(t *testing.T) {
	c := reconcileFixture()
	staleSet := map[string]bool{}
	for _, s := range c.Stale {
		staleSet[s] = true
	}
	for _, want := range []string{"collector:ghost", "parser:ghost"} {
		if !staleSet[want] {
			t.Errorf("expected stale manifest surface %q, stale=%v", want, c.Stale)
		}
	}
	if len(c.Stale) != 2 {
		t.Errorf("stale count = %d, want 2", len(c.Stale))
	}
}

func TestFindingsAdvisoryNeverBlocks(t *testing.T) {
	c := reconcileFixture()
	findings := Findings(c, false)
	for _, f := range findings {
		if f.Required {
			t.Errorf("advisory mode produced a required finding: %+v", f)
		}
	}
	// Covered and exempt surfaces are OK; uncovered/unresolved/stale are not.
	okByCheck := map[string]bool{}
	for _, f := range findings {
		okByCheck[f.Check] = f.OK
	}
	if !okByCheck["collector:aws"] || !okByCheck["capability:c.x"] {
		t.Error("covered/exempt surfaces should be OK findings")
	}
	if okByCheck["collector:git"] || okByCheck["parser:hcl"] {
		t.Error("unresolved/uncovered surfaces should be non-OK findings")
	}
}

func TestFindingsBlockingFailsOnGaps(t *testing.T) {
	c := reconcileFixture()
	findings := Findings(c, true)
	var requiredFail int
	for _, f := range findings {
		if f.Required && !f.OK {
			requiredFail++
		}
	}
	// 1 unresolved + 1 uncovered + 2 stale = 4 required failures in blocking mode.
	if requiredFail != 4 {
		t.Errorf("blocking required-fail count = %d, want 4", requiredFail)
	}
}

func TestReconcileReportsMissingRequiredDepthScenarioType(t *testing.T) {
	c := depthReconcileFixture()
	var found bool
	for _, sc := range c.Surfaces {
		if sc.Surface.Key != "collector:aws" || sc.ScenarioType != ScenarioTypeFault {
			continue
		}
		found = true
		if sc.Status != StatusUncovered {
			t.Errorf("collector:aws fault status = %q, want %q", sc.Status, StatusUncovered)
		}
		if sc.Detail != "no replay scenario mapped for required scenario_type fault" {
			t.Errorf("collector:aws fault detail = %q", sc.Detail)
		}
	}
	if !found {
		t.Fatal("missing collector:aws fault requirement row")
	}
}

func TestReconcileReportsUnresolvedDepthScenarioType(t *testing.T) {
	c := depthReconcileFixture()
	var found bool
	for _, sc := range c.Surfaces {
		if sc.Surface.Key != "collector:git" || sc.ScenarioType != ScenarioTypeFault {
			continue
		}
		found = true
		if sc.Status != StatusUnresolved {
			t.Errorf("collector:git fault status = %q, want %q", sc.Status, StatusUnresolved)
		}
	}
	if !found {
		t.Fatal("missing collector:git fault requirement row")
	}
}
