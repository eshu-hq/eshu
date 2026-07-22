// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildKubernetesCorrelationDecisionsIdentityEdgeUsesRelationshipClusterID
// is the issue #5437 root-cause regression: an identity-edge (owner_reference
// or selector_match) decision's ClusterID must come from the
// kubernetes_live.relationship fact's own cluster_id field, not be
// reconstructed by string-parsing fromObjectID for a "k8s://<cluster>/..."
// scheme.
//
// Real collector object ids (kuberneteslive/identity.go ObjectID(), an opaque
// sha256 via facts.StableID) and the cassette ids
// (kubernetes_live:supply-chain-demo:...) are never k8s://-prefixed — only
// hand-authored unit-test fixtures elsewhere in this package use that scheme.
// This test uses a REAL-style opaque fromObjectID so it fails against the old
// string-parse derivation (which returns "") and proves the relationship
// fact's own cluster_id is carried through instead. Without this fix,
// identity-edge correlations are written with cluster_id="" and silently
// dropped by the cluster_id-filtered query surfaces
// (GET /api/v0/kubernetes/correlations, list_kubernetes_correlations).
func TestBuildKubernetesCorrelationDecisionsIdentityEdgeUsesRelationshipClusterID(t *testing.T) {
	t.Parallel()

	const wantClusterID = "supply-chain-demo"

	tests := []struct {
		name    string
		relType string
	}{
		{name: "owner_reference", relType: relKubernetesOwnerReference},
		{name: "selector_match", relType: relKubernetesSelectorMatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Opaque object ids mirror the real collector identity (a stable-id
			// hash), never a "k8s://<cluster>/..." string. The old
			// edgeClusterID string-parse derivation returns "" for ids in this
			// shape even though the relationship fact itself carries the real
			// cluster_id.
			fromID := "sha256:" + tc.name + "-from-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			toID := "sha256:" + tc.name + "-to-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

			envelope := facts.Envelope{
				FactID:   "rel-opaque-" + tc.name,
				FactKind: facts.KubernetesRelationshipFactKind,
				Payload: map[string]any{
					"cluster_id":          wantClusterID,
					"relationship_type":   tc.relType,
					"from_object_id":      fromID,
					"to_object_id":        toID,
					"correlation_anchors": []string{fromID, toID},
				},
			}

			decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{envelope})

			var identity *KubernetesCorrelationDecision
			for i := range decisions {
				if decisions[i].IdentityEdgeKey != "" && decisions[i].RelationshipType == tc.relType {
					identity = &decisions[i]
				}
			}
			if identity == nil {
				t.Fatalf("expected a %s identity decision; decisions=%+v", tc.relType, decisions)
			}
			if identity.ClusterID != wantClusterID {
				t.Fatalf(
					"ClusterID = %q, want %q (the relationship fact's own cluster_id, not derived from fromObjectID)",
					identity.ClusterID, wantClusterID,
				)
			}
		})
	}
}
