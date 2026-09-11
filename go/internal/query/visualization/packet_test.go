// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func nodeIDSet(packet Packet) []string {
	ids := make([]string, 0, len(packet.Nodes))
	for _, node := range packet.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func TestServiceStoryVisualizationDeterministicOrdering(t *testing.T) {
	forward := []string{"r3", "r1", "r5", "r2", "r4"}
	reverse := []string{"r4", "r2", "r5", "r1", "r3"}

	packetA := BuildServiceStoryPacket(querytestutil.StoryResponseWithUpstream(forward), querytestutil.FreshTruth())
	packetB := BuildServiceStoryPacket(querytestutil.StoryResponseWithUpstream(reverse), querytestutil.FreshTruth())

	idsA := nodeIDSet(packetA)
	idsB := nodeIDSet(packetB)
	if len(idsA) == 0 {
		t.Fatalf("expected nodes, got none")
	}
	if fmt.Sprint(idsA) != fmt.Sprint(idsB) {
		t.Fatalf("node IDs not order-independent:\n a=%v\n b=%v", idsA, idsB)
	}
	for i := 1; i < len(idsA); i++ {
		if idsA[i-1] >= idsA[i] {
			t.Fatalf("nodes not sorted by stable ID: %v", idsA)
		}
	}
	for i := 1; i < len(packetA.Edges); i++ {
		if packetA.Edges[i-1].ID >= packetA.Edges[i].ID {
			t.Fatalf("edges not sorted by stable ID: %v", packetA.Edges)
		}
	}
	if packetA.Limits.Ordering != "stable_id" {
		t.Fatalf("expected stable_id ordering, got %q", packetA.Limits.Ordering)
	}
}

func TestServiceStoryVisualizationStableIDsAcrossRuns(t *testing.T) {
	resp := querytestutil.StoryResponseWithUpstream([]string{"r1", "r2"})
	first := BuildServiceStoryPacket(resp, querytestutil.FreshTruth())
	second := BuildServiceStoryPacket(querytestutil.StoryResponseWithUpstream([]string{"r2", "r1"}), querytestutil.FreshTruth())
	if fmt.Sprint(nodeIDSet(first)) != fmt.Sprint(nodeIDSet(second)) {
		t.Fatalf("stable IDs differ across runs")
	}
}

func TestServiceStoryVisualizationTruncatesNodes(t *testing.T) {
	// The response yields 2 fixed nodes (service anchor + the svc-repo
	// evidence-graph node), so MaxNodes-1 distinct upstream repos push the
	// total to MaxNodes+1 and force exactly one node to be dropped.
	ids := make([]string, 0, MaxNodes)
	for i := 0; i < MaxNodes-1; i++ {
		ids = append(ids, fmt.Sprintf("repo-%04d", i))
	}
	packet := BuildServiceStoryPacket(querytestutil.StoryResponseWithUpstream(ids), querytestutil.FreshTruth())

	if len(packet.Nodes) != MaxNodes {
		t.Fatalf("expected %d nodes after truncation, got %d", MaxNodes, len(packet.Nodes))
	}
	if !packet.Truncation.Truncated {
		t.Fatalf("expected truncation marker set")
	}
	if packet.Truncation.DroppedNodeCount != 1 {
		t.Fatalf("expected exactly 1 dropped node, got %d", packet.Truncation.DroppedNodeCount)
	}
	if len(packet.Truncation.DroppedNodeIDs) != 1 {
		t.Fatalf("expected 1 dropped node id recorded, got %d", len(packet.Truncation.DroppedNodeIDs))
	}
	if packet.Limits.NodeCount != MaxNodes {
		t.Fatalf("limits node count mismatch: %d", packet.Limits.NodeCount)
	}
	// No edge may dangle: every edge endpoint must be a retained node.
	retained := map[string]struct{}{}
	for _, node := range packet.Nodes {
		retained[node.ID] = struct{}{}
	}
	for _, edge := range packet.Edges {
		if _, ok := retained[edge.Source]; !ok {
			t.Fatalf("edge source %s not a retained node", edge.Source)
		}
		if _, ok := retained[edge.Target]; !ok {
			t.Fatalf("edge target %s not a retained node", edge.Target)
		}
	}
}

func TestServiceStoryVisualizationPrivacyInvariant(t *testing.T) {
	// The response intentionally omits repo names and confidence so the packet
	// must not invent labels or truth labels beyond what the source carried.
	resp := map[string]any{
		"service_identity": map[string]any{"service_id": "svc-1", "repo_id": "svc-repo"},
		"upstream_dependencies": []map[string]any{
			{"source_repo_id": "up-1", "target_repo_id": "svc-repo", "relationship_type": "DEPENDS_ON"},
		},
		"downstream_consumers": map[string]any{},
	}
	packet := BuildServiceStoryPacket(resp, querytestutil.FreshTruth())
	checkedRepository := false
	for _, node := range packet.Nodes {
		// Labels fall back to the id the response carried; never to a fabricated name.
		if node.Type == "repository" && node.Role == "deployment_configuration" {
			checkedRepository = true
			if node.Label != "up-1" {
				t.Fatalf("label fabricated for deployment repository: %q", node.Label)
			}
		}
	}
	if !checkedRepository {
		t.Fatal("privacy assertion did not inspect the deployment repository")
	}
	for _, edge := range packet.Edges {
		if edge.TruthLabel != "" {
			t.Fatalf("truth label invented with no source confidence: %q", edge.TruthLabel)
		}
	}
}

func TestServiceStoryVisualizationReconcilesCanonicalRepositoryObservations(t *testing.T) {
	response := map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:api-node-boats",
			"service_name": "api-node-boats",
			"repo_id":      "repository:r_service",
		},
		"evidence_graph": map[string]any{
			"nodes": []map[string]any{
				{
					"id":       "workload:api-node-boats",
					"label":    "api-node-boats",
					"kind":     "service",
					"category": "service",
					"role":     "workload",
				},
				{
					"id":       "repository:r_service",
					"label":    "api-node-boats",
					"kind":     "repository",
					"category": "source",
					"role":     "source_repository",
				},
				{
					"id":            "repository:r_argocd_observation_a",
					"label":         "iac-eks-argocd",
					"kind":          "repository",
					"category":      "deployment",
					"role":          "deployment_configuration",
					"canonical_key": "repository:r_argocd_canonical",
					"scope_key":     "scope:s_a",
				},
				{
					"id":            "repository:r_argocd_observation_b",
					"label":         "iac-eks-argocd",
					"kind":          "repository",
					"category":      "deployment",
					"role":          "deployment_configuration",
					"canonical_key": "repository:r_argocd_canonical",
					"scope_key":     "scope:s_b",
				},
				{
					"id":       "runtime:api-node-boats:eks-prod",
					"label":    "eks-prod",
					"kind":     "runtime",
					"category": "runtime",
					"role":     "runtime_instance",
					"repo_id":  "repository:r_service",
				},
			},
			"edges": []map[string]any{
				{
					"source":            "repository:r_argocd_observation_a",
					"target":            "repository:r_service",
					"relationship_type": "PROVISIONING_SOURCE_CHAIN",
					"confidence":        0.9,
				},
				{
					"source":            "repository:r_argocd_observation_b",
					"target":            "repository:r_service",
					"relationship_type": "PROVISIONING_SOURCE_CHAIN",
					"confidence":        0.9,
				},
				{
					"source":            "workload:api-node-boats",
					"target":            "runtime:api-node-boats:eks-prod",
					"relationship_type": "RUNS_AS",
				},
			},
		},
		"upstream_dependencies": []map[string]any{},
		"downstream_consumers":  map[string]any{},
	}

	packet := BuildServiceStoryPacket(response, querytestutil.FreshTruth())

	serviceNodes := 0
	canonicalRepositories := 0
	var canonicalNode Node
	for _, node := range packet.Nodes {
		if node.Role == "workload" && node.Type == "service" {
			serviceNodes++
		}
		if node.Type == "repository" && node.CanonicalKey == "repository:r_argocd_canonical" {
			canonicalRepositories++
			canonicalNode = node
		}
	}
	if serviceNodes != 1 {
		t.Fatalf("workload service anchors = %d, want exactly 1", serviceNodes)
	}
	if canonicalRepositories != 1 {
		t.Fatalf("canonical Argo repositories = %d, want 1", canonicalRepositories)
	}
	if len(canonicalNode.EvidenceHandles) != 2 {
		t.Fatalf("canonical Argo evidence handles = %d, want 2 observation handles", len(canonicalNode.EvidenceHandles))
	}
	serviceNodeID := visualizationNodeID("service", "workload:api-node-boats")
	for _, edge := range packet.Edges {
		if edge.Relationship == "PROVISIONING_SOURCE_CHAIN" && edge.Target != serviceNodeID {
			t.Fatalf("provisioning edge target = %q, want workload anchor %q", edge.Target, serviceNodeID)
		}
	}
}

func TestServiceStoryVisualizationDoesNotMergeEqualLabelsWithoutCanonicalKey(t *testing.T) {
	response := map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:api-node-boats",
			"service_name": "api-node-boats",
		},
		"evidence_graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "repository:r_a", "label": "iac-eks-argocd", "kind": "repository", "category": "deployment", "role": "deployment_configuration", "scope_key": "scope:s_a"},
				{"id": "repository:r_b", "label": "iac-eks-argocd", "kind": "repository", "category": "deployment", "role": "deployment_configuration", "scope_key": "scope:s_b"},
			},
		},
	}

	packet := BuildServiceStoryPacket(response, querytestutil.FreshTruth())
	observations := 0
	for _, node := range packet.Nodes {
		if node.Label == "iac-eks-argocd" {
			observations++
			if node.ScopeKey == "" {
				t.Fatalf("unreconciled repository observation missing scope disambiguation: %+v", node)
			}
		}
	}
	if observations != 2 {
		t.Fatalf("unreconciled Argo observations = %d, want 2", observations)
	}
}

func TestServiceStoryVisualizationUnsupported(t *testing.T) {
	packet := BuildServiceStoryPacket(map[string]any{}, querytestutil.FreshTruth())
	if packet.Supported {
		t.Fatalf("expected unsupported packet for empty response")
	}
	if packet.View != ViewUnsupported {
		t.Fatalf("expected unsupported view, got %q", packet.View)
	}
	if len(packet.RecommendedNextCalls) == 0 {
		t.Fatalf("expected recommended next calls on unsupported packet")
	}
	if len(packet.Limitations) == 0 {
		t.Fatalf("expected limitations on unsupported packet")
	}
}

func TestEvidenceCitationVisualizationDeterministicOrdering(t *testing.T) {
	forward := querytestutil.CitationResponse([]string{"e3", "e1", "e2"})
	reverse := querytestutil.CitationResponse([]string{"e2", "e3", "e1"})
	packetA := BuildEvidenceCitationPacket(forward, querytestutil.FreshTruth())
	packetB := BuildEvidenceCitationPacket(reverse, querytestutil.FreshTruth())
	if fmt.Sprint(nodeIDSet(packetA)) != fmt.Sprint(nodeIDSet(packetB)) {
		t.Fatalf("citation node IDs not order independent")
	}
	for i := 1; i < len(packetA.Nodes); i++ {
		if packetA.Nodes[i-1].ID >= packetA.Nodes[i].ID {
			t.Fatalf("citation nodes not sorted by stable ID")
		}
	}
}

func TestEvidenceCitationVisualizationPrivacyInvariant(t *testing.T) {
	resp := querytestutil.CitationResponse([]string{"e1"})
	packet := BuildEvidenceCitationPacket(resp, querytestutil.FreshTruth())
	if len(packet.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(packet.Nodes))
	}
	node := packet.Nodes[0]
	// The packet must carry only handle-shaped fields, never the citation excerpt.
	if node.EvidenceHandle == nil {
		t.Fatalf("expected evidence handle on citation node")
	}
	if node.EvidenceHandle.EntityID != "e1" {
		t.Fatalf("handle entity id mismatch: %q", node.EvidenceHandle.EntityID)
	}
	if node.Label == "secret excerpt body" {
		t.Fatalf("excerpt leaked into node label")
	}
}

func TestEvidenceCitationVisualizationTruncates(t *testing.T) {
	ids := make([]string, 0, MaxNodes+5)
	for i := 0; i < MaxNodes+5; i++ {
		ids = append(ids, fmt.Sprintf("e%04d", i))
	}
	packet := BuildEvidenceCitationPacket(querytestutil.CitationResponse(ids), querytestutil.FreshTruth())
	if len(packet.Nodes) != MaxNodes {
		t.Fatalf("expected %d nodes, got %d", MaxNodes, len(packet.Nodes))
	}
	if !packet.Truncation.Truncated {
		t.Fatalf("expected truncation marker")
	}
	if packet.Truncation.DroppedNodeCount != 5 {
		t.Fatalf("expected 5 dropped nodes, got %d", packet.Truncation.DroppedNodeCount)
	}
}

func TestEvidenceCitationVisualizationUnsupported(t *testing.T) {
	packet := BuildEvidenceCitationPacket(querycontract.EvidenceCitationResponse{}, querytestutil.FreshTruth())
	if packet.Supported {
		t.Fatalf("expected unsupported packet for empty citations")
	}
	if len(packet.RecommendedNextCalls) == 0 {
		t.Fatalf("expected recommended next calls")
	}
}

func TestIncidentVisualizationDeterministicAndTruthLabels(t *testing.T) {
	slots := []model.IncidentEvidenceSlot{model.IncidentSlotIncident, model.IncidentSlotService, model.IncidentSlotDeployable}
	packet := BuildIncidentContextPacket(querytestutil.IncidentResponse(slots), querytestutil.FreshTruth())

	if len(packet.Nodes) != len(slots)+1 {
		t.Fatalf("expected incident anchor + %d slot nodes, got %d", len(slots), len(packet.Nodes))
	}
	for i := 1; i < len(packet.Nodes); i++ {
		if packet.Nodes[i-1].ID >= packet.Nodes[i].ID {
			t.Fatalf("incident nodes not sorted by stable ID")
		}
	}
	// Truth labels must come straight from the source evidence edges.
	foundSlotTruth := false
	for _, node := range packet.Nodes {
		if node.Type == "evidence_slot" {
			if node.TruthLabel != string(model.IncidentTruthExact) {
				t.Fatalf("slot truth label not carried from source: %q", node.TruthLabel)
			}
			foundSlotTruth = true
		}
	}
	if !foundSlotTruth {
		t.Fatalf("expected at least one evidence_slot node")
	}
	if len(packet.Edges) == 0 {
		t.Fatalf("expected evidence-path edges")
	}
	for _, edge := range packet.Edges {
		if edge.TruthLabel != string(model.IncidentTruthExact) {
			t.Fatalf("edge truth label not carried from source: %q", edge.TruthLabel)
		}
	}
}

func TestIncidentVisualizationUnsupported(t *testing.T) {
	packet := BuildIncidentContextPacket(model.IncidentContextResponse{}, querytestutil.FreshTruth())
	if packet.Supported {
		t.Fatalf("expected unsupported packet with no evidence path")
	}
	if len(packet.RecommendedNextCalls) == 0 {
		t.Fatalf("expected recommended next calls")
	}
}

func TestVisualizationPacketPreservesTruth(t *testing.T) {
	packet := BuildServiceStoryPacket(querytestutil.StoryResponseWithUpstream([]string{"r1"}), querytestutil.FreshTruth())
	if packet.Truth == nil {
		t.Fatalf("expected truth envelope preserved")
	}
	if packet.Truth.Level != querycontract.TruthLevelExact || packet.Truth.Basis != querycontract.TruthBasisAuthoritativeGraph {
		t.Fatalf("truth envelope not copied verbatim: %+v", packet.Truth)
	}
}
