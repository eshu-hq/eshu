// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildKubernetesCorrelationDecisionsFallsBackToContainerImages proves a
// pod-template fact that carries no image_refs list still correlates the
// per-container image strings (the redaction-safe fallback path).
func TestBuildKubernetesCorrelationDecisionsFallsBackToContainerImages(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	workload := facts.Envelope{
		FactID:   "pod-no-refs",
		FactKind: facts.KubernetesPodTemplateFactKind,
		Payload: map[string]any{
			"cluster_id": testK8sCluster,
			"object_id":  objectID,
			"namespace":  testK8sNamespace,
			"name":       "checkout",
			"uid":        "uid-1",
			// No image_refs key; only per-container image strings.
			"containers": []any{
				map[string]any{"name": "app", "image": imageRef, "init": false},
			},
		},
	}
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		workload,
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
}

// TestBuildKubernetesCorrelationDecisionsDigestAlsoEvidencedByTagObservation
// proves a digest-named live reference resolves exact even when the source
// digest is only carried by a tag observation fact (not a standalone manifest).
func TestBuildKubernetesCorrelationDecisionsDigestAlsoEvidencedByTagObservation(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("oci-tag-1", testK8sRegistry, testK8sRepository, "v1", testK8sDigest, "", false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
}

// TestBuildKubernetesCorrelationDecisionsActiveManifestOverridesTombstone proves
// a digest seen active anywhere is not classified stale even if another
// observation of the same digest is tombstoned.
func TestBuildKubernetesCorrelationDecisionsActiveManifestOverridesTombstone(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-dead", testK8sRegistry, testK8sRepository, testK8sDigest, true),
		k8sSourceManifestFact("oci-live", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
}
