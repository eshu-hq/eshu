// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// graphQueryNodeLabelKeys are the node property keys, in priority order, used to
// derive a human-readable label for a projected graph-query node. The first
// non-empty property wins; the node's element id is the final fallback so a node
// always renders with a stable label.
var graphQueryNodeLabelKeys = []string{"name", "title", "path", "relative_path", "id", "uid"}

// BuildGraphQueryVisualizationPacket projects the graph nodes, relationships,
// and paths present in executed read-only Cypher result rows into a bounded,
// renderable visualization packet. Each row is a column->value map as returned
// by GraphQuery.Run; only column values that are graph entities (neo4j.Node,
// neo4j.Relationship, neo4j.Path) contribute to the subgraph. Scalar columns
// (counts, strings, property projections) are not renderable as a graph and are
// ignored, so a RETURN of bare properties yields an explicit unsupported packet
// rather than a fabricated subgraph.
//
// Node IDs are derived from the graph element id, so the same node always yields
// the same packet node ID regardless of row order. Edges are retained only when
// both endpoints appear as nodes in the same result set; the builder drops
// dangling edges into the truncation block. The packet is a pure transformation
// of the rows already returned by the authorized query and performs no further
// graph access.
func BuildGraphQueryVisualizationPacket(rows []map[string]any, truth *TruthEnvelope) VisualizationPacket {
	builder := newVisualizationBuilder(VisualizationViewGraphQuery, "graph query result")
	builder.truth = truth

	for _, row := range rows {
		for _, key := range sortedRowKeys(row) {
			addGraphQueryValue(builder, row[key])
		}
	}

	if len(builder.nodes) == 0 && len(builder.edges) == 0 {
		return unsupportedVisualizationPacket(
			VisualizationViewGraphQuery,
			truth,
			[]string{"query returned no graph nodes, relationships, or paths to visualize; RETURN whole nodes/relationships/paths (for example RETURN n, r, m) rather than scalar properties"},
			graphQueryVisualizationNextCalls(),
		)
	}

	packet := builder.finalize()
	if len(builder.edges) == 0 {
		packet.Limitations = appendReason(packet.Limitations,
			"query returned graph nodes but no relationships; the subgraph has no edges")
	}
	return packet
}

// sortedRowKeys returns the column keys of a result row in deterministic order
// so projection (and therefore the first-record-wins dedup in the builder) does
// not depend on Go map iteration order.
func sortedRowKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// addGraphQueryValue projects one result-column value into the builder. It
// handles graph nodes, relationships, and paths (including pointer and slice
// forms), and recurses into []any so a collected list such as collect(n) is
// projected element by element. Non-graph values are ignored.
func addGraphQueryValue(builder *visualizationBuilder, value any) {
	switch v := value.(type) {
	case neo4jdriver.Node:
		addGraphQueryNode(builder, v)
	case *neo4jdriver.Node:
		if v != nil {
			addGraphQueryNode(builder, *v)
		}
	case neo4jdriver.Relationship:
		addGraphQueryRelationship(builder, v)
	case *neo4jdriver.Relationship:
		if v != nil {
			addGraphQueryRelationship(builder, *v)
		}
	case neo4jdriver.Path:
		addGraphQueryPath(builder, v)
	case *neo4jdriver.Path:
		if v != nil {
			addGraphQueryPath(builder, *v)
		}
	case []any:
		for _, item := range v {
			addGraphQueryValue(builder, item)
		}
	}
}

// addGraphQueryNode adds a single graph node, keyed by its element id, and
// returns the stable visualization node ID. A node with no element id is
// skipped because it cannot anchor a deterministic ID or an edge endpoint.
func addGraphQueryNode(builder *visualizationBuilder, node neo4jdriver.Node) string {
	elementID := strings.TrimSpace(node.ElementId)
	if elementID == "" {
		return ""
	}
	nodeID := visualizationNodeID("graph", elementID)
	builder.addNode(VisualizationNode{
		ID:       nodeID,
		Type:     graphQueryNodeType(node.Labels),
		Label:    graphQueryNodeLabel(node),
		Category: graphQueryNodeType(node.Labels),
	})
	return nodeID
}

// addGraphQueryRelationship adds an edge between two graph nodes referenced by
// their element ids. The edge is recorded with both endpoint IDs; if either
// endpoint node was not itself returned by the query, finalize drops the edge as
// dangling, so a relationship never invents a node.
func addGraphQueryRelationship(builder *visualizationBuilder, rel neo4jdriver.Relationship) {
	start := strings.TrimSpace(rel.StartElementId)
	end := strings.TrimSpace(rel.EndElementId)
	if start == "" || end == "" {
		return
	}
	builder.addEdge(VisualizationEdge{
		Source:       visualizationNodeID("graph", start),
		Target:       visualizationNodeID("graph", end),
		Relationship: firstNonEmptyString(strings.TrimSpace(rel.Type), "RELATED"),
	})
}

// addGraphQueryPath projects every node and relationship in a path. Because the
// path carries both endpoint nodes for each relationship, path edges always have
// present endpoints and survive finalize.
func addGraphQueryPath(builder *visualizationBuilder, path neo4jdriver.Path) {
	for _, node := range path.Nodes {
		addGraphQueryNode(builder, node)
	}
	for _, rel := range path.Relationships {
		addGraphQueryRelationship(builder, rel)
	}
}

// graphQueryNodeType returns the primary label for a node, used as both its type
// and layout category. Multi-label nodes use their first label; unlabeled nodes
// fall back to "node".
func graphQueryNodeType(labels []string) string {
	for _, label := range labels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			return trimmed
		}
	}
	return "node"
}

// graphQueryNodeLabel derives a bounded human-readable label from the node's
// properties, falling back to the element id so a node is never unlabeled.
func graphQueryNodeLabel(node neo4jdriver.Node) string {
	for _, key := range graphQueryNodeLabelKeys {
		if label := strings.TrimSpace(StringVal(node.Props, key)); label != "" {
			return label
		}
	}
	return strings.TrimSpace(node.ElementId)
}

func graphQueryVisualizationNextCalls() []map[string]any {
	return []map[string]any{
		{
			"tool":   "execute_cypher_query",
			"reason": "run the same read-only Cypher to inspect the raw rows when the query returns scalar columns rather than graph entities",
		},
	}
}
