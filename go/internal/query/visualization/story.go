// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuildServiceStoryPacket derives a bounded visualization packet
// from an existing service-story dossier response. The response map is the same
// shape enrichServiceStoryDossierResponseWithContext produces: service_identity,
// upstream_dependencies, downstream_consumers, and evidence_graph. The builder
// is a pure transformation; it reads only fields already present in the
// response and performs no graph access, so it cannot surface anything the
// authorized story response did not already return.
//
// Node IDs are derived from the repository/service identity in the response,
// not from iteration order, so the same response always yields the same IDs.
// When the response carries no identity and no evidence graph, an explicit
// unsupported packet is returned with recommended next calls.
func BuildServiceStoryPacket(response map[string]any, truth *querycontract.TruthEnvelope) Packet {
	identity := querycontract.MapValue(response, "service_identity")
	evidenceGraph := querycontract.MapValue(response, "evidence_graph")
	upstream := querycontract.MapSliceValue(response, "upstream_dependencies")
	downstream := querycontract.MapValue(response, "downstream_consumers")

	if len(identity) == 0 && len(evidenceGraph) == 0 && len(upstream) == 0 && len(downstream) == 0 {
		return unsupportedVisualizationPacket(
			ViewServiceStory,
			truth,
			[]string{"service story response carried no identity, evidence graph, or dependency topology to visualize"},
			serviceStoryVisualizationNextCalls(),
		)
	}

	builder := newVisualizationBuilder(ViewServiceStory, serviceStoryTitle(identity))
	builder.SetTruth(truth)

	serviceNodeID := addServiceStoryServiceNode(builder, identity)
	addServiceStoryEvidenceGraph(builder, evidenceGraph, identity, serviceNodeID)
	addServiceStoryUpstreamEdges(builder, upstream, serviceNodeID)
	addServiceStoryDownstream(builder, downstream, serviceNodeID)

	packet := builder.Finalize()
	if querycontract.BoolVal(evidenceGraph, "truncated") || serviceStoryDownstreamTruncated(downstream) {
		packet.Truncation.Truncated = true
		if sourceEdgeCount := querycontract.IntVal(evidenceGraph, "edge_count"); sourceEdgeCount > len(querycontract.MapSliceValue(evidenceGraph, "edges")) {
			packet.Truncation.DroppedEdgeCount += sourceEdgeCount - len(querycontract.MapSliceValue(evidenceGraph, "edges"))
		}
		packet.Limitations = querycontract.AppendReason(packet.Limitations,
			"source story response was already truncated; visualized subgraph is a bounded subset")
	}
	return packet
}

func serviceStoryTitle(identity map[string]any) string {
	if name := strings.TrimSpace(querycontract.SafeStr(identity, "service_name")); name != "" {
		return name
	}
	return strings.TrimSpace(querycontract.SafeStr(identity, "service_id"))
}

// addServiceStoryServiceNode adds the central service node and returns its
// stable ID. The ID is derived from the service id (falling back to the name)
// so it is stable across responses. An empty identity yields no node and an
// empty anchor ID, which the dependency builders treat as "no anchor".
func addServiceStoryServiceNode(builder *visualizationBuilder, identity map[string]any) string {
	serviceID := strings.TrimSpace(querycontract.SafeStr(identity, "service_id"))
	serviceName := strings.TrimSpace(querycontract.SafeStr(identity, "service_name"))
	repoID := strings.TrimSpace(querycontract.SafeStr(identity, "repo_id"))
	anchor := querycontract.FirstNonEmptyString(serviceID, serviceName)
	if anchor == "" {
		return ""
	}
	nodeID := visualizationNodeID("service", anchor)
	node := Node{
		ID:       nodeID,
		Type:     "service",
		Label:    querycontract.FirstNonEmptyString(serviceName, serviceID),
		Category: "service",
		Role:     "workload",
	}
	if repoID != "" {
		node.EvidenceHandle = &querycontract.EvidenceCitationHandle{Kind: "entity", RepoID: repoID, EntityID: serviceID}
	}
	builder.AddNode(node)
	return nodeID
}

// addServiceStoryEvidenceGraph maps the dossier evidence_graph nodes and edges
// straight onto visualization nodes and edges. Each evidence-graph node already
// carries a stable id (repo/service id), so the visualization node ID is
// derived from it. Edges reference the same source/target repo ids.
func addServiceStoryEvidenceGraph(
	builder *visualizationBuilder,
	evidenceGraph map[string]any,
	identity map[string]any,
	serviceNodeID string,
) {
	var nodeIDOverrides map[string]string
	serviceID := strings.TrimSpace(querycontract.SafeStr(identity, "service_id"))
	serviceRepoID := strings.TrimSpace(querycontract.SafeStr(identity, "repo_id"))
	for _, node := range querycontract.MapSliceValue(evidenceGraph, "nodes") {
		rawID := strings.TrimSpace(querycontract.StringVal(node, "id"))
		if rawID == "" {
			continue
		}
		kind := querycontract.FirstNonEmptyString(querycontract.StringVal(node, "kind"), "repository")
		canonicalKey := strings.TrimSpace(querycontract.StringVal(node, "canonical_key"))
		nodeID := serviceStoryVisualizationNodeID(kind, rawID, canonicalKey)
		if serviceNodeID != "" && kind == "service" && rawID == serviceID {
			nodeID = serviceNodeID
			if nodeIDOverrides == nil {
				nodeIDOverrides = map[string]string{}
			}
			nodeIDOverrides[rawID] = nodeID
			continue
		}
		if kind != "repository" || canonicalKey != "" {
			if nodeIDOverrides == nil {
				nodeIDOverrides = map[string]string{}
			}
			nodeIDOverrides[rawID] = nodeID
		}
		handle := serviceStoryEvidenceHandle(kind, rawID, querycontract.StringVal(node, "repo_id"))
		builder.AddNode(Node{
			ID:             nodeID,
			Type:           kind,
			Label:          querycontract.FirstNonEmptyString(querycontract.StringVal(node, "label"), rawID),
			Category:       querycontract.StringVal(node, "category"),
			Role:           querycontract.StringVal(node, "role"),
			CanonicalKey:   canonicalKey,
			ScopeKey:       querycontract.StringVal(node, "scope_key"),
			EvidenceHandle: handle,
		})
	}
	for _, edge := range querycontract.MapSliceValue(evidenceGraph, "edges") {
		source := strings.TrimSpace(querycontract.StringVal(edge, "source"))
		target := strings.TrimSpace(querycontract.StringVal(edge, "target"))
		if source == "" || target == "" {
			continue
		}
		sourceNodeID := visualizationNodeID("repository", source)
		targetNodeID := visualizationNodeID("repository", target)
		if override := nodeIDOverrides[source]; override != "" {
			sourceNodeID = override
		}
		if override := nodeIDOverrides[target]; override != "" {
			targetNodeID = override
		}
		if serviceNodeID != "" && (target == serviceRepoID || target == serviceID) {
			targetNodeID = serviceNodeID
		}
		if serviceNodeID != "" && source == serviceID {
			sourceNodeID = serviceNodeID
		}
		builder.AddEdge(Edge{
			Source:       sourceNodeID,
			Target:       targetNodeID,
			Relationship: querycontract.FirstNonEmptyString(querycontract.StringVal(edge, "relationship_type"), "RELATED"),
			TruthLabel:   serviceStoryConfidenceLabel(edge),
		})
	}
}

// addServiceStoryUpstreamEdges adds edges from upstream source repositories to
// the service anchor. Upstream rows already carry source/target repo ids; nodes
// are added on demand so an upstream repo missing from the evidence graph still
// renders.
func addServiceStoryUpstreamEdges(builder *visualizationBuilder, upstream []map[string]any, serviceNodeID string) {
	for _, row := range upstream {
		sourceID := strings.TrimSpace(querycontract.StringVal(row, "source_repo_id"))
		if sourceID == "" {
			continue
		}
		canonicalKey := strings.TrimSpace(querycontract.StringVal(row, "source_repo_canonical_id"))
		sourceNodeID := serviceStoryVisualizationNodeID("repository", sourceID, canonicalKey)
		handle := serviceStoryRepoHandle("repository", sourceID)
		builder.AddNode(Node{
			ID:             sourceNodeID,
			Type:           "repository",
			Label:          querycontract.FirstNonEmptyString(querycontract.StringVal(row, "source"), sourceID),
			Category:       "deployment",
			Role:           "deployment_configuration",
			CanonicalKey:   canonicalKey,
			ScopeKey:       querycontract.StringVal(row, "source_repo_scope_key"),
			EvidenceHandle: handle,
		})
		targetNodeID := serviceNodeID
		if targetNodeID == "" {
			continue
		}
		builder.AddEdge(Edge{
			Source:       sourceNodeID,
			Target:       targetNodeID,
			Relationship: querycontract.FirstNonEmptyString(querycontract.StringVal(row, "relationship_type"), "DEPENDS_ON"),
			TruthLabel:   serviceStoryConfidenceLabel(row),
		})
	}
}

// addServiceStoryDownstream adds downstream consumer repository nodes and edges
// from the service anchor to each consumer. It reads the graph_dependents and
// content_consumers rows the dossier already bounded.
func addServiceStoryDownstream(builder *visualizationBuilder, downstream map[string]any, serviceNodeID string) {
	if serviceNodeID == "" {
		return
	}
	rows := append([]map[string]any{}, querycontract.MapSliceValue(downstream, "graph_dependents")...)
	rows = append(rows, querycontract.MapSliceValue(downstream, "content_consumers")...)
	for _, row := range rows {
		repoID := strings.TrimSpace(querycontract.StringVal(row, "repo_id"))
		if repoID == "" {
			continue
		}
		nodeID := visualizationNodeID("repository", repoID)
		builder.AddNode(Node{
			ID:             nodeID,
			Type:           "repository",
			Label:          querycontract.FirstNonEmptyString(querycontract.StringVal(row, "repository"), repoID),
			Category:       "downstream",
			Role:           "downstream_consumer",
			EvidenceHandle: serviceStoryRepoHandle("repository", repoID),
		})
		builder.AddEdge(Edge{
			Source:       serviceNodeID,
			Target:       nodeID,
			Relationship: "CONSUMED_BY",
		})
	}
}

func serviceStoryVisualizationNodeID(kind, rawID, canonicalKey string) string {
	identity := strings.TrimSpace(rawID)
	if kind == "repository" && strings.TrimSpace(canonicalKey) != "" {
		identity = strings.TrimSpace(canonicalKey)
	}
	return visualizationNodeID(kind, identity)
}

func serviceStoryEvidenceHandle(kind, id, repoID string) *querycontract.EvidenceCitationHandle {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if kind == "repository" || kind == "service" {
		return serviceStoryRepoHandle(kind, id)
	}
	return &querycontract.EvidenceCitationHandle{
		Kind:           "entity",
		RepoID:         strings.TrimSpace(repoID),
		EntityID:       id,
		EvidenceFamily: kind,
	}
}

func serviceStoryDownstreamTruncated(downstream map[string]any) bool {
	return querycontract.BoolVal(downstream, "truncated")
}

// serviceStoryRepoHandle returns an evidence_citation handle for a repository or
// service node, so a rendered node maps back to the citation handle shape. It is
// derived only from the node id already present in the response.
func serviceStoryRepoHandle(kind, id string) *querycontract.EvidenceCitationHandle {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return &querycontract.EvidenceCitationHandle{Kind: "entity", RepoID: id, EntityID: id, EvidenceFamily: kind}
}

// serviceStoryConfidenceLabel folds a relationship confidence into a truth-style
// label, reusing the answer-packet truth vocabulary. It never invents truth
// beyond the confidence the source row already carried.
func serviceStoryConfidenceLabel(row map[string]any) string {
	confidence := querycontract.FloatVal(row, "confidence")
	switch {
	case confidence <= 0:
		return ""
	case confidence >= 0.85:
		return string(querycontract.TruthLevelExact)
	case confidence >= 0.5:
		return string(querycontract.TruthLevelDerived)
	default:
		return string(querycontract.TruthLevelFallback)
	}
}

func serviceStoryVisualizationNextCalls() []map[string]any {
	return []map[string]any{
		{
			"tool":   "get_service_story",
			"reason": "fetch a service story dossier with evidence graph and dependency topology before visualizing",
		},
	}
}
