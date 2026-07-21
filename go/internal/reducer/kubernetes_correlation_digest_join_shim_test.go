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
// after the CRI-resolved-digest feature (#5432). It runs against a coherent
// synthetic fixture — every image ref uses the same repository, and every
// source manifest uses the same repository, so the join keys align.
//
// This is a THEORY-PROOF shim per AGENTS.md "Prove-The-Theory-First" — run it
// with:
//
//	cd go && go test ./internal/reducer -run TestDigestJoinCardinalityShim -v -count=1
func TestDigestJoinCardinalityShim(t *testing.T) {
	// Coherent repository: every image ref and every source manifest uses the
	// same repository, avoiding the vacuous repo-key mismatch the original
	// version had (team/api refs vs team/checkout manifests).
	repo := testK8sRegistry + "/" + testK8sRepository // registry.example.com/team/checkout

	type fixture struct {
		name           string
		imageRef       string
		resolvedDigest string           // empty => no CRI digest
		sourceFacts    []facts.Envelope // source evidence for this ref's digest(s)
		// expectedExactPost reports whether the decision should be Exact
		// (non-provenance-only) POST-5432.
		expectedExactPost bool
		// expectedEdgePost reports whether a RUNS_IMAGE edge should be produced
		// POST-5432.
		expectedEdgePost bool
	}

	descriptorID := func(digest string) string {
		return "oci-descriptor://" + testK8sRegistry + "/" + testK8sRepository + "@" + digest
	}

	fixtures := []fixture{
		// Digest-pinned refs — always joinable (pre and post).
		{
			name:              "digest-pinned-1",
			imageRef:          repo + "@" + testK8sDigest,
			sourceFacts:       []facts.Envelope{k8sSourceManifestWithNode("oci-d1", testK8sRegistry, testK8sRepository, testK8sDigest, descriptorID(testK8sDigest), false)},
			expectedExactPost: true,
			expectedEdgePost:  true,
		},
		{
			name:              "digest-pinned-2",
			imageRef:          repo + "@" + testK8sDigest2,
			sourceFacts:       []facts.Envelope{k8sSourceManifestWithNode("oci-d2", testK8sRegistry, testK8sRepository, testK8sDigest2, descriptorID(testK8sDigest2), false)},
			expectedExactPost: true,
			expectedEdgePost:  true,
		},

		// Tag-refs WITH CRI-resolved digest + matching source — promote to exact.
		{
			name:              "tag-cri-match",
			imageRef:          repo + ":v1.0.0",
			resolvedDigest:    repo + "@" + testK8sDigest,
			sourceFacts:       []facts.Envelope{k8sSourceManifestWithNode("oci-tcm", testK8sRegistry, testK8sRepository, testK8sDigest, descriptorID(testK8sDigest), false)},
			expectedExactPost: true,
			expectedEdgePost:  true,
		},

		// Tag-ref WITH CRI-resolved digest but NO source observation → unresolved.
		{
			name:              "tag-cri-no-source",
			imageRef:          repo + ":v9.9.9",
			resolvedDigest:    repo + "@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			sourceFacts:       nil,
			expectedExactPost: false,
			expectedEdgePost:  false,
		},

		// Tag-refs WITHOUT CRI digest — never joinable (tag classification always
		// provenance-only Derived/Ambiguous/Unresolved).
		{
			name:              "tag-no-cri-1",
			imageRef:          repo + ":latest",
			sourceFacts:       []facts.Envelope{k8sSourceTagFact("oci-tn1", testK8sRegistry, testK8sRepository, "latest", testK8sDigest, "", false)},
			expectedExactPost: false,
			expectedEdgePost:  false,
		},
		{
			name:              "tag-no-cri-2",
			imageRef:          repo + ":v2.0.0",
			sourceFacts:       nil,
			expectedExactPost: false,
			expectedEdgePost:  false,
		},
	}

	// Counts for the honest before/after comparison.
	// BEFORE (#5432 not present): only digest-pinned refs are exact/edge-eligible.
	// AFTER (#5432 present): digest-pinned refs + CRI-digest-promoted tag refs
	//   with matching source evidence.
	joinablePre := 0
	joinablePost := 0
	edgePre := 0
	edgePost := 0
	digestPinnedPre := 0
	digestPinnedPost := 0

	for _, f := range fixtures {
		var envelopes []facts.Envelope
		var resolved map[string]string
		if f.resolvedDigest != "" {
			resolved = map[string]string{f.imageRef: f.resolvedDigest}
		}
		envelopes = append(envelopes, podTemplateFactWithResolvedDigests(
			"pod-"+f.name, f.name, "uid-"+f.name,
			[]string{f.imageRef}, nil, false, resolved,
		))
		envelopes = append(envelopes, f.sourceFacts...)

		// Run classification (always with the CRI-digest code path active).
		decisions := BuildKubernetesCorrelationDecisions(envelopes)
		var decision *KubernetesCorrelationDecision
		for i := range decisions {
			if decisions[i].ImageRef == f.imageRef {
				decision = &decisions[i]
				break
			}
		}
		if decision == nil {
			t.Fatalf("%s: no decision produced for imageRef %q", f.name, f.imageRef)
		}

		// Assert POST-5432 expectation.
		isExact := decision.Outcome == KubernetesCorrelationExact && !decision.ProvenanceOnly
		if isExact != f.expectedExactPost {
			t.Fatalf("%s: exact = %v (outcome=%s, provenance_only=%v), want exact=%v",
				f.name, isExact, decision.Outcome, decision.ProvenanceOnly, f.expectedExactPost)
		}

		// Check edge production.
		rows, _, _, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
		hasEdge := len(rows) > 0
		if hasEdge != f.expectedEdgePost {
			t.Fatalf("%s: edge_rows = %d, want edge=%v", f.name, len(rows), f.expectedEdgePost)
		}

		// Compute BEFORE/POST counts for the honest report.
		// BEFORE: only digest-pinned refs (parsed.digest != "") with exact outcome.
		// POST: all exact outcomes (digest-pinned + CRI-promoted).
		isDigestPinned := false
		if parsed, ok := parseContainerImageRef(f.imageRef); ok && parsed.digest != "" {
			isDigestPinned = true
		}

		if isExact {
			joinablePost++
			if isDigestPinned {
				joinablePre++
			}
		}
		if hasEdge {
			edgePost++
			if isDigestPinned {
				edgePre++
			}
		}
		if isDigestPinned {
			digestPinnedPre++
			if isExact {
				digestPinnedPost++
			}
		}
	}

	totalRefs := len(fixtures)
	pctPre := float64(joinablePre) / float64(totalRefs) * 100
	pctPost := float64(joinablePost) / float64(totalRefs) * 100
	edgePctPre := float64(edgePre) / float64(totalRefs) * 100
	edgePctPost := float64(edgePost) / float64(totalRefs) * 100

	fmt.Printf("\n=== Digest-Join Cardinality Shim (#5432) ===\n")
	fmt.Printf("Total image refs in fixture:        %d (%d digest-pinned, %d tag-only)\n", totalRefs, digestPinnedPre, totalRefs-digestPinnedPre)
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
