// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
)

// identRE constrains graph labels and relationship types to a safe identifier
// shape. Labels and relationship types cannot be parameterized in Cypher, so the
// gate interpolates them into the query string; this allowlist prevents a
// malformed or hostile snapshot from injecting Cypher. The snapshot is a trusted
// committed contract, but defense in depth is cheap.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// graphCounter executes scalar-count Cypher against the graph backend. It is the
// only graph-facing seam the gate needs, so it is defined here where it is
// consumed and faked in tests.
type graphCounter interface {
	// CountNodes returns the number of nodes carrying label.
	CountNodes(ctx context.Context, label string) (int64, error)
	// CountEdges returns the number of relationships of the given type.
	CountEdges(ctx context.Context, relationship string) (int64, error)
	// CountCorrelation returns the number of (from)-[rel]->(to) paths.
	CountCorrelation(ctx context.Context, from, rel, to string) (int64, error)
	// CountCorrelationWithEvidence returns the number of (from)-[rel]->(to) paths
	// whose rel.evidence_kinds property contains every kind in evidenceKinds. It
	// isolates a single verb on a shared, tool-agnostic edge type (see
	// RequiredCorrelation.EvidenceKinds).
	CountCorrelationWithEvidence(ctx context.Context, from, rel, to string, evidenceKinds []string) (int64, error)
	// ListCorrelationEdgeProperty returns the value of relationship property prop
	// for every (from)-[rel]->(to) edge, narrowed to those whose evidence_kinds
	// contains every kind in evidenceKinds (empty = no narrowing). A null/absent or
	// non-string property yields "". The narrowing and value collection run in Go
	// because NornicDB does not evaluate a WHERE clause on this relationship shape
	// (see CountCorrelationWithEvidence); the gate corpus has at most a handful of
	// edges per relationship type, so returning the values is cheap and bounded.
	ListCorrelationEdgeProperty(ctx context.Context, from, rel, to string, evidenceKinds []string, prop string) ([]string, error)
	// ListNodeProperty returns the value of property prop for every node carrying
	// label. A null/absent or non-string property yields "".
	ListNodeProperty(ctx context.Context, label, prop string) ([]string, error)
	// CountSelfLoopEdges returns the number of (n:label {property: value})
	// -[r:relationship]->(n) self-loop edges — same source and target node. Used
	// to pin per-language recursion truth (e.g. Function{language:"dart"} CALLS
	// itself) distinctly from a spurious declaration-vs-call-site self-loop (see
	// RequiredSelfLoop).
	CountSelfLoopEdges(ctx context.Context, label, relationship, property, value string) (int64, error)
	// ListGraphElementProperties returns every node and relationship with its
	// property map, for the unresolved row-token check (graph_row_tokens.go).
	ListGraphElementProperties(ctx context.Context) ([]gg.GraphElementProperties, error)
}

// checkRequiredNodes asserts each label in requiredLabels has at least one node.
// This is the minimal required graph smoke check: it proves the pipeline
// projected the corpus into the graph, and holds for any non-empty corpus.
func checkRequiredNodes(ctx context.Context, c graphCounter, requiredLabels []string, r *Report) error {
	for _, label := range requiredLabels {
		count, err := c.CountNodes(ctx, label)
		if err != nil {
			return fmt.Errorf("count nodes %s: %w", label, err)
		}
		r.Add(EvaluateNodePresent(label, count))
	}
	return nil
}

// checkRequiredNodeAssertions evaluates the snapshot's RequiredNodes: each asserts
// label presence (count floor) and, when it names properties, that at least
// MinimumCount nodes carry each property with a non-empty (and, when pinned,
// allowed) value. It is snapshot-driven and distinct from checkRequiredNodes,
// which serves the flag-driven smoke check.
func checkRequiredNodeAssertions(ctx context.Context, c graphCounter, nodes []RequiredNode, r *Report) error {
	for _, rn := range nodes {
		count, err := c.CountNodes(ctx, rn.Label)
		if err != nil {
			return fmt.Errorf("count nodes %s: %w", rn.Label, err)
		}
		r.Add(EvaluateRequiredNode(rn, count))
		for _, prop := range rn.RequiredNodeProperties {
			values, err := c.ListNodeProperty(ctx, rn.Label, prop)
			if err != nil {
				return fmt.Errorf("list node %s property %s: %w", rn.Label, prop, err)
			}
			r.Add(EvaluateNodeProperty(rn, prop, values, rn.AllowedNodePropertyValues[prop]))
		}
	}
	return nil
}

// checkRequiredSelfLoops evaluates the snapshot's RequiredSelfLoops: the count
// of (n:Label {NodeProperty: NodePropertyValue})-[:Relationship]->(n) self-loop
// edges must fall within [MinimumCount, MaximumCount]. Always required
// (unconditionally blocking), matching checkRequiredNodeAssertions — a
// self-loop bound is corpus-size independent, not a tolerance calibrated to the
// 20-repo corpus scale.
func checkRequiredSelfLoops(ctx context.Context, c graphCounter, selfLoops []RequiredSelfLoop, r *Report) error {
	for _, rsl := range selfLoops {
		count, err := c.CountSelfLoopEdges(ctx, rsl.Label, rsl.Relationship, rsl.NodeProperty, rsl.NodePropertyValue)
		if err != nil {
			return fmt.Errorf("count self-loop edges %s: %w", rsl.ID, err)
		}
		r.Add(EvaluateRequiredSelfLoop(rsl, count))
	}
	return nil
}

// checkGraph runs every B-7(b) graph assertion: required correlations and
// node/edge count tolerances. blockingCorrelations names the correlation IDs
// that fail the gate (the rest are advisory). requiredOnly limits the run to the
// corpus-size-independent correlation/node assertions; when false (the full
// 20-repo mode) it additionally asserts the snapshot node/edge count tolerances
// as required (#3866 criterion 3).
// pipeline is optional: nil means no Postgres handle in this phase, and the
// producer-state half of a zero-correlation diagnosis is simply omitted.
func checkGraph(ctx context.Context, c graphCounter, snap Snapshot, requiredOnly bool, blockingCorrelations map[string]bool, pipeline pipelineStateQuerier, r *Report) error {
	for _, rc := range snap.Graph.RequiredCorrelations {
		var (
			count int64
			err   error
		)
		if len(rc.EvidenceKinds) > 0 {
			count, err = c.CountCorrelationWithEvidence(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel, rc.EvidenceKinds)
		} else {
			count, err = c.CountCorrelation(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel)
		}
		if err != nil {
			return fmt.Errorf("count correlation %s: %w", rc.ID, err)
		}
		finding := EvaluateRequiredCorrelation(rc, count, blockingCorrelations[rc.ID])
		r.Add(finding)
		// Diagnose only a zero, and only while the graph is still reachable. A
		// nonzero-but-short count already names its own shortfall; a zero does
		// not, and the stack is gone by the time anyone reads the log.
		if !finding.OK && count == 0 {
			emitZeroCorrelationDiagnostics(ctx, c, pipeline, rc, r)
		}
		for _, prop := range rc.RequiredEdgeProperties {
			values, err := c.ListCorrelationEdgeProperty(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel, rc.EvidenceKinds, prop)
			if err != nil {
				return fmt.Errorf("list correlation %s edge property %s: %w", rc.ID, prop, err)
			}
			r.Add(EvaluateEdgeProperty(rc, prop, values, rc.AllowedEdgePropertyValues[prop], blockingCorrelations[rc.ID]))
		}
	}
	// Required-node assertions (existence + optional property) are corpus-size
	// independent like correlations, so they run in both the minimal and full
	// gate, before the requiredOnly early return.
	if err := checkRequiredNodeAssertions(ctx, c, snap.Graph.RequiredNodes, r); err != nil {
		return err
	}
	// Required self-loop bounds are likewise corpus-size independent (they pin a
	// specific fixture's known-closed recursion set, not a scale tolerance), so
	// they also run before the requiredOnly early return.
	if err := checkRequiredSelfLoops(ctx, c, snap.Graph.RequiredSelfLoops, r); err != nil {
		return err
	}
	if err := checkUnresolvedRowTokens(ctx, c, r); err != nil {
		return err
	}
	if requiredOnly {
		return nil
	}

	// Reaching here means full-corpus mode (requiredOnly=false): the count
	// tolerances are calibrated for the 20-repo corpus and are asserted as
	// required (#3866 criterion 3).
	for _, label := range sortedKeys(snap.Graph.NodeCounts) {
		count, err := c.CountNodes(ctx, label)
		if err != nil {
			return fmt.Errorf("count nodes %s: %w", label, err)
		}
		r.Add(EvaluateNodeCount(label, snap.Graph.NodeCounts[label], count, true))
	}
	for _, rel := range sortedKeys(snap.Graph.EdgeCounts) {
		count, err := c.CountEdges(ctx, rel)
		if err != nil {
			return fmt.Errorf("count edges %s: %w", rel, err)
		}
		r.Add(EvaluateEdgeCount(rel, snap.Graph.EdgeCounts[rel], count, true))
	}
	return nil
}

func sortedKeys(m map[string]CountRange) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
