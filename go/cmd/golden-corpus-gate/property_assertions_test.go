// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

// TestValueAllowed locks the membership helper that backs both the edge- and
// node-property value checks.
func TestValueAllowed(t *testing.T) {
	cases := []struct {
		v       string
		allowed []string
		want    bool
	}{
		{"ansible", []string{"ansible", "gomod"}, true},
		{"terraform", []string{"ansible", "gomod"}, false},
		{"", []string{"ansible"}, false},
		{"ansible", nil, false}, // empty allowed set matches nothing
	}
	for _, tc := range cases {
		if got := valueAllowed(tc.v, tc.allowed); got != tc.want {
			t.Errorf("valueAllowed(%q, %v) = %v, want %v", tc.v, tc.allowed, got, tc.want)
		}
	}
}

// TestEvaluateEdgeProperty proves the absence-zero edge-property contract: among
// the matching (already evidence-narrowed) edges, an edge missing the property or
// carrying a value outside the allowed set is offending, and any offending edge
// fails the finding. A no-matching-edges case passes vacuously because the
// separate rc count finding guards existence.
func TestEvaluateEdgeProperty(t *testing.T) {
	rc := RequiredCorrelation{ID: "rc-x", Relationship: "DEPENDS_ON", FromLabel: "Repository", ToLabel: "Repository"}
	cases := []struct {
		name    string
		values  []string
		allowed []string
		wantOK  bool
	}{
		{"all carry property", []string{"ansible", "ansible"}, nil, true},
		{"one edge missing property", []string{"ansible", ""}, nil, false},
		{"all edges missing property", []string{"", ""}, nil, false},
		{"value outside allowed set", []string{"ansible", "terraform"}, []string{"ansible"}, false},
		{"all values in allowed set", []string{"ansible", "ansible"}, []string{"ansible", "gomod"}, true},
		{"no matching edges passes vacuously", nil, nil, true},
	}
	for _, tc := range cases {
		f := evaluateEdgeProperty(rc, "source_tool", tc.values, tc.allowed, true)
		if f.OK != tc.wantOK {
			t.Errorf("%s: OK=%v want %v (detail %q)", tc.name, f.OK, tc.wantOK, f.Detail)
		}
		if !f.Required {
			t.Errorf("%s: edge-property finding must inherit required=true", tc.name)
		}
	}
}

// TestEvaluateNodeProperty proves the presence-positive node-property contract:
// at least MinimumCount nodes must carry a non-empty value (in the allowed set
// when pinned). Unlike edges, a label legitimately contains property-less nodes
// (a LICENSE has no language), so the gate asserts a floor of tagged nodes rather
// than the absence of any untagged node.
func TestEvaluateNodeProperty(t *testing.T) {
	rn := RequiredNode{ID: "rn-file", Label: "File", MinimumCount: 2}
	cases := []struct {
		name    string
		values  []string
		allowed []string
		wantOK  bool
	}{
		{"enough carriers", []string{"go", "python", "yaml"}, nil, true},
		{"exactly the floor", []string{"go", "python"}, nil, true},
		{"below the floor", []string{"go", ""}, nil, false},
		{"all empty", []string{"", ""}, nil, false},
		{"allowed set filters out non-canonical", []string{"go", "Golang"}, []string{"go"}, false},
		{"allowed set met", []string{"go", "python", "java"}, []string{"go", "python"}, true},
	}
	for _, tc := range cases {
		f := evaluateNodeProperty(rn, "language", tc.values, tc.allowed)
		if f.OK != tc.wantOK {
			t.Errorf("%s: OK=%v want %v (detail %q)", tc.name, f.OK, tc.wantOK, f.Detail)
		}
		if !f.Required {
			t.Errorf("%s: node-property finding must be required", tc.name)
		}
	}
}

// edgePropertySnapshot models "ansible DEPENDS_ON must carry source_tool=ansible"
// riding the shared, tool-agnostic DEPENDS_ON edge, narrowed by the Ansible
// evidence kind.
func edgePropertySnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredCorrelations: []RequiredCorrelation{{
				ID:                        "rc-test-sourcetool",
				Relationship:              "DEPENDS_ON",
				FromLabel:                 "Repository",
				ToLabel:                   "Repository",
				MinimumCount:              1,
				EvidenceKinds:             []string{"ANSIBLE_ROLE_REFERENCE"},
				RequiredEdgeProperties:    []string{"source_tool"},
				AllowedEdgePropertyValues: map[string][]string{"source_tool": {"ansible"}},
			}},
		},
	}
}

// TestCheckGraphEdgePropertyMissingFails is the keystone acceptance for #4010:
// the Ansible DEPENDS_ON edge exists (rc count passes) but carries no source_tool
// — exactly the "an emitter forgot to stamp source_tool" regression — and the
// gate MUST fail. Without the property assertion this passes green (the bug the
// issue exists to prevent).
func TestCheckGraphEdgePropertyMissingFails(t *testing.T) {
	c := fakeCounter{
		corr:   map[string]int64{"Repository|DEPENDS_ON|Repository": 3},
		corrEv: map[string]int64{"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE": 1},
		edgeProp: map[string][]string{
			"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE|source_tool": {""},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, edgePropertySnapshot(), true,
		map[string]bool{"rc-test-sourcetool": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("an evidence-isolated edge missing the required source_tool property must fail the gate")
	}
}

// TestCheckGraphEdgePropertyPresentPasses confirms the same correlation passes
// once the matching edge carries the canonical source_tool value.
func TestCheckGraphEdgePropertyPresentPasses(t *testing.T) {
	c := fakeCounter{
		corr:   map[string]int64{"Repository|DEPENDS_ON|Repository": 3},
		corrEv: map[string]int64{"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE": 1},
		edgeProp: map[string][]string{
			"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE|source_tool": {"ansible"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, edgePropertySnapshot(), true,
		map[string]bool{"rc-test-sourcetool": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when the matching edge carries source_tool=ansible; findings: %+v", r.Findings)
	}
}

// TestCheckGraphEdgePropertyWrongValueFails confirms a value outside the pinned
// canonical vocabulary (an un-normalized token) fails even though the property is
// present — the gate enforces the vocabulary, not just presence.
func TestCheckGraphEdgePropertyWrongValueFails(t *testing.T) {
	c := fakeCounter{
		corr:   map[string]int64{"Repository|DEPENDS_ON|Repository": 3},
		corrEv: map[string]int64{"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE": 1},
		edgeProp: map[string][]string{
			"Repository|DEPENDS_ON|Repository|ANSIBLE_ROLE_REFERENCE|source_tool": {"ANSIBLE_ROLE_REFERENCE"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, edgePropertySnapshot(), true,
		map[string]bool{"rc-test-sourcetool": true}, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("an un-normalized source_tool value outside the allowed vocabulary must fail the gate")
	}
}

// nodePropertySnapshot models "at least 2 File nodes carry a non-empty language".
func nodePropertySnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredNodes: []RequiredNode{{
				ID:                     "rn-file-language",
				Label:                  "File",
				MinimumCount:           2,
				RequiredNodeProperties: []string{"language"},
			}},
		},
	}
}

// TestCheckGraphNodePropertyFloorFails proves the language axis (#4003) regresses
// to a gate failure: enough File nodes exist, but fewer than the floor carry a
// language (extraction regressed), so the gate fails.
func TestCheckGraphNodePropertyFloorFails(t *testing.T) {
	c := fakeCounter{
		nodes:    map[string]int64{"File": 5},
		nodeProp: map[string][]string{"File|language": {"go", "", "", "", ""}},
	}
	var r Report
	if err := checkGraph(context.Background(), c, nodePropertySnapshot(), true, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("fewer File nodes carrying language than the floor must fail the gate")
	}
}

// TestCheckGraphNodePropertyFloorPasses confirms the floor passes once enough
// File nodes carry a language; legitimately language-less files do not fail it.
func TestCheckGraphNodePropertyFloorPasses(t *testing.T) {
	c := fakeCounter{
		nodes:    map[string]int64{"File": 5},
		nodeProp: map[string][]string{"File|language": {"go", "python", "", ""}},
	}
	var r Report
	if err := checkGraph(context.Background(), c, nodePropertySnapshot(), true, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when >=2 File nodes carry language; findings: %+v", r.Findings)
	}
}

// TestCheckGraphNodePresenceFailsWhenLabelEmpty confirms a RequiredNode also
// asserts label presence (count floor) even with no property requirement.
func TestCheckGraphNodePresenceFailsWhenLabelEmpty(t *testing.T) {
	snap := Snapshot{SchemaVersion: "1", Graph: GraphSnapshot{
		RequiredNodes: []RequiredNode{{ID: "rn-platform", Label: "Platform", MinimumCount: 1}},
	}}
	var r Report
	if err := checkGraph(context.Background(), fakeCounter{}, snap, true, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a RequiredNode with no matching nodes must fail")
	}
}

// TestLoadSnapshotParsesPropertyAssertions proves the schema additions are
// additive and round-trip: the committed golden snapshot still loads, and the new
// optional fields default to empty (no property check) for existing entries.
func TestLoadSnapshotParsesPropertyAssertions(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	for _, rc := range snap.Graph.RequiredCorrelations {
		if rc.ID == "" {
			t.Fatalf("rc missing id: %+v", rc)
		}
		// Existing entries carry no property requirements (default = no check).
		_ = rc.RequiredEdgeProperties
		_ = rc.AllowedEdgePropertyValues
	}
}
