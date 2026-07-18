// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"reflect"
	"testing"
)

func TestServiceStoryVisualizationCanonicalCollapseIsOrderIndependent(t *testing.T) {
	buildResponse := func(reverse bool) map[string]any {
		nodes := []map[string]any{
			{
				"id":            "repository:observation-a",
				"label":         "iac-eks-argocd",
				"kind":          "repository",
				"category":      "deployment",
				"role":          "deployment_configuration",
				"canonical_key": "repository:r_argocd",
				"scope_key":     "scope:s_b",
			},
			{
				"id":            "repository:observation-b",
				"label":         "argocd-control-plane",
				"kind":          "repository",
				"category":      "source",
				"role":          "source_repository",
				"canonical_key": "repository:r_argocd",
				"scope_key":     "scope:s_a",
			},
		}
		edges := []map[string]any{
			{
				"source":            "repository:observation-a",
				"target":            "repository:r_service",
				"relationship_type": "PROVISIONING_SOURCE_CHAIN",
				"confidence":        0.4,
			},
			{
				"source":            "repository:observation-b",
				"target":            "repository:r_service",
				"relationship_type": "PROVISIONING_SOURCE_CHAIN",
				"confidence":        0.9,
			},
		}
		if reverse {
			nodes[0], nodes[1] = nodes[1], nodes[0]
			edges[0], edges[1] = edges[1], edges[0]
		}
		return map[string]any{
			"service_identity": map[string]any{
				"service_id":   "workload:api-node-boats",
				"service_name": "api-node-boats",
				"repo_id":      "repository:r_service",
			},
			"evidence_graph": map[string]any{"nodes": nodes, "edges": edges},
		}
	}

	forward := BuildServiceStoryVisualizationPacket(buildResponse(false), freshTruth())
	reverse := BuildServiceStoryVisualizationPacket(buildResponse(true), freshTruth())
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("canonical collapse depends on observation order:\nforward=%+v\nreverse=%+v", forward, reverse)
	}

	var canonical VisualizationNode
	for _, node := range forward.Nodes {
		if node.CanonicalKey == "repository:r_argocd" {
			canonical = node
			break
		}
	}
	if got, want := canonical.Role, "source_repository"; got != want {
		t.Fatalf("canonical primary role = %q, want %q", got, want)
	}
	if got, want := fmt.Sprint(canonical.Roles), "[source_repository deployment_configuration]"; got != want {
		t.Fatalf("canonical roles = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(canonical.ScopeKeys), "[scope:s_a scope:s_b]"; got != want {
		t.Fatalf("canonical scope keys = %s, want %s", got, want)
	}
	if len(forward.Edges) != 1 || forward.Edges[0].TruthLabel != string(TruthLevelExact) {
		t.Fatalf("canonical edge truth = %+v, want one exact edge", forward.Edges)
	}
}

func TestServiceStoryVisualizationCarriesKnownSourceDroppedEdgeCount(t *testing.T) {
	response := map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:api-node-boats",
			"service_name": "api-node-boats",
		},
		"evidence_graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "repository:r_config", "kind": "repository", "label": "helm-charts"},
			},
			"edges":      []map[string]any{},
			"edge_count": 3,
			"truncated":  true,
		},
	}

	packet := BuildServiceStoryVisualizationPacket(response, freshTruth())
	if !packet.Truncation.Truncated {
		t.Fatal("source-truncated story must remain truncated")
	}
	if got, want := packet.Truncation.DroppedEdgeCount, 3; got != want {
		t.Fatalf("source dropped edge count = %d, want %d", got, want)
	}
}
