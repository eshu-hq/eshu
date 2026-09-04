// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// podTemplateFactWithContainerDigests builds a kubernetes_live.pod_template fact
// from explicit (declared image reference, CRI-resolved digest) pairs, one pair
// per container. Unlike podTemplateFactWithResolvedDigests, which keys resolved
// digests by reference and therefore cannot represent a conflict, this helper
// can express two containers that share one declared reference but report
// different resolved digests.
func podTemplateFactWithContainerDigests(
	factID, name, uid string,
	pairs [][2]string,
	selector map[string]string,
) facts.Envelope {
	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/" + name
	containers := make([]any, 0, len(pairs))
	imageRefs := make([]string, 0, len(pairs))
	for i, pair := range pairs {
		container := map[string]any{
			"name":  name + "-c" + string(rune('0'+i)),
			"image": pair[0],
			"init":  false,
		}
		if pair[1] != "" {
			container["resolved_image_digest"] = pair[1]
		}
		containers = append(containers, container)
		imageRefs = append(imageRefs, pair[0])
	}
	sel := make(map[string]any, len(selector))
	for key, value := range selector {
		sel[key] = value
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.KubernetesPodTemplateFactKind,
		Payload: map[string]any{
			"cluster_id":             testK8sCluster,
			"object_id":              objectID,
			"group_version_resource": "apps/v1/deployments",
			"namespace":              testK8sNamespace,
			"name":                   name,
			"uid":                    uid,
			"service_account":        "default",
			"image_refs":             imageRefs,
			"containers":             containers,
			"selector":               sel,
			"labels":                 sel,
			"correlation_anchors":    append([]string{objectID}, imageRefs...),
		},
	}
}

// TestBuildKubernetesCorrelationDecisionsConflictingCRIDigestsAreAmbiguous proves
// that when two containers share one declared image reference but report
// different CRI-resolved digests, the correlation is ambiguous and records both
// candidates, instead of promoting whichever container was read first (#5517).
//
// Both digests below have an active deployment-source observation, so the
// first-wins policy this replaces produced a confident Exact decision naming
// only testK8sDigest — an exact runtime identity claim that is wrong half the
// time, with nothing in the data telling an operator a second digest existed.
func TestBuildKubernetesCorrelationDecisionsConflictingCRIDigestsAreAmbiguous(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	resolvedA := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	resolvedB := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest2
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithContainerDigests("pod-1", "checkout", "uid-1",
			[][2]string{{imageRef, resolvedA}, {imageRef, resolvedB}},
			map[string]string{"app": "checkout"}),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
		k8sSourceManifestFact("oci-2", testK8sRegistry, testK8sRepository, testK8sDigest2, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationAmbiguous, driftUnknown)
	if decision.SourceDigest != "" {
		t.Fatalf("source_digest = %q, want empty: no single runtime identity was proven", decision.SourceDigest)
	}
	if !decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = false, want true for an unpromoted correlation")
	}
	if decision.NonPromotion == "" {
		t.Fatal("NonPromotion = empty, want an explicit non-promotion reason")
	}
	want := []string{testK8sDigest, testK8sDigest2}
	if !slices.Equal(decision.CandidateSourceDigests, want) {
		t.Fatalf("candidate_source_digests = %v, want %v", decision.CandidateSourceDigests, want)
	}
	// The inconsistent observation is the pod template itself, so an operator
	// triaging the ambiguity has the fact to go read.
	if !slices.Contains(decision.EvidenceFactIDs, "pod-1") {
		t.Fatalf("evidence_fact_ids = %v, want it to cite the pod template fact", decision.EvidenceFactIDs)
	}
}

// TestBuildKubernetesCorrelationDecisionsSameDigestDifferentRegistryStaysExact
// proves that two containers resolving one declared reference to the SAME
// content digest through different registry or mirror spellings still promote
// to exact. A digest is content-addressable, so those are the same image;
// treating the differing repository prefix as a conflict would suppress a
// legitimate RUNS_IMAGE edge for every deployment that pulls through a mirror.
func TestBuildKubernetesCorrelationDecisionsSameDigestDifferentRegistryStaysExact(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	viaRegistry := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	viaMirror := "mirror.example.net/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithContainerDigests("pod-1", "checkout", "uid-1",
			[][2]string{{imageRef, viaRegistry}, {imageRef, viaMirror}},
			map[string]string{"app": "checkout"}),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
	if decision.SourceDigest != testK8sDigest {
		t.Fatalf("source_digest = %q, want %q", decision.SourceDigest, testK8sDigest)
	}
	if len(decision.CandidateSourceDigests) != 0 {
		t.Fatalf("candidate_source_digests = %v, want none: one digest is not a conflict", decision.CandidateSourceDigests)
	}
}

// TestBuildKubernetesCorrelationDecisionsThreeConflictingCRIDigestsAreAmbiguous
// proves the conflict path holds past the two-digest case: the collect-time
// dedup and the sort/compact that builds the candidate list must both survive
// N > 2, since nothing bounds a pod to two containers.
func TestBuildKubernetesCorrelationDecisionsThreeConflictingCRIDigestsAreAmbiguous(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	prefix := testK8sRegistry + "/" + testK8sRepository + "@"
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithContainerDigests("pod-1", "checkout", "uid-1",
			[][2]string{
				{imageRef, prefix + testK8sDigest},
				{imageRef, prefix + testK8sDigest2},
				{imageRef, prefix + testK8sDigest3},
			},
			map[string]string{"app": "checkout"}),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
		k8sSourceManifestFact("oci-2", testK8sRegistry, testK8sRepository, testK8sDigest2, false),
		k8sSourceManifestFact("oci-3", testK8sRegistry, testK8sRepository, testK8sDigest3, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationAmbiguous, driftUnknown)
	want := []string{testK8sDigest, testK8sDigest2, testK8sDigest3}
	if !slices.Equal(decision.CandidateSourceDigests, want) {
		t.Fatalf("candidate_source_digests = %v, want %v", decision.CandidateSourceDigests, want)
	}
}

// TestBuildKubernetesCorrelationDecisionsAgreeingCRIDigestsStayExact proves the
// conflict detection does not disturb the ordinary case: two containers sharing
// one declared reference AND agreeing on the resolved digest still promote to
// exact/digest (#5517 non-regression for the #5432 promotion path).
func TestBuildKubernetesCorrelationDecisionsAgreeingCRIDigestsStayExact(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	resolved := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFactWithContainerDigests("pod-1", "checkout", "uid-1",
			[][2]string{{imageRef, resolved}, {imageRef, resolved}},
			map[string]string{"app": "checkout"}),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
	if decision.SourceDigest != testK8sDigest {
		t.Fatalf("source_digest = %q, want %q", decision.SourceDigest, testK8sDigest)
	}
	if len(decision.CandidateSourceDigests) != 0 {
		t.Fatalf("candidate_source_digests = %v, want none for an agreeing pair", decision.CandidateSourceDigests)
	}
}
