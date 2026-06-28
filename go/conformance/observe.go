// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Starter fact kinds. A contributor's real collector emits its own fact kinds;
// these neutral starter.* kinds keep the template independent of any one
// production collector. Swap them (and the Observe mapping below) for your own.
const (
	factKindRepository = "starter.repository"
	factKindDirectory  = "starter.directory"
	factKindFile       = "starter.file"
	factKindDependency = "starter.dependency"
)

// Node labels and edge types the starter schema projects. They mirror the graph
// vocabulary the spec asserts (node_counts / edge_counts / required_*).
const (
	labelRepository = "Repository"
	labelDirectory  = "Directory"
	labelFile       = "File"
	labelPackage    = "Package"
	edgeContains    = "CONTAINS"
	edgeDependsOn   = "DEPENDS_ON"
)

// EdgeObservation is one observed correlation edge: the evidence kinds it
// carries and its (string-valued) properties. It is what lets the conformance
// driver narrow a correlation by evidence_kinds and check required_edge_
// properties exactly like the in-repo gate, instead of counting only the bare
// (from)-[rel]->(to) triple. A tool-agnostic edge type (DEPENDS_ON, DEPLOYS_FROM)
// is emitted by several verbs sharing one relationship; without the evidence
// kinds an unrelated emitter's edge would satisfy a verb-specific assertion (a
// false green), so the evidence kinds are first-class here.
type EdgeObservation struct {
	// EvidenceKinds is the edge's evidence_kinds list (empty when the fact
	// carries none).
	EvidenceKinds []string
	// Properties holds the edge's string properties (e.g. source_tool); a value
	// is "" when absent or non-string, matching the gate's boltPropertyString.
	Properties map[string]string
}

// Observation is the in-memory, backend-free view of what the replayed collector
// facts would project to the graph: per-label node counts, per-edge counts, the
// directed correlation edges the required_correlations assertions read (with
// their evidence/properties), and the per-label property values the
// required_nodes property floor reads.
//
// It is the contributor analogue of the values the in-repo gate reads back from
// a live graph over Bolt — derived here from the materialization seam instead,
// so the same goldengate.Evaluate* assertions run with zero backend.
type Observation struct {
	// NodeCounts is the projected node count per label.
	NodeCounts map[string]int64
	// EdgeCounts is the projected edge count per relationship type.
	EdgeCounts map[string]int64
	// CorrelationEdges holds the observed edges per directed (from)-[rel]->(to)
	// triple, keyed by correlationKey, each carrying its evidence kinds and
	// properties for the required_correlations assertions.
	CorrelationEdges map[string][]EdgeObservation
	// NodeProps holds, per label and property name, the property value of every
	// node carrying the label ("" when the node has no value for that property).
	NodeProps map[string]map[string][]string
}

// correlationKey is the stable map key for a directed correlation triple.
func correlationKey(from, rel, to string) string { return from + "|" + rel + "|" + to }

// CorrelationCount returns the total observed count of (from)-[rel]->(to) edges,
// ignoring evidence narrowing. Evidence-narrowed counts are computed by the
// driver via matchingCorrelationEdges.
func (o Observation) CorrelationCount(from, rel, to string) int64 {
	return int64(len(o.CorrelationEdges[correlationKey(from, rel, to)]))
}

// matchingCorrelationEdges returns the edges for rc's triple, narrowed to those
// whose evidence kinds contain every kind in rc.EvidenceKinds. An empty
// rc.EvidenceKinds matches all edges of the triple — mirroring the gate's
// CountCorrelation (unfiltered) vs CountCorrelationWithEvidence (filtered) split.
func (o Observation) matchingCorrelationEdges(rc goldengate.RequiredCorrelation) []EdgeObservation {
	edges := o.CorrelationEdges[correlationKey(rc.FromLabel, rc.Relationship, rc.ToLabel)]
	if len(rc.EvidenceKinds) == 0 {
		return edges
	}
	out := make([]EdgeObservation, 0, len(edges))
	for _, e := range edges {
		if evidenceContainsAll(e.EvidenceKinds, rc.EvidenceKinds) {
			out = append(out, e)
		}
	}
	return out
}

// NodeProperty returns the property values observed across all nodes of label
// (one entry per node; "" for a node with no value), in cassette order.
func (o Observation) NodeProperty(label, prop string) []string {
	if byProp, ok := o.NodeProps[label]; ok {
		return byProp[prop]
	}
	return nil
}

// Observe walks the replayed fact envelopes and derives the projected graph
// observation. It fails loudly on a malformed fact (missing required field,
// duplicate or absent repository, unknown fact kind) so a bad cassette surfaces
// as an error rather than a silently-empty observation that would look green.
//
// The mapping is the starter schema's projection contract:
//   - a repository fact is one Repository node;
//   - a directory fact is one Directory node plus one CONTAINS edge from its
//     parent, and a top-level Repository->Directory correlation when its parent
//     is the repository root;
//   - a file fact is one File node plus one CONTAINS edge from its directory,
//     and contributes its (possibly empty) language to the File language floor;
//   - a dependency fact is one Package node plus one Repository->Package
//     DEPENDS_ON edge carrying its evidence_kinds and source_tool, so a
//     contributor's evidence-narrowed correlation + edge-property assertions are
//     exercised end to end.
func Observe(envelopes []facts.Envelope) (Observation, error) {
	obs := Observation{
		NodeCounts:       map[string]int64{},
		EdgeCounts:       map[string]int64{},
		CorrelationEdges: map[string][]EdgeObservation{},
		NodeProps:        map[string]map[string][]string{},
	}

	repoPath, err := repositoryPath(envelopes)
	if err != nil {
		return Observation{}, err
	}

	for i, env := range envelopes {
		switch env.FactKind {
		case factKindRepository:
			// path was already validated (and uniqueness enforced) by
			// repositoryPath above, so the node count is all that remains here.
			obs.NodeCounts[labelRepository]++
		case factKindDirectory:
			if err := observeDirectory(&obs, env.Payload, repoPath); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
		case factKindFile:
			if err := observeFile(&obs, env.Payload); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
		case factKindDependency:
			if err := observeDependency(&obs, env.Payload); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
		default:
			return Observation{}, fmt.Errorf("fact[%d]: unsupported fact_kind %q for the starter conformance schema", i, env.FactKind)
		}
	}

	return obs, nil
}

// observeDirectory projects one directory fact: a Directory node, a CONTAINS
// edge from its parent, and a top-level Repository->Directory correlation edge
// when its parent is the repository root.
func observeDirectory(obs *Observation, payload map[string]any, repoPath string) error {
	parent, err := requireString(payload, "parent_path")
	if err != nil {
		return err
	}
	if _, err := requireString(payload, "path"); err != nil {
		return err
	}
	obs.NodeCounts[labelDirectory]++
	obs.EdgeCounts[edgeContains]++ // one CONTAINS edge from the parent
	if parent == repoPath {
		addCorrelationEdge(obs, labelRepository, edgeContains, labelDirectory, EdgeObservation{})
	}
	return nil
}

// observeFile projects one file fact: a File node, a CONTAINS edge from its
// directory, and its (possibly empty) language toward the File language floor.
func observeFile(obs *Observation, payload map[string]any) error {
	if _, err := requireString(payload, "path"); err != nil {
		return err
	}
	if _, err := requireString(payload, "dir_path"); err != nil {
		return err
	}
	obs.NodeCounts[labelFile]++
	obs.EdgeCounts[edgeContains]++ // one CONTAINS edge from the directory
	addNodeProp(obs.NodeProps, labelFile, "language", optionalString(payload, "language"))
	return nil
}

// observeDependency projects one dependency fact: a Package node and a
// Repository->Package DEPENDS_ON edge carrying the fact's evidence_kinds and
// source_tool, so evidence-narrowed correlation and edge-property assertions run.
func observeDependency(obs *Observation, payload map[string]any) error {
	if _, err := requireString(payload, "package_name"); err != nil {
		return err
	}
	obs.NodeCounts[labelPackage]++
	obs.EdgeCounts[edgeDependsOn]++
	addCorrelationEdge(obs, labelRepository, edgeDependsOn, labelPackage, EdgeObservation{
		EvidenceKinds: optionalStringSlice(payload, "evidence_kinds"),
		Properties:    map[string]string{"source_tool": optionalString(payload, "source_tool")},
	})
	return nil
}

// addCorrelationEdge appends an observed edge to the directed-triple bucket.
func addCorrelationEdge(obs *Observation, from, rel, to string, edge EdgeObservation) {
	key := correlationKey(from, rel, to)
	obs.CorrelationEdges[key] = append(obs.CorrelationEdges[key], edge)
}

// repositoryPath finds the single repository fact's path, erroring when the
// cassette carries no repository fact or more than one.
func repositoryPath(envelopes []facts.Envelope) (string, error) {
	path := ""
	seen := false
	for i, env := range envelopes {
		if env.FactKind != factKindRepository {
			continue
		}
		if seen {
			return "", fmt.Errorf("fact[%d] %s: duplicate repository fact", i, env.FactKind)
		}
		p, err := requireString(env.Payload, "path")
		if err != nil {
			return "", fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
		}
		path = p
		seen = true
	}
	if !seen {
		return "", fmt.Errorf("cassette has no %s fact", factKindRepository)
	}
	return path, nil
}

// evidenceContainsAll reports whether have contains every kind in required. An
// empty required set returns false (callers narrow only when required is
// non-empty) — the same conservative contract as the gate's
// edgeEvidenceContainsAll, so an empty narrowing kind never silently matches
// every edge.
func evidenceContainsAll(have, required []string) bool {
	if len(required) == 0 {
		return false
	}
	present := make(map[string]struct{}, len(have))
	for _, k := range have {
		present[k] = struct{}{}
	}
	for _, want := range required {
		if _, ok := present[want]; !ok {
			return false
		}
	}
	return true
}

// edgePropertyValues returns prop's value for every edge in matches ("" when an
// edge lacks the property), the input EvaluateEdgeProperty consumes.
func edgePropertyValues(matches []EdgeObservation, prop string) []string {
	out := make([]string, 0, len(matches))
	for _, e := range matches {
		out = append(out, e.Properties[prop])
	}
	return out
}

// addNodeProp appends a node's property value (possibly empty) to the per-label,
// per-property value list.
func addNodeProp(m map[string]map[string][]string, label, prop, value string) {
	byProp, ok := m[label]
	if !ok {
		byProp = map[string][]string{}
		m[label] = byProp
	}
	byProp[prop] = append(byProp[prop], value)
}

// requireString reads a non-empty string payload field or returns an error.
func requireString(payload map[string]any, key string) (string, error) {
	raw, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("field %q is %T, want string", key, raw)
	}
	if s == "" {
		return "", fmt.Errorf("field %q is empty", key)
	}
	return s, nil
}

// optionalString reads a string payload field, returning "" when absent, null,
// or non-string. Used for properties a node or edge may legitimately lack (a
// LICENSE has no language), which the presence-positive property floor tolerates
// and the edge-property check flags as offending only when the spec requires it.
func optionalString(payload map[string]any, key string) string {
	if raw, ok := payload[key]; ok {
		if s, ok := raw.(string); ok {
			return s
		}
	}
	return ""
}

// optionalStringSlice reads a JSON string array payload field, returning nil
// when absent or not an array of strings. The cassette decodes arrays as
// []any, so each element is type-asserted to string.
func optionalStringSlice(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
