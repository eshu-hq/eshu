// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func writeDepthSpec(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), DepthRequirementsFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDepthRequirementsValid(t *testing.T) {
	dr, err := LoadDepthRequirements(writeDepthSpec(t, `version: "v1"
retractable_node_types: [Function, Class]
retractable_edge_types: [CALLS, REFERENCES]
reducer_drain: {surface: reducer-projection-drain, detail: the drain}
exemptions:
  - {surface: "retractable_node:Class", scenario_type: delta_tombstone, reason: structural}
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(dr.RetractableNodeTypes); got != 2 {
		t.Fatalf("node types = %d, want 2", got)
	}
	if got := len(dr.RetractableEdgeTypes); got != 2 {
		t.Fatalf("edge types = %d, want 2", got)
	}
	if dr.ReducerDrain.Surface != "reducer-projection-drain" {
		t.Errorf("drain surface = %q", dr.ReducerDrain.Surface)
	}
	if len(dr.Exemptions) != 1 || dr.Exemptions[0].ScenarioType != ScenarioTypeDeltaTombstone {
		t.Errorf("exemptions = %+v", dr.Exemptions)
	}
}

func TestLoadDepthRequirementsRejects(t *testing.T) {
	cases := map[string]string{
		"blank node type":        "version: \"v1\"\nretractable_node_types: [\"\"]\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: d}\n",
		"duplicate node type":    "version: \"v1\"\nretractable_node_types: [Function, Function]\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: d}\n",
		"no node types":          "version: \"v1\"\nretractable_node_types: []\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: d}\n",
		"blank edge type":        "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: [\"\"]\nreducer_drain: {surface: d}\n",
		"duplicate edge type":    "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: [CALLS, CALLS]\nreducer_drain: {surface: d}\n",
		"no edge types":          "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: []\nreducer_drain: {surface: d}\n",
		"blank drain":            "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: \"\"}\n",
		"bad exemption type":     "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: d}\nexemptions: [{surface: x, scenario_type: bogus, reason: r}]\n",
		"blank exemption reason": "version: \"v1\"\nretractable_node_types: [Function]\nretractable_edge_types: [CALLS]\nreducer_drain: {surface: d}\nexemptions: [{surface: x, scenario_type: cost, reason: \"\"}]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadDepthRequirements(writeDepthSpec(t, body)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestLoadDepthRequirementsMissingFileIsError(t *testing.T) {
	// A missing depth spec must fail loudly: silently dropping every depth
	// requirement is the #4186 blindness this gate removes.
	if _, err := LoadDepthRequirements(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("missing depth spec must be an error, not an empty (silent) requirement set")
	}
}

// factKind is a terse constructor for the registry fields the derivation reads.
func factKind(domain, hook string) facts.FactKindRegistryEntry {
	return facts.FactKindRegistryEntry{ReducerDomain: domain, ProjectionHook: hook}
}

func TestSharedConflictKeyProjections(t *testing.T) {
	fks := []facts.FactKindRegistryEntry{
		factKind("solo", "solo_hook"), // 1 hook -> not shared
		factKind("solo", "solo_hook"), // duplicate hook, still 1 distinct
		factKind("shared", "hook_a"),  // shared: 2 distinct hooks
		factKind("shared", "hook_b"),  //
		factKind("", "ignored"),       // blank domain ignored
	}
	shared := sharedConflictKeyProjections(fks)
	if _, ok := shared["shared"]; !ok {
		t.Error("expected 'shared' (>=2 distinct projection hooks) to be a shared-conflict-key projection")
	}
	if _, ok := shared["solo"]; ok {
		t.Error("'solo' has one projection hook and must not be shared")
	}
	if got := projectionDomains(fks); len(got) != 2 {
		t.Fatalf("projection domains = %v, want 2 (solo, shared)", got)
	}
}

func TestEnumerateDepthSurfaces(t *testing.T) {
	dr := DepthRequirements{
		RetractableNodeTypes: []string{"Function"},
		RetractableEdgeTypes: []string{"CALLS"},
		ReducerDrain:         ReducerDrainSurface{Surface: "drain"},
	}
	fks := []facts.FactKindRegistryEntry{factKind("dom_a", "h"), factKind("dom_b", "h")}
	got := map[string]Registry{}
	for _, s := range EnumerateDepthSurfaces(dr, fks) {
		got[s.Key] = s.Registry
	}
	want := map[string]Registry{
		"retractable_node:Function": RegistryRetractableType,
		"retractable_edge:CALLS":    RegistryRetractableEdgeType,
		"projection:dom_a":          RegistryProjection,
		"projection:dom_b":          RegistryProjection,
		"reducer_drain:drain":       RegistryReducerDrain,
	}
	for key, reg := range want {
		if got[key] != reg {
			t.Errorf("surface %q registry = %q, want %q", key, got[key], reg)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d surfaces, want %d: %v", len(got), len(want), got)
	}
}

func TestDeriveRequirementsPerApplicableSurface(t *testing.T) {
	dr := DepthRequirements{RetractableNodeTypes: []string{"Function"}, RetractableEdgeTypes: []string{"CALLS"}, ReducerDrain: ReducerDrainSurface{Surface: "drain"}}
	fks := []facts.FactKindRegistryEntry{
		factKind("solo", "h1"),
		factKind("shared", "h1"),
		factKind("shared", "h2"),
	}
	supported := []SupportedSurface{
		{Registry: RegistrySurfaceInventory, Key: "collector:aws"},
		{Registry: RegistryCapabilityMatrix, Key: "capability:demo"}, // no depth requirement
	}
	supported = append(supported, EnumerateDepthSurfaces(dr, fks)...)

	got := map[string][]DepthScenarioType{}
	for _, req := range DeriveRequirements(supported, dr, fks) {
		got[req.Surface] = req.ScenarioTypes
	}

	assertTypes(t, got, "collector:aws", ScenarioTypeBaseline, ScenarioTypeFault)
	assertTypes(t, got, "retractable_node:Function", ScenarioTypeDeltaTombstone)
	assertTypes(t, got, "retractable_edge:CALLS", ScenarioTypeDeltaTombstone)
	assertTypes(t, got, "projection:solo", ScenarioTypeCost)
	assertTypes(t, got, "projection:shared", ScenarioTypeCost, ScenarioTypeOrdering)
	assertTypes(t, got, "reducer_drain:drain", ScenarioTypeCrash)
	if _, ok := got["capability:demo"]; ok {
		t.Error("capability surface must not derive a depth requirement (only baseline applies)")
	}
}

func assertTypes(t *testing.T, got map[string][]DepthScenarioType, surface string, want ...DepthScenarioType) {
	t.Helper()
	gotTypes, ok := got[surface]
	if !ok {
		t.Errorf("surface %q has no derived requirement", surface)
		return
	}
	if len(gotTypes) != len(want) {
		t.Errorf("surface %q types = %v, want %v", surface, gotTypes, want)
		return
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Errorf("surface %q types = %v, want %v", surface, gotTypes, want)
			return
		}
	}
}

func TestUnionRequirementsMergesTypes(t *testing.T) {
	a := []ScenarioRequirement{{Surface: "collector:aws", ScenarioTypes: []DepthScenarioType{ScenarioTypeBaseline, ScenarioTypeFault}}}
	b := []ScenarioRequirement{
		{Surface: "collector:aws", ScenarioTypes: []DepthScenarioType{ScenarioTypeBaseline}},
		{Surface: "projection:x", ScenarioTypes: []DepthScenarioType{ScenarioTypeCost}},
	}
	got := map[string][]DepthScenarioType{}
	for _, req := range unionRequirements(a, b) {
		got[req.Surface] = req.ScenarioTypes
	}
	assertTypes(t, got, "collector:aws", ScenarioTypeBaseline, ScenarioTypeFault)
	assertTypes(t, got, "projection:x", ScenarioTypeCost)
}

// TestNewRetractableNodeReportedUncovered is the #4366 acceptance criterion:
// adding a retractable node type with no delta scenario must be reported as an
// uncovered, ADVISORY surface (it never fails the blocking breadth gate).
func TestNewRetractableNodeReportedUncovered(t *testing.T) {
	dr := DepthRequirements{RetractableNodeTypes: []string{"BrandNewNode"}, RetractableEdgeTypes: []string{"CALLS"}, ReducerDrain: ReducerDrainSurface{Surface: "drain"}}
	in := Inputs{
		FactKinds:         []facts.FactKindRegistryEntry{factKind("dom_a", "h")},
		DepthRequirements: dr,
		Manifest:          Manifest{Version: "v1"}, // no coverage entries
		Resolver:          ArtifactResolver{RepoRoot: t.TempDir()},
		Blocking:          true, // even blocking: depth gaps must stay advisory
	}
	_, _, gate := RunGate(in)
	if gate.Failed() {
		t.Fatal("depth gaps must be advisory: a missing delta scenario must not fail the blocking gate")
	}

	cov := Reconcile(appendDepth(in), unionDerived(in), in.Resolver)
	found := false
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == "retractable_node:BrandNewNode" {
			found = true
			if sc.ScenarioType != ScenarioTypeDeltaTombstone {
				t.Errorf("scenario_type = %q, want delta_tombstone", sc.ScenarioType)
			}
			if sc.Status != StatusUncovered {
				t.Errorf("status = %q, want uncovered", sc.Status)
			}
		}
	}
	if !found {
		t.Fatal("retractable_node:BrandNewNode not enumerated as a required depth surface")
	}
}

// TestNewRetractableEdgeReportedUncovered is the #4370 acceptance criterion:
// adding a retractable edge type with no delta scenario must be reported as an
// uncovered, advisory surface so C-14 can backfill the exact missing scenario.
func TestNewRetractableEdgeReportedUncovered(t *testing.T) {
	dr := DepthRequirements{RetractableNodeTypes: []string{"Function"}, RetractableEdgeTypes: []string{"BRAND_NEW_EDGE"}, ReducerDrain: ReducerDrainSurface{Surface: "drain"}}
	in := Inputs{
		FactKinds:         []facts.FactKindRegistryEntry{factKind("dom_a", "h")},
		DepthRequirements: dr,
		Manifest:          Manifest{Version: "v1"}, // no coverage entries
		Resolver:          ArtifactResolver{RepoRoot: t.TempDir()},
		Blocking:          true, // even blocking: depth gaps must stay advisory
	}
	_, _, gate := RunGate(in)
	if gate.Failed() {
		t.Fatal("edge depth gaps must be advisory: a missing delta scenario must not fail the blocking gate")
	}

	cov := Reconcile(appendDepth(in), unionDerived(in), in.Resolver)
	found := false
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == "retractable_edge:BRAND_NEW_EDGE" {
			found = true
			if sc.ScenarioType != ScenarioTypeDeltaTombstone {
				t.Errorf("scenario_type = %q, want delta_tombstone", sc.ScenarioType)
			}
			if sc.Status != StatusUncovered {
				t.Errorf("status = %q, want uncovered", sc.Status)
			}
		}
	}
	if !found {
		t.Fatal("retractable_edge:BRAND_NEW_EDGE not enumerated as a required depth surface")
	}
}

// appendDepth and unionDerived rebuild the supported set / manifest the way
// RunGate does, so the assertion above inspects the same reconciliation the gate
// produces rather than a re-implementation of it.
func appendDepth(in Inputs) []SupportedSurface {
	supported := EnumerateSupported(in.Inventory, in.FactKinds, in.Ledger, in.Matrix, in.ProductClaims, in.CLIShapes, in.Authorization)
	supported = append(supported, EnumerateDepthSurfaces(in.DepthRequirements, in.FactKinds)...)
	sortSupportedSurfaces(supported)
	return supported
}

func unionDerived(in Inputs) Manifest {
	m := in.Manifest
	m.Requirements = unionRequirements(m.Requirements, DeriveRequirements(appendDepth(in), in.DepthRequirements, in.FactKinds))
	return m
}

func TestApplyDepthExemptionsMarksExempt(t *testing.T) {
	cov := Coverage{Surfaces: []SurfaceCoverage{
		{Surface: SupportedSurface{Key: "retractable_node:Skip"}, ScenarioType: ScenarioTypeDeltaTombstone, Status: StatusUncovered},
		{Surface: SupportedSurface{Key: "retractable_node:Keep"}, ScenarioType: ScenarioTypeDeltaTombstone, Status: StatusUncovered},
	}}
	out := applyDepthExemptions(cov, map[string]string{
		manifestCoverageKey("retractable_node:Skip", ScenarioTypeDeltaTombstone): "structural",
	})
	if out.Surfaces[0].Status != StatusExempt || out.Surfaces[0].Detail != "structural" {
		t.Errorf("Skip should be exempt with reason, got %+v", out.Surfaces[0])
	}
	if out.Surfaces[1].Status != StatusUncovered {
		t.Errorf("Keep should stay uncovered, got %+v", out.Surfaces[1])
	}
}
