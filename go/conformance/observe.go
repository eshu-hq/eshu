// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Starter fact kinds. A contributor's real collector emits its own fact kinds;
// these neutral starter.* kinds keep the template independent of any one
// production collector. Swap them (and the Observe mapping below) for your own.
const (
	factKindRepository = "starter.repository"
	factKindDirectory  = "starter.directory"
	factKindFile       = "starter.file"
)

// Node labels and edge types the starter schema projects. They mirror the graph
// vocabulary the spec asserts (node_counts / edge_counts / required_*).
const (
	labelRepository = "Repository"
	labelDirectory  = "Directory"
	labelFile       = "File"
	edgeContains    = "CONTAINS"
)

// Observation is the in-memory, backend-free view of what the replayed collector
// facts would project to the graph: per-label node counts, per-edge counts, the
// directed correlation counts the required_correlations assertions read, and the
// per-label property values the required_nodes property floor reads.
//
// It is the contributor analogue of the values the in-repo gate reads back from
// a live graph over Bolt — derived here from the materialization seam instead,
// so the same goldengate.Evaluate* assertions run with zero backend.
type Observation struct {
	// NodeCounts is the projected node count per label.
	NodeCounts map[string]int64
	// EdgeCounts is the projected edge count per relationship type.
	EdgeCounts map[string]int64
	// Correlations counts directed (from)-[rel]->(to) edges, keyed by
	// correlationKey, for the existence-style required_correlations assertions.
	Correlations map[string]int64
	// NodeProps holds, per label and property name, the property value of every
	// node carrying the label ("" when the node has no value for that property).
	NodeProps map[string]map[string][]string
}

// correlationKey is the stable map key for a directed correlation triple.
func correlationKey(from, rel, to string) string { return from + "|" + rel + "|" + to }

// CorrelationCount returns the observed count of (from)-[rel]->(to) edges.
func (o Observation) CorrelationCount(from, rel, to string) int64 {
	return o.Correlations[correlationKey(from, rel, to)]
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
//     and contributes its (possibly empty) language to the File language floor.
func Observe(envelopes []facts.Envelope) (Observation, error) {
	obs := Observation{
		NodeCounts:   map[string]int64{},
		EdgeCounts:   map[string]int64{},
		Correlations: map[string]int64{},
		NodeProps:    map[string]map[string][]string{},
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
			parent, err := requireString(env.Payload, "parent_path")
			if err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			if _, err := requireString(env.Payload, "path"); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			obs.NodeCounts[labelDirectory]++
			obs.EdgeCounts[edgeContains]++ // one CONTAINS edge from the parent
			if parent == repoPath {
				obs.Correlations[correlationKey(labelRepository, edgeContains, labelDirectory)]++
			}
		case factKindFile:
			if _, err := requireString(env.Payload, "path"); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			if _, err := requireString(env.Payload, "dir_path"); err != nil {
				return Observation{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			obs.NodeCounts[labelFile]++
			obs.EdgeCounts[edgeContains]++ // one CONTAINS edge from the directory
			addNodeProp(obs.NodeProps, labelFile, "language", optionalString(env.Payload, "language"))
		default:
			return Observation{}, fmt.Errorf("fact[%d]: unsupported fact_kind %q for the starter conformance schema", i, env.FactKind)
		}
	}

	return obs, nil
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
// or non-string. Used for properties a node may legitimately lack (a LICENSE has
// no language), which the presence-positive property floor tolerates.
func optionalString(payload map[string]any, key string) string {
	if raw, ok := payload[key]; ok {
		if s, ok := raw.(string); ok {
			return s
		}
	}
	return ""
}
