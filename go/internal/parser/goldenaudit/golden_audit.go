// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldenaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const schemaVersion = "parser_golden_graph.v1"

// Graph is the independent expected or observed code graph used by parser
// golden audits.
type Graph struct {
	SchemaVersion string `json:"schema_version,omitempty"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
}

// Node identifies one source-derived code graph node.
type Node struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Edge identifies one expected source-derived relationship between graph nodes.
type Edge struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Type     string `json:"type"`
}

// Key returns the stable identity used for graph edge comparison.
func (e Edge) Key() string {
	return e.SourceID + "|" + e.Type + "|" + e.TargetID
}

// Report records every deterministic difference between expected and observed
// graph truth.
type Report struct {
	MissingNodes           []Node
	UnexpectedNodes        []Node
	DuplicateExpectedNodes []Node
	DuplicateObservedNodes []Node
	MissingEdges           []Edge
	UnexpectedEdges        []Edge
	DuplicateExpectedEdges []Edge
	DuplicateObservedEdges []Edge

	// Accuracy carries the precision/recall scoring of observed edges against
	// expected edges (see ScoreAccuracy). It is informational: Pass() ignores
	// it and reports only structural drift, so a wrong-target edge that keeps
	// node and edge sets structurally intact still surfaces a precision drop
	// here without changing the pass/fail gate.
	Accuracy AccuracyResult
}

// Pass reports whether expected and observed graph facts match exactly.
func (r Report) Pass() bool {
	return len(r.MissingNodes) == 0 &&
		len(r.UnexpectedNodes) == 0 &&
		len(r.DuplicateExpectedNodes) == 0 &&
		len(r.DuplicateObservedNodes) == 0 &&
		len(r.MissingEdges) == 0 &&
		len(r.UnexpectedEdges) == 0 &&
		len(r.DuplicateExpectedEdges) == 0 &&
		len(r.DuplicateObservedEdges) == 0
}

// Summary returns a stable one-line difference summary for test failures.
func (r Report) Summary() string {
	parts := []string{
		fmt.Sprintf("missing_nodes=%d", len(r.MissingNodes)),
		fmt.Sprintf("unexpected_nodes=%d", len(r.UnexpectedNodes)),
		fmt.Sprintf("duplicate_expected_nodes=%d", len(r.DuplicateExpectedNodes)),
		fmt.Sprintf("duplicate_observed_nodes=%d", len(r.DuplicateObservedNodes)),
		fmt.Sprintf("missing_edges=%d", len(r.MissingEdges)),
		fmt.Sprintf("unexpected_edges=%d", len(r.UnexpectedEdges)),
		fmt.Sprintf("duplicate_expected_edges=%d", len(r.DuplicateExpectedEdges)),
		fmt.Sprintf("duplicate_observed_edges=%d", len(r.DuplicateObservedEdges)),
		fmt.Sprintf("accuracy_precision=%.3f", r.Accuracy.Overall.Precision),
		fmt.Sprintf("accuracy_recall=%.3f", r.Accuracy.Overall.Recall),
	}
	return strings.Join(parts, " ")
}

// LoadGoldenGraph reads a checked-in independent golden graph fixture.
func LoadGoldenGraph(path string) (Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("read golden graph %q: %w", path, err)
	}
	var graph Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		return Graph{}, fmt.Errorf("parse golden graph %q: %w", path, err)
	}
	if graph.SchemaVersion != schemaVersion {
		return Graph{}, fmt.Errorf("golden graph %q schema_version = %q, want %q", path, graph.SchemaVersion, schemaVersion)
	}
	if err := validateGoldenGraph(graph); err != nil {
		return Graph{}, fmt.Errorf("golden graph %q is invalid: %w", path, err)
	}
	report := CompareGraph(graph, Graph{})
	if len(report.DuplicateExpectedNodes) > 0 || len(report.DuplicateExpectedEdges) > 0 {
		return Graph{}, fmt.Errorf("golden graph %q contains duplicate truth: %s", path, report.Summary())
	}
	return graph, nil
}

// CompareGraph compares expected graph truth against observed graph facts.
func CompareGraph(expected Graph, observed Graph) Report {
	expectedNodes, duplicateExpectedNodes := indexNodes(expected.Nodes)
	observedNodes, duplicateObservedNodes := indexNodes(observed.Nodes)
	expectedEdges, duplicateExpectedEdges := indexEdges(expected.Edges)
	observedEdges, duplicateObservedEdges := indexEdges(observed.Edges)

	return Report{
		MissingNodes:           missingNodes(expectedNodes, observedNodes),
		UnexpectedNodes:        missingNodes(observedNodes, expectedNodes),
		DuplicateExpectedNodes: duplicateExpectedNodes,
		DuplicateObservedNodes: duplicateObservedNodes,
		MissingEdges:           missingEdges(expectedEdges, observedEdges),
		UnexpectedEdges:        missingEdges(observedEdges, expectedEdges),
		DuplicateExpectedEdges: duplicateExpectedEdges,
		DuplicateObservedEdges: duplicateObservedEdges,
		Accuracy:               ScoreAccuracy(expected, observed),
	}
}

func validateGoldenGraph(graph Graph) error {
	nodeIDs := make(map[string]struct{}, len(graph.Nodes))
	for i, node := range graph.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("node[%d] id is required", i)
		}
		if strings.TrimSpace(node.Kind) == "" {
			return fmt.Errorf("node[%d] kind is required", i)
		}
		nodeIDs[node.ID] = struct{}{}
	}
	for i, edge := range graph.Edges {
		if strings.TrimSpace(edge.SourceID) == "" {
			return fmt.Errorf("edge[%d] source_id is required", i)
		}
		if strings.TrimSpace(edge.TargetID) == "" {
			return fmt.Errorf("edge[%d] target_id is required", i)
		}
		if strings.TrimSpace(edge.Type) == "" {
			return fmt.Errorf("edge[%d] type is required", i)
		}
		if _, ok := nodeIDs[edge.SourceID]; !ok {
			return fmt.Errorf("edge[%d] source_id %q has no fixture node", i, edge.SourceID)
		}
		if _, ok := nodeIDs[edge.TargetID]; !ok {
			return fmt.Errorf("edge[%d] target_id %q has no fixture node", i, edge.TargetID)
		}
	}
	return nil
}

func indexNodes(nodes []Node) (map[string]Node, []Node) {
	index := make(map[string]Node, len(nodes))
	duplicates := make([]Node, 0)
	for _, node := range nodes {
		if _, ok := index[node.ID]; ok {
			duplicates = append(duplicates, node)
			continue
		}
		index[node.ID] = node
	}
	sortNodes(duplicates)
	return index, duplicates
}

func indexEdges(edges []Edge) (map[string]Edge, []Edge) {
	index := make(map[string]Edge, len(edges))
	duplicates := make([]Edge, 0)
	for _, edge := range edges {
		key := edge.Key()
		if _, ok := index[key]; ok {
			duplicates = append(duplicates, edge)
			continue
		}
		index[key] = edge
	}
	sortEdges(duplicates)
	return index, duplicates
}

func missingNodes(expected map[string]Node, observed map[string]Node) []Node {
	missing := make([]Node, 0)
	for id, node := range expected {
		if _, ok := observed[id]; !ok {
			missing = append(missing, node)
		}
	}
	sortNodes(missing)
	return missing
}

func missingEdges(expected map[string]Edge, observed map[string]Edge) []Edge {
	missing := make([]Edge, 0)
	for key, edge := range expected {
		if _, ok := observed[key]; !ok {
			missing = append(missing, edge)
		}
	}
	sortEdges(missing)
	return missing
}

func sortNodes(nodes []Node) {
	sort.Slice(nodes, func(i int, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i int, j int) bool {
		return edges[i].Key() < edges[j].Key()
	})
}
