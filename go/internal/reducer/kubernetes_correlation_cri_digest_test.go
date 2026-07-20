// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildKubernetesCorrelationDecisionsCRIDigestTagBecomesExact proves that a
// tag-referenced live image WITH a CRI-resolved digest promotes to exact/digest
// when the resolved digest has an active deployment-source observation (#5432).
// Without this, a tag reference is always provenance-only Derived — the common
// case for tag-declared deployments, which means they NEVER produce RUNS_IMAGE
// edges.
func TestBuildKubernetesCorrelationDecisionsCRIDigestTagBecomesExact(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	resolvedDigest := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	resolved := map[string]string{imageRef: resolvedDigest}
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithResolvedDigests("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false, resolved),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
	if decision.ProvenanceOnly {
		t.Fatal("CRI-digest-promoted tag ProvenanceOnly = true, want false")
	}
	if decision.SourceDigest != testK8sDigest {
		t.Fatalf("source_digest = %q, want %q", decision.SourceDigest, testK8sDigest)
	}
	if decision.JoinMode != joinModeDigest {
		t.Fatalf("join_mode = %q, want %q", decision.JoinMode, joinModeDigest)
	}
}

// TestBuildKubernetesCorrelationDecisionsTagWithoutCRIDigestStaysDerived proves
// a tag-referenced image WITHOUT a resolved digest stays Derived/provenance-only
// — byte-identical to today's behavior (#5432 non-regression).
func TestBuildKubernetesCorrelationDecisionsTagWithoutCRIDigestStaysDerived(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	// No resolved digest — same as podTemplateFact.
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("oci-tag-1", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest, "", false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationDerived, driftInSync)
	if !decision.ProvenanceOnly {
		t.Fatal("tag without CRI digest ProvenanceOnly = false, want true (stays provenance-only)")
	}
}

// TestBuildKubernetesCorrelationDecisionsCRIDigestWithoutSourceIsUnresolved proves
// that a tag-referenced image WITH a resolved digest but NO deployment-source
// observation is unresolved/driftMissingSource, NOT tag-derived (#5432). The
// CRI digest is ground truth of what is running; without source evidence it is
// unresolved, never falling through to the weaker tag path.
func TestBuildKubernetesCorrelationDecisionsCRIDigestWithoutSourceIsUnresolved(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	resolvedDigest := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	resolved := map[string]string{imageRef: resolvedDigest}
	// Only a tag observation exists for a DIFFERENT digest — the resolved digest
	// has no source observation.
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithResolvedDigests("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false, resolved),
		k8sSourceTagFact("oci-tag-1", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest2, "", false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationUnresolved, driftMissingSource)
	if !decision.ProvenanceOnly {
		t.Fatal("CRI-digest unresolved ProvenanceOnly = false, want true")
	}
	// Must NOT fall through to tag classification — the CRI digest is ground truth.
	if decision.Outcome == KubernetesCorrelationDerived {
		t.Fatal("CRI-digest unresolved must NOT fall through to tag-derived")
	}
}

// TestExtractKubernetesCorrelationEdgeRowsCRIDigestProducesEdge proves
// ExtractKubernetesCorrelationEdgeRows emits a RUNS_IMAGE row for a CRI-digest-
// promoted workload whose resolved digest matches a source node (#5432).
func TestExtractKubernetesCorrelationEdgeRowsCRIDigestProducesEdge(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	resolvedDigest := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	resolved := map[string]string{imageRef: resolvedDigest}
	descriptorID := "oci-descriptor://" + testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	rows, tally, quarantined, err := ExtractKubernetesCorrelationEdgeRows([]facts.Envelope{
		podTemplateFactWithResolvedDigests("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false, resolved),
		k8sSourceManifestWithNode("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, descriptorID, false),
	})
	if err != nil {
		t.Fatalf("ExtractKubernetesCorrelationEdgeRows error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	if tally.materialized[joinModeDigest] != 1 {
		t.Fatalf("materialized[digest] = %d, want 1", tally.materialized[joinModeDigest])
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_digest"], testK8sDigest; got != want {
		t.Fatalf("source_digest = %q, want %q", got, want)
	}
}

// TestExtractKubernetesCorrelationEdgeRowsTagWithoutCRIDigestProducesNoEdge proves
// a tag-referenced workload WITHOUT a resolved digest produces no RUNS_IMAGE edge
// — byte-identical to today (#5432 non-regression).
func TestExtractKubernetesCorrelationEdgeRowsTagWithoutCRIDigestProducesNoEdge(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	rows, tally, quarantined, err := ExtractKubernetesCorrelationEdgeRows([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("oci-tag-1", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest, "", false),
	})
	if err != nil {
		t.Fatalf("ExtractKubernetesCorrelationEdgeRows error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 (tag-derived is not edge-eligible)", len(rows))
	}
	if tally.materialized[joinModeDigest] != 0 {
		t.Fatalf("materialized[digest] = %d, want 0 (tag-derived never materialized)", tally.materialized[joinModeDigest])
	}
	// Tag-derived decisions are not edge-eligible (Outcome != Exact), so they are
	// neither materialized nor skipped — they are simply provenance-only.
}
