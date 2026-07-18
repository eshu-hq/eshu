// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic stable node/edge ID digest for visualization identity, not a security primitive
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	// VisualizationMaxNodes bounds the number of nodes a visualization packet
	// may carry. Nodes beyond the limit are dropped and recorded in the
	// truncation block so a client can render a bounded, explainable subgraph
	// without unbounded payloads.
	VisualizationMaxNodes = 60
	// VisualizationMaxEdges bounds the number of edges a visualization packet
	// may carry. Edges beyond the limit are dropped and recorded in the
	// truncation block.
	VisualizationMaxEdges = 120
)

// VisualizationView names the derived-view family a packet was built from. It
// lets a client tell a service-story subgraph apart from an evidence-citation
// or incident-context subgraph without inspecting node shapes.
type VisualizationView string

const (
	// VisualizationViewServiceStory is the service-story dossier subgraph:
	// service identity plus upstream/downstream repository topology and the
	// dossier evidence_graph.
	VisualizationViewServiceStory VisualizationView = "service_story"
	// VisualizationViewEvidenceCitation is the evidence-citation subgraph: one
	// node per resolved citation handle, with no synthetic edges.
	VisualizationViewEvidenceCitation VisualizationView = "evidence_citation"
	// VisualizationViewIncidentContext is the incident-context subgraph: the
	// incident anchor plus one node per evidence-path slot and an edge per slot
	// transition.
	VisualizationViewIncidentContext VisualizationView = "incident_context"
	// VisualizationViewGraphQuery is the subgraph projected from the graph
	// nodes, relationships, and paths returned by an executed read-only Cypher
	// query. Unlike the other views it is derived from a live graph read rather
	// than from a prior answer response.
	VisualizationViewGraphQuery VisualizationView = "graph_query"
	// VisualizationViewUnsupported marks a packet that could not be derived from
	// the given response. It carries limitations and recommended next calls
	// instead of nodes and edges.
	VisualizationViewUnsupported VisualizationView = "unsupported"
)

// VisualizationNode is one bounded node in a visualization packet. Its ID is
// derived deterministically from the underlying entity or handle (never from
// iteration order), so the same source response always yields the same node ID.
type VisualizationNode struct {
	// ID is the stable, deterministic node identifier. It is derived from the
	// underlying entity/handle identity (repo id, entity id, slot, or
	// repo_id+relative_path), hashed into a short opaque token. Equal source
	// identities always produce the same ID.
	ID string `json:"id"`
	// Type classifies the node (service, repository, citation, incident,
	// evidence_slot).
	Type string `json:"type"`
	// Label is a bounded human-readable label for rendering.
	Label string `json:"label"`
	// Category is an optional sub-classification (source, deployment, service,
	// runtime, downstream, or an incident evidence slot) that a client may use
	// for layout or color.
	Category string `json:"category,omitempty"`
	// Role describes what the source evidence proves this node does in the
	// service story. It keeps a workload anchor distinct from source-code,
	// deployment-configuration, runtime, and downstream repository nodes.
	Role string `json:"role,omitempty"`
	// Roles preserves every source-proven role when canonical repository
	// observations reconcile to one node. Role remains the deterministic primary
	// role used by compatibility clients and layout.
	Roles []string `json:"roles,omitempty"`
	// CanonicalKey is a privacy-safe repository identity supplied by the source
	// evidence. Repository observations are reconciled only when this key is
	// non-empty and equal; display labels are never identity.
	CanonicalKey string `json:"canonical_key,omitempty"`
	// ScopeKey is a privacy-safe observation-scope discriminator. It remains
	// visible when canonical identity is unavailable so equal labels are not
	// presented as unexplained duplicates.
	ScopeKey string `json:"scope_key,omitempty"`
	// ScopeKeys preserves every privacy-safe observation scope represented by a
	// reconciled repository node. ScopeKey remains the deterministic first value
	// for compatibility clients.
	ScopeKeys []string `json:"scope_keys,omitempty"`
	// TruthLabel is an optional per-node truth label, carried straight from the
	// source response (for example an incident evidence-path truth label). It is
	// never synthesized here.
	TruthLabel string `json:"truth_label,omitempty"`
	// EvidenceHandle is an optional reference back to an evidence_citation
	// handle, so a rendered node maps to the citation that hydrates it. It is
	// populated only when the source response already carried the handle fields.
	EvidenceHandle *evidenceCitationHandle `json:"evidence_handle,omitempty"`
	// EvidenceHandles preserves every supported observation handle when several
	// repository observations reconcile to one canonical visualization node.
	EvidenceHandles []evidenceCitationHandle `json:"evidence_handles,omitempty"`
}

// VisualizationEdge is one bounded edge in a visualization packet. Source and
// Target are stable node IDs that match VisualizationNode.ID values in the same
// packet.
type VisualizationEdge struct {
	// ID is the stable, deterministic edge identifier, derived from the source
	// and target node IDs plus the relationship label.
	ID string `json:"id"`
	// Source is the stable ID of the originating node.
	Source string `json:"source"`
	// Target is the stable ID of the destination node.
	Target string `json:"target"`
	// Relationship is the relationship label (for example a graph relationship
	// type or an incident slot transition).
	Relationship string `json:"relationship"`
	// TruthLabel is an optional truth label carried from the source response (an
	// incident evidence truth label or a relationship confidence-derived label).
	// It is never synthesized beyond what the source provided.
	TruthLabel string `json:"truth_label,omitempty"`
	// EvidenceHandle is an optional reference back to an evidence_citation
	// handle for the relationship, when the source response carried one.
	EvidenceHandle *evidenceCitationHandle `json:"evidence_handle,omitempty"`
	// EvidenceHandles preserves every supported relationship handle when
	// canonical endpoint reconciliation collapses duplicate evidence edges.
	EvidenceHandles []evidenceCitationHandle `json:"evidence_handles,omitempty"`
}

// VisualizationLimits states the payload bounds and observed counts for a
// packet so a client can show "showing N of M" without recomputing.
type VisualizationLimits struct {
	// MaxNodes is the node bound applied (VisualizationMaxNodes).
	MaxNodes int `json:"max_nodes"`
	// MaxEdges is the edge bound applied (VisualizationMaxEdges).
	MaxEdges int `json:"max_edges"`
	// Ordering names the deterministic ordering contract ("stable_id").
	Ordering string `json:"ordering"`
	// NodeCount is the number of nodes retained in the packet.
	NodeCount int `json:"node_count"`
	// EdgeCount is the number of edges retained in the packet.
	EdgeCount int `json:"edge_count"`
}

// VisualizationTruncation records what a packet dropped to stay within bounds,
// so truncation is explicit and auditable rather than silent.
type VisualizationTruncation struct {
	// Truncated is true when any node or edge was dropped.
	Truncated bool `json:"truncated"`
	// DroppedNodeCount is how many nodes exceeded MaxNodes and were dropped.
	DroppedNodeCount int `json:"dropped_node_count"`
	// DroppedEdgeCount is how many edges were dropped: edges over MaxEdges plus
	// edges whose endpoints were dropped with the over-limit nodes.
	DroppedEdgeCount int `json:"dropped_edge_count"`
	// DroppedNodeIDs lists the stable IDs of dropped nodes, sorted, so a client
	// can name what is missing. It is itself bounded by MaxNodes.
	DroppedNodeIDs []string `json:"dropped_node_ids,omitempty"`
}

// VisualizationPacket is a compact, bounded, derived view of an existing
// story, evidence-citation, or incident-context query response. It exposes a
// renderable subgraph (nodes and edges) with stable identifiers, the source
// truth/freshness metadata, payload limits, and explicit truncation, so a
// client can draw an explainable subgraph without raw Cypher or a
// Neo4j-browser-only workflow.
//
// A visualization packet is a pure transformation of data the caller already
// received from an authorized query response. It performs no graph access and
// surfaces no field beyond what the source response already contained. It is a
// sibling of the AnswerPacket derived-view contract and shares its
// truth/freshness language and evidence-handle shape.
type VisualizationPacket struct {
	// View names the derived-view family the packet was built from.
	View VisualizationView `json:"view"`
	// Title is a short human-readable subject for the subgraph.
	Title string `json:"title,omitempty"`
	// Supported is false when no subgraph could be derived from the source
	// response; the packet then carries Limitations and RecommendedNextCalls.
	Supported bool `json:"supported"`
	// Nodes are the bounded, deterministically ordered nodes.
	Nodes []VisualizationNode `json:"nodes"`
	// Edges are the bounded, deterministically ordered edges.
	Edges []VisualizationEdge `json:"edges"`
	// Truth is a copy of the source response's TruthEnvelope, when one was
	// provided. It is the canonical truth metadata for the subgraph.
	Truth *TruthEnvelope `json:"truth,omitempty"`
	// Limits states the payload bounds and retained counts.
	Limits VisualizationLimits `json:"limits"`
	// Truncation records what was dropped to stay within bounds.
	Truncation VisualizationTruncation `json:"truncation"`
	// Limitations carries bounded human-readable caveats (unsupported view,
	// truncation, missing source fields).
	Limitations []string `json:"limitations,omitempty"`
	// RecommendedNextCalls lists bounded follow-up calls in the same shape as the
	// evidence-citation recommended_next_calls convention.
	RecommendedNextCalls []map[string]any `json:"recommended_next_calls,omitempty"`
}

// visualizationBuilder accumulates nodes and edges keyed by stable ID before a
// packet is finalized. It deduplicates by ID so the same underlying identity is
// never emitted twice regardless of how many source rows referenced it.
type visualizationBuilder struct {
	view     VisualizationView
	title    string
	truth    *TruthEnvelope
	nodes    map[string]VisualizationNode
	edges    map[string]VisualizationEdge
	nodeKeys []string
	edgeKeys []string
}

func newVisualizationBuilder(view VisualizationView, title string) *visualizationBuilder {
	return &visualizationBuilder{
		view:  view,
		title: strings.TrimSpace(title),
		nodes: map[string]VisualizationNode{},
		edges: map[string]VisualizationEdge{},
	}
}

// addNode records a node under its stable ID. Duplicate canonical observations
// merge semantic fields and provenance deterministically so input order cannot
// change the rendered role, scope, label, truth, or evidence handles.
func (b *visualizationBuilder) addNode(node VisualizationNode) {
	if node.ID == "" {
		return
	}
	node = normalizeVisualizationNode(node)
	if existing, exists := b.nodes[node.ID]; exists {
		existing = mergeVisualizationNodePresentation(existing, node)
		existing.Roles = mergeVisualizationRoles(existing.Roles, node.Roles)
		existing.Role = primaryVisualizationRole(existing.Roles)
		if category := visualizationCategoryForRole(existing.Role); category != "" {
			existing.Category = category
		}
		existing.ScopeKeys = mergeVisualizationStrings(existing.ScopeKeys, node.ScopeKeys)
		existing.ScopeKey = firstVisualizationString(existing.ScopeKeys)
		existing.TruthLabel = strongerVisualizationTruthLabel(existing.TruthLabel, node.TruthLabel)
		existing.EvidenceHandles = mergeVisualizationEvidenceHandles(
			existing.EvidenceHandles,
			node.EvidenceHandles,
			existing.EvidenceHandle,
			node.EvidenceHandle,
		)
		if len(existing.EvidenceHandles) > 0 {
			existing.EvidenceHandle = &existing.EvidenceHandles[0]
		}
		b.nodes[node.ID] = existing
		return
	}
	if len(node.EvidenceHandles) > 0 {
		node.EvidenceHandles = mergeVisualizationEvidenceHandles(node.EvidenceHandles, nil, node.EvidenceHandle)
		node.EvidenceHandle = &node.EvidenceHandles[0]
	}
	b.nodes[node.ID] = node
	b.nodeKeys = append(b.nodeKeys, node.ID)
}

// addEdge records an edge under its stable ID. Endpoints with empty IDs are
// rejected so an edge never dangles. Duplicate canonical edges retain the
// strongest supported truth label and every evidence handle deterministically.
func (b *visualizationBuilder) addEdge(edge VisualizationEdge) {
	if edge.Source == "" || edge.Target == "" {
		return
	}
	edge.ID = visualizationEdgeID(edge.Source, edge.Target, edge.Relationship)
	if existing, exists := b.edges[edge.ID]; exists {
		existing.TruthLabel = strongerVisualizationTruthLabel(existing.TruthLabel, edge.TruthLabel)
		existing.EvidenceHandles = mergeVisualizationEvidenceHandles(
			existing.EvidenceHandles,
			edge.EvidenceHandles,
			existing.EvidenceHandle,
			edge.EvidenceHandle,
		)
		if len(existing.EvidenceHandles) > 0 {
			existing.EvidenceHandle = &existing.EvidenceHandles[0]
		}
		b.edges[edge.ID] = existing
		return
	}
	if len(edge.EvidenceHandles) > 0 || edge.EvidenceHandle != nil {
		edge.EvidenceHandles = mergeVisualizationEvidenceHandles(edge.EvidenceHandles, nil, edge.EvidenceHandle)
		edge.EvidenceHandle = &edge.EvidenceHandles[0]
	}
	b.edges[edge.ID] = edge
	b.edgeKeys = append(b.edgeKeys, edge.ID)
}

// finalize sorts nodes and edges by stable ID, enforces the node and edge
// bounds, drops edges whose endpoints fell outside the node bound, and records
// truncation. Ordering is by stable ID so the same source response always
// yields the same packet regardless of input order.
func (b *visualizationBuilder) finalize() VisualizationPacket {
	nodes := make([]VisualizationNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	truncation := VisualizationTruncation{}
	if len(nodes) > VisualizationMaxNodes {
		dropped := nodes[VisualizationMaxNodes:]
		truncation.Truncated = true
		truncation.DroppedNodeCount = len(dropped)
		for _, node := range dropped {
			truncation.DroppedNodeIDs = append(truncation.DroppedNodeIDs, node.ID)
		}
		nodes = nodes[:VisualizationMaxNodes]
	}

	retained := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		retained[node.ID] = struct{}{}
	}

	edges := make([]VisualizationEdge, 0, len(b.edges))
	for _, edge := range b.edges {
		_, srcOK := retained[edge.Source]
		_, dstOK := retained[edge.Target]
		if !srcOK || !dstOK {
			truncation.Truncated = true
			truncation.DroppedEdgeCount++
			continue
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	if len(edges) > VisualizationMaxEdges {
		truncation.Truncated = true
		truncation.DroppedEdgeCount += len(edges) - VisualizationMaxEdges
		edges = edges[:VisualizationMaxEdges]
	}

	packet := VisualizationPacket{
		View:      b.view,
		Title:     b.title,
		Supported: true,
		Nodes:     nodes,
		Edges:     edges,
		Truth:     cloneTruthEnvelope(b.truth),
		Limits: VisualizationLimits{
			MaxNodes:  VisualizationMaxNodes,
			MaxEdges:  VisualizationMaxEdges,
			Ordering:  "stable_id",
			NodeCount: len(nodes),
			EdgeCount: len(edges),
		},
		Truncation: truncation,
	}
	if truncation.Truncated {
		packet.Limitations = appendReason(packet.Limitations,
			"subgraph truncated to payload limits; some nodes or edges were dropped")
	}
	return packet
}

// unsupportedVisualizationPacket returns an explicit unsupported packet that
// names why a subgraph could not be derived and recommends bounded next calls,
// rather than erroring opaquely. Truth is preserved when the source response
// supplied one.
func unsupportedVisualizationPacket(
	view VisualizationView,
	truth *TruthEnvelope,
	limitations []string,
	nextCalls []map[string]any,
) VisualizationPacket {
	if len(limitations) == 0 {
		limitations = []string{"no renderable subgraph could be derived from this response"}
	}
	return VisualizationPacket{
		View:      VisualizationViewUnsupported,
		Supported: false,
		Nodes:     []VisualizationNode{},
		Edges:     []VisualizationEdge{},
		Truth:     cloneTruthEnvelope(truth),
		Limits: VisualizationLimits{
			MaxNodes: VisualizationMaxNodes,
			MaxEdges: VisualizationMaxEdges,
			Ordering: "stable_id",
		},
		Truncation:           VisualizationTruncation{},
		Limitations:          limitations,
		RecommendedNextCalls: nextCalls,
	}
}

// visualizationNodeID derives a stable, opaque node ID from a kind plus the
// underlying identity parts. Equal identities always hash to the same ID, so
// node IDs are independent of iteration order.
func visualizationNodeID(kind string, parts ...string) string {
	hash := sha1.New() // #nosec G401 -- non-cryptographic stable node ID digest for visualization identity, not a security primitive
	_, _ = fmt.Fprintf(hash, "%s\x00", kind)
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%s\x00", strings.TrimSpace(part))
	}
	return "viznode:" + hex.EncodeToString(hash.Sum(nil))[:16]
}

// visualizationEdgeID derives a stable edge ID from its endpoints and label, so
// the same relationship between the same nodes always yields the same edge ID.
func visualizationEdgeID(source, target, relationship string) string {
	hash := sha1.New() // #nosec G401 -- non-cryptographic stable edge ID digest for visualization identity, not a security primitive
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", source, target, strings.TrimSpace(relationship))
	return "vizedge:" + hex.EncodeToString(hash.Sum(nil))[:16]
}
