// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// TestBuildKubernetesCorrelationDecisionsSelectorMatchFromRealCollectorFact is
// the issue #5437 collector -> reducer contract proof: a
// kubernetes_live.relationship payload built through the REAL shared schema
// encoder (factschema.EncodeKubernetesLiveRelationship, the exact function
// kuberneteslive.NewRelationshipEnvelope calls), not a hand-typed
// map[string]any fixture, decodes through ingestRelationship's generic
// relationship handling and lands as an ambiguous, provenance-only,
// never-exact decision.
//
// This test cannot import go/internal/collector/kuberneteslive directly:
// that package imports go/internal/collector, which imports
// go/internal/workflow, which imports go/internal/reducer, so importing it
// from a reducer test is an import cycle. Going through
// sdk/go/factschema + kuberneteslivev1 instead still exercises the real,
// shared typed encode/decode contract both sides use — the actual seam this
// test is proving — rather than re-implementing it as a hand-built payload
// map like TestBuildKubernetesCorrelationDecisionsSelectorAmbiguityNeverPromoted
// in kubernetes_correlation_test.go does (that test proves the classification
// rule in isolation; this one proves the collector's real encoder and the
// reducer's real decoder agree on the wire shape).
func TestBuildKubernetesCorrelationDecisionsSelectorMatchFromRealCollectorFact(t *testing.T) {
	t.Parallel()

	clusterID := testK8sCluster
	fromID := "k8s://" + clusterID + "/v1/services/" + testK8sNamespace + "/checkout-svc"
	toID := "k8s://" + clusterID + "/v1/pods/" + testK8sNamespace + "/checkout-abc12"
	fromGVR := "core/v1/services"
	toGVR := "core/v1/pods"

	payload, err := factschema.EncodeKubernetesLiveRelationship(kuberneteslivev1.Relationship{
		RelationshipType:         relKubernetesSelectorMatch,
		FromObjectID:             fromID,
		ToObjectID:               toID,
		ClusterID:                &clusterID,
		FromGroupVersionResource: &fromGVR,
		ToGroupVersionResource:   &toGVR,
		CorrelationAnchors:       []string{fromID, toID},
	})
	if err != nil {
		t.Fatalf("factschema.EncodeKubernetesLiveRelationship() error = %v", err)
	}
	envelope := facts.Envelope{
		FactID:   "rel-selector-match-real",
		FactKind: facts.KubernetesRelationshipFactKind,
		Payload:  payload,
	}

	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{envelope})

	var selector *KubernetesCorrelationDecision
	for i := range decisions {
		if decisions[i].RelationshipType == relKubernetesSelectorMatch {
			selector = &decisions[i]
		}
	}
	if selector == nil {
		t.Fatalf("expected a selector_match identity decision from the real-encoder fact; decisions=%+v", decisions)
	}
	assertKubernetesOutcome(t, *selector, KubernetesCorrelationAmbiguous, driftUnknown)
	if !selector.ProvenanceOnly {
		t.Fatal("selector match from real-encoder fact: ProvenanceOnly = false, want true")
	}
	if selector.NonPromotion == "" {
		t.Fatal("selector match from real-encoder fact: NonPromotion is empty, want a recorded non-promotion reason")
	}
	if selector.Outcome == KubernetesCorrelationExact {
		t.Fatal("selector match from real-encoder fact must never be promoted to exact")
	}
}
