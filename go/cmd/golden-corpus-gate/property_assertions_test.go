// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

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
		map[string]bool{"rc-test-sourcetool": true}, nil, &r); err != nil {
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
		map[string]bool{"rc-test-sourcetool": true}, nil, &r); err != nil {
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
		map[string]bool{"rc-test-sourcetool": true}, nil, &r); err != nil {
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
	if err := checkGraph(context.Background(), c, nodePropertySnapshot(), true, nil, nil, &r); err != nil {
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
	if err := checkGraph(context.Background(), c, nodePropertySnapshot(), true, nil, nil, &r); err != nil {
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
	if err := checkGraph(context.Background(), fakeCounter{}, snap, true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a RequiredNode with no matching nodes must fail")
	}
}

// selfLoopSnapshot models "exactly 2 Function{language:dart} CALLS self-loops"
// — the eshu-hq/eshu#5349 durable corpus gate for the eshu-hq/eshu#5332
// declaration-vs-call-site self-loop fix.
func selfLoopSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredSelfLoops: []RequiredSelfLoop{{
				ID: "sl-dart-calls-recursion", Label: "Function", Relationship: "CALLS",
				NodeProperty: "language", NodePropertyValue: "dart",
				MinimumCount: 2, MaximumCount: 2,
			}},
		},
	}
}

// TestCheckGraphSelfLoopExactCountPasses confirms the pinned exact count (2:
// arrow-recursion + block-recursion, see tests/fixtures/ecosystems/
// dart_comprehensive) passes.
func TestCheckGraphSelfLoopExactCountPasses(t *testing.T) {
	c := fakeCounter{selfLoop: map[string]int64{"Function|CALLS|language|dart": 2}}
	var r Report
	if err := checkGraph(context.Background(), c, selfLoopSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass at the pinned exact self-loop count; findings: %+v", r.Findings)
	}
}

// TestCheckGraphSelfLoopRegressedInflationFails is the keystone acceptance for
// eshu-hq/eshu#5349: if the eshu-hq/eshu#5332 declaration-vs-call-site
// self-loop bug regresses, EVERY declaration in the fixture becomes a spurious
// self-loop, pushing the observed count well past the pinned ceiling of 2. A
// floor-only assertion would not catch this; the gate must fail.
func TestCheckGraphSelfLoopRegressedInflationFails(t *testing.T) {
	c := fakeCounter{selfLoop: map[string]int64{"Function|CALLS|language|dart": 11}}
	var r Report
	if err := checkGraph(context.Background(), c, selfLoopSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a self-loop count above the pinned ceiling (spurious declaration self-loops) must fail the gate")
	}
}

// TestCheckGraphSelfLoopDroppedRecursionFails confirms the floor side of the
// bound also blocks: genuine recursion silently filtered out must not pass.
func TestCheckGraphSelfLoopDroppedRecursionFails(t *testing.T) {
	c := fakeCounter{selfLoop: map[string]int64{"Function|CALLS|language|dart": 0}}
	var r Report
	if err := checkGraph(context.Background(), c, selfLoopSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("zero self-loops when genuine recursion is pinned must fail the gate")
	}
}

// packageArtifactHashesSnapshot mirrors the rn-package-artifact-hashes entry
// committed in testdata/golden/e2e-20repo-snapshot.json (#5820 P2 review
// finding): the exact digest value pinned for the package_registry
// supply-chain-demo cassette's github.com/acme/lib-common@1.0.0
// package_artifact fact. rc-168 (required_correlations) already asserts the
// PackageVersion-[:HAS_ARTIFACT]->PackageArtifact edge exists, but a live
// projection that creates the node and edge while omitting or corrupting
// `hashes` would still satisfy rc-168 -- this is the value-level counterpart
// that a #5458 feature whose entire purpose is preserving per-artifact
// digests needs.
func packageArtifactHashesSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredNodes: []RequiredNode{{
				ID:                     "rn-package-artifact-hashes",
				Label:                  "PackageArtifact",
				MinimumCount:           1,
				RequiredNodeProperties: []string{"hashes"},
				AllowedNodePropertyValues: map[string][]string{
					"hashes": {
						"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85|sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3",
					},
				},
			}},
		},
	}
}

// TestCheckGraphPackageArtifactHashesCorrectValuePasses is the non-vacuity
// positive proof for rn-package-artifact-hashes: the PackageArtifact node
// exists and its hashes property -- already "|"-joined the way
// ListNodeProperty/boltPropertyString join a live Bolt list property -- matches
// the pinned exact digest value.
func TestCheckGraphPackageArtifactHashesCorrectValuePasses(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{
			"PackageArtifact|hashes": {
				"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85|sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3",
			},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when hashes carries the exact pinned digest value; findings: %+v", r.Findings)
	}
}

// TestCheckGraphPackageArtifactHashesMissingFails proves the assertion is
// non-vacuous the way the #5820 P2 finding demanded: a live projection that
// creates the PackageArtifact node (and, via rc-168, the HAS_ARTIFACT edge)
// but OMITS hashes -- the exact defect class rc-168 alone could not catch --
// must fail the gate.
func TestCheckGraphPackageArtifactHashesMissingFails(t *testing.T) {
	c := fakeCounter{
		nodes:    map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{"PackageArtifact|hashes": {""}},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a PackageArtifact node missing hashes must fail the gate")
	}
}

// TestCheckGraphPackageArtifactHashesWrongDigestFails proves the assertion
// checks the VALUE, not just presence: a writer that writes hashes but with a
// corrupted or incomplete digest must fail even though the property is
// non-empty.
func TestCheckGraphPackageArtifactHashesWrongDigestFails(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"PackageArtifact": 1},
		nodeProp: map[string][]string{
			"PackageArtifact|hashes": {"sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, packageArtifactHashesSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a PackageArtifact node carrying a corrupted/incomplete hashes value must fail the gate")
	}
}

// TestLoadSnapshotPackageArtifactHashesEntryMatchesCassette proves the
// COMMITTED rn-package-artifact-hashes entry in
// testdata/golden/e2e-20repo-snapshot.json parses with the expected
// label/property/pinned-value shape, catching a typo in the committed JSON
// that the hermetic packageArtifactHashesSnapshot()-based tests above cannot
// catch (they hardcode their own copy of the expected shape).
func TestLoadSnapshotPackageArtifactHashesEntryMatchesCassette(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	var found *RequiredNode
	for i := range snap.Graph.RequiredNodes {
		if snap.Graph.RequiredNodes[i].ID == "rn-package-artifact-hashes" {
			found = &snap.Graph.RequiredNodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("testdata/golden/e2e-20repo-snapshot.json required_nodes is missing rn-package-artifact-hashes")
	}
	if found.Label != "PackageArtifact" {
		t.Fatalf("Label = %q, want %q", found.Label, "PackageArtifact")
	}
	if len(found.RequiredNodeProperties) != 1 || found.RequiredNodeProperties[0] != "hashes" {
		t.Fatalf("RequiredNodeProperties = %v, want [hashes]", found.RequiredNodeProperties)
	}
	want := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85|" +
		"sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3"
	allowed := found.AllowedNodePropertyValues["hashes"]
	if len(allowed) != 1 || allowed[0] != want {
		t.Fatalf("AllowedNodePropertyValues[hashes] = %v, want [%s]", allowed, want)
	}
}

// registryEventFieldsSnapshot mirrors the rn-registry-event-fields entry
// committed in testdata/golden/e2e-20repo-snapshot.json: the exact event_type
// value pinned for the package_registry supply-chain-demo cassette's
// github.com/acme/lib-common@1.0.0 registry_event fact. rc-169
// (required_correlations) already asserts the
// PackageVersion-[:HAS_REGISTRY_EVENT]->RegistryEvent edge exists, but a live
// projection that creates the node and edge while omitting or corrupting
// event_type would still satisfy rc-169 -- this is the value-level
// counterpart, mirroring packageArtifactHashesSnapshot's rationale above.
func registryEventFieldsSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: "1",
		Graph: GraphSnapshot{
			RequiredNodes: []RequiredNode{{
				ID:                     "rn-registry-event-fields",
				Label:                  "RegistryEvent",
				MinimumCount:           1,
				RequiredNodeProperties: []string{"event_type"},
				AllowedNodePropertyValues: map[string][]string{
					"event_type": {"yank"},
				},
			}},
		},
	}
}

// TestCheckGraphRegistryEventFieldsCorrectValuePasses is the non-vacuity
// positive proof for rn-registry-event-fields: the RegistryEvent node exists
// and its event_type property matches the pinned exact value.
func TestCheckGraphRegistryEventFieldsCorrectValuePasses(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"RegistryEvent": 1},
		nodeProp: map[string][]string{
			"RegistryEvent|event_type": {"yank"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, registryEventFieldsSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected pass when event_type carries the exact pinned value; findings: %+v", r.Findings)
	}
}

// TestCheckGraphRegistryEventFieldsMissingFails proves the assertion is
// non-vacuous: a live projection that creates the RegistryEvent node (and,
// via rc-169, the HAS_REGISTRY_EVENT edge) but OMITS event_type -- the exact
// defect class rc-169 alone could not catch -- must fail the gate.
func TestCheckGraphRegistryEventFieldsMissingFails(t *testing.T) {
	c := fakeCounter{
		nodes:    map[string]int64{"RegistryEvent": 1},
		nodeProp: map[string][]string{"RegistryEvent|event_type": {""}},
	}
	var r Report
	if err := checkGraph(context.Background(), c, registryEventFieldsSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a RegistryEvent node missing event_type must fail the gate")
	}
}

// TestCheckGraphRegistryEventFieldsWrongValueFails proves the assertion
// checks the VALUE, not just presence: a writer that writes event_type but
// with the wrong lifecycle token must fail even though the property is
// non-empty.
func TestCheckGraphRegistryEventFieldsWrongValueFails(t *testing.T) {
	c := fakeCounter{
		nodes: map[string]int64{"RegistryEvent": 1},
		nodeProp: map[string][]string{
			"RegistryEvent|event_type": {"publish"},
		},
	}
	var r Report
	if err := checkGraph(context.Background(), c, registryEventFieldsSnapshot(), true, nil, nil, &r); err != nil {
		t.Fatalf("checkGraph err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("a RegistryEvent node carrying the wrong event_type value must fail the gate")
	}
}

// TestLoadSnapshotRegistryEventFieldsEntryMatchesCassette proves the
// COMMITTED rn-registry-event-fields entry in
// testdata/golden/e2e-20repo-snapshot.json parses with the expected
// label/property/pinned-value shape, catching a typo in the committed JSON
// that the hermetic registryEventFieldsSnapshot()-based tests above cannot
// catch (they hardcode their own copy of the expected shape).
func TestLoadSnapshotRegistryEventFieldsEntryMatchesCassette(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	var found *RequiredNode
	for i := range snap.Graph.RequiredNodes {
		if snap.Graph.RequiredNodes[i].ID == "rn-registry-event-fields" {
			found = &snap.Graph.RequiredNodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("testdata/golden/e2e-20repo-snapshot.json required_nodes is missing rn-registry-event-fields")
	}
	if found.Label != "RegistryEvent" {
		t.Fatalf("Label = %q, want %q", found.Label, "RegistryEvent")
	}
	if len(found.RequiredNodeProperties) != 1 || found.RequiredNodeProperties[0] != "event_type" {
		t.Fatalf("RequiredNodeProperties = %v, want [event_type]", found.RequiredNodeProperties)
	}
	allowed := found.AllowedNodePropertyValues["event_type"]
	if len(allowed) != 1 || allowed[0] != "yank" {
		t.Fatalf("AllowedNodePropertyValues[event_type] = %v, want [yank]", allowed)
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
