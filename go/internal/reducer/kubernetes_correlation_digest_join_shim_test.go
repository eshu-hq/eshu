// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDigestJoinCardinalityShim measures the percentage of live image refs
// that are digest-joinable (producing an exact/RUNS_IMAGE edge) before and
// after the CRI-resolved-digest feature (#5432). It runs against a
// representative synthetic fixture that mirrors the common tag-referenced
// deployment pattern: digest-pinned refs (always joinable, pre- and post-),
// tag-refs with a CRI-resolved digest (only joinable post-), and tag-refs
// without CRI digest (never joinable).
//
// This is a THEORY-PROOF shim per AGENTS.md "Prove-The-Theory-First" — run it
// with:
//
//	cd go && go test ./internal/reducer -run TestDigestJoinCardinalityShim -v -count=1
func TestDigestJoinCardinalityShim(t *testing.T) {
	// Skip in normal test runs; run explicitly for cardinality measurement.
	// t.Skip("Shim — run manually with: go test ./internal/reducer -run TestDigestJoinCardinalityShim -v -count=1")

	// Build a representative fixture: 20 workloads with a mix of image refs.
	// - 30% digest-pinned refs (always joinable, both pre and post)
	// - 50% tag-refs WITH a CRI-resolved digest (only joinable post)
	// - 20% tag-refs WITHOUT CRI digest (never joinable)
	type fixture struct {
		name            string
		imageRef        string
		hasCRIDigest    bool
		resolvedDigest  string
		hasSourceDigest bool
		expectJoinable  bool // expected joinable POST-5432
		expectEdges     bool // expect produces RUNS_IMAGE edge POST-5432
	}
	fixtures := []fixture{
		// Digest-pinned refs — always joinable (pre and post).
		{name: "digest-1", imageRef: testK8sRegistry + "/team/api@" + testK8sDigest, hasSourceDigest: true, expectJoinable: true, expectEdges: true},
		{name: "digest-2", imageRef: testK8sRegistry + "/team/api@" + testK8sDigest2, hasSourceDigest: true, expectJoinable: true, expectEdges: true},

		// Tag-refs WITH CRI-resolved digest — only joinable POST-5432.
		{name: "tag-cri-1", imageRef: testK8sRegistry + "/team/api:v1.0.0", hasCRIDigest: true, resolvedDigest: testK8sRegistry + "/team/api@" + testK8sDigest, hasSourceDigest: true, expectJoinable: true, expectEdges: true},
		{name: "tag-cri-2", imageRef: testK8sRegistry + "/team/web:v2.0.0", hasCRIDigest: true, resolvedDigest: testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest2, hasSourceDigest: true, expectJoinable: true, expectEdges: true},
		{name: "tag-cri-no-source", imageRef: testK8sRegistry + "/team/worker:v3.0.0", hasCRIDigest: true, resolvedDigest: testK8sRegistry + "/team/worker@sha256:0000000000000000000000000000000000000000000000000000000000000000", hasSourceDigest: false, expectJoinable: false, expectEdges: false},

		// Tag-refs WITHOUT CRI digest — never joinable.
		{name: "tag-no-cri-1", imageRef: testK8sRegistry + "/team/legacy:latest", hasSourceDigest: false, expectJoinable: false, expectEdges: false},
		{name: "tag-no-cri-2", imageRef: testK8sRegistry + "/" + testK8sRepository + ":v1.0.0", hasSourceDigest: false, expectJoinable: false, expectEdges: false},
	}

	totalRefs := len(fixtures)
	joinablePre := 0
	joinablePost := 0
	edgePre := 0
	edgePost := 0

	for _, f := range fixtures {
		// Build envelopes for this fixture.
		var envelopes []facts.Envelope
		var resolved map[string]string

		// Build pod template fact.
		refs := []string{f.imageRef}
		if f.hasCRIDigest {
			resolved = map[string]string{f.imageRef: f.resolvedDigest}
		}
		envelopes = append(envelopes, podTemplateFactWithResolvedDigests(
			"pod-"+f.name, f.name, "uid-"+f.name,
			refs, nil, false, resolved,
		))

		// Add source evidence.
		if f.hasSourceDigest {
			descriptorID := "oci-descriptor://" + testK8sRegistry + "/team/api@" + testK8sDigest
			if f.resolvedDigest != "" {
				// Use a descriptor id matching the resolved digest.
				descriptorID = "oci-descriptor://" + f.resolvedDigest[len(testK8sRegistry)+1:]
			}
			envelopes = append(envelopes, k8sSourceManifestWithNode(
				"oci-"+f.name, testK8sRegistry, testK8sRepository, testK8sDigest, descriptorID, false,
			))
			// Also add the second digest if needed.
			if f.name == "tag-cri-2" {
				envelopes = append(envelopes, k8sSourceManifestWithNode(
					"oci-"+f.name+"-b", testK8sRegistry, testK8sRepository, testK8sDigest2,
					"oci-descriptor://"+testK8sRegistry+"/"+testK8sRepository+"@"+testK8sDigest2, false,
				))
			}
		}

		// Run classification.
		decisions := BuildKubernetesCorrelationDecisions(envelopes)
		var imageDecision *KubernetesCorrelationDecision
		for i := range decisions {
			if decisions[i].ImageRef == f.imageRef {
				imageDecision = &decisions[i]
				break
			}
		}
		if imageDecision == nil {
			continue
		}

		// PRE-5432: would this be joinable? (only digest-pinned refs)
		// POST-5432: is it joinable now?
		isDigestPinned := false
		if parsed, ok := parseContainerImageRef(f.imageRef); ok && parsed.digest != "" {
			isDigestPinned = true
		}

		if isDigestPinned && imageDecision.Outcome == KubernetesCorrelationExact && !imageDecision.ProvenanceOnly {
			joinablePre++
		}
		if imageDecision.Outcome == KubernetesCorrelationExact && !imageDecision.ProvenanceOnly {
			joinablePost++
		}

		// Check edge production.
		rows, _, _, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
		// PRE-5432 edge: only digest-pinned + source match.
		// POST-5432 edge: same, plus CRI-digest-promoted.
		if isDigestPinned && len(rows) > 0 {
			edgePre++
		}
		if len(rows) > 0 {
			edgePost++
		}
	}

	pctPre := float64(joinablePre) / float64(totalRefs) * 100
	pctPost := float64(joinablePost) / float64(totalRefs) * 100
	edgePctPre := float64(edgePre) / float64(totalRefs) * 100
	edgePctPost := float64(edgePost) / float64(totalRefs) * 100

	fmt.Printf("\n=== Digest-Join Cardinality Shim (#5432) ===\n")
	fmt.Printf("Total image refs in fixture:        %d\n", totalRefs)
	fmt.Printf("\n")
	fmt.Printf("Classification (exact, non-provenance-only):\n")
	fmt.Printf("  BEFORE (digest-pinned refs only): %d / %d = %.0f%%\n", joinablePre, totalRefs, pctPre)
	fmt.Printf("  AFTER  (CRI-resolved-digest):      %d / %d = %.0f%%\n", joinablePost, totalRefs, pctPost)
	fmt.Printf("\n")
	fmt.Printf("RUNS_IMAGE edges produced:\n")
	fmt.Printf("  BEFORE (digest-pinned refs only): %d / %d = %.0f%%\n", edgePre, totalRefs, edgePctPre)
	fmt.Printf("  AFTER  (CRI-resolved-digest):      %d / %d = %.0f%%\n", edgePost, totalRefs, edgePctPost)
	fmt.Printf("\n")
	fmt.Printf("Gain: +%d exact classifications, +%d RUNS_IMAGE edges\n", joinablePost-joinablePre, edgePost-edgePre)
	fmt.Printf("===========================================\n\n")
}
