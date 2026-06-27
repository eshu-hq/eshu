// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Snapshot is the typed view of testdata/golden/e2e-20repo-snapshot.json, the
// B-12 golden contract that the B-7 corpus gate diffs a live run against. Only
// the fields the gate asserts on are modeled; unknown JSON keys are ignored so
// the snapshot can carry human-facing notes the gate does not consume.
type Snapshot struct {
	SchemaVersion string `json:"schema_version"`
	CorpusID      string `json:"corpus_id"`

	Graph           GraphSnapshot   `json:"graph"`
	DrainAssertions DrainAssertions `json:"drain_assertions"`
	QueryShapes     QueryShapes     `json:"query_shapes"`
}

// GraphSnapshot holds the per-label node and per-relationship edge count
// tolerances plus the existence-style required correlations and required nodes.
type GraphSnapshot struct {
	NodeCounts           map[string]CountRange `json:"node_counts"`
	EdgeCounts           map[string]CountRange `json:"edge_counts"`
	RequiredCorrelations []RequiredCorrelation `json:"required_correlations"`
	// RequiredNodes are existence-plus-property node assertions (the node-axis
	// counterpart to RequiredCorrelations). Optional and additive: an empty list
	// preserves the historical behaviour where node presence came only from the
	// -required-node-labels flag.
	RequiredNodes []RequiredNode `json:"required_nodes,omitempty"`
}

// CountRange is an inclusive [Min, Max] tolerance for a node label or edge type.
// The Note is human-facing and not asserted.
type CountRange struct {
	Min  int64  `json:"min"`
	Max  int64  `json:"max"`
	Note string `json:"note"`
}

// Contains reports whether n falls within the inclusive [Min, Max] range.
func (r CountRange) Contains(n int64) bool {
	return n >= r.Min && n <= r.Max
}

// RequiredCorrelation is an existence assertion: at least MinimumCount edges of
// Relationship must connect a From node to a To node. These hold regardless of
// corpus size and are the backbone of the minimal 5-repo gate (rc-1, rc-3).
//
// EvidenceKinds, when non-empty, narrows the assertion to edges whose
// evidence_kinds relationship property contains every listed kind. Tool-agnostic
// relationships such as DEPLOYS_FROM and DEPENDS_ON are emitted by several verbs
// (ArgoCD, kustomize, ansible, ...) that all share one edge type; the bare
// (From)-[Rel]->(To) count cannot tell them apart, so an unfiltered rc for
// "kustomize DEPLOYS_FROM" would pass on an ArgoCD-only graph (a false green).
// Filtering on the verb's signature evidence kind (e.g.
// KUSTOMIZE_RESOURCE_REFERENCE) makes the count provably zero without that
// verb's fixture, isolating the verb inside the golden gate without fragmenting
// the shared, semantically-correct edge type into per-tool relationships.
type RequiredCorrelation struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Relationship  string   `json:"relationship"`
	FromLabel     string   `json:"from_label"`
	ToLabel       string   `json:"to_label"`
	MinimumCount  int64    `json:"minimum_count"`
	EvidenceKinds []string `json:"evidence_kinds,omitempty"`

	// RequiredEdgeProperties, when non-empty, asserts that every matching edge
	// (after EvidenceKinds narrowing) carries each listed relationship property
	// with a non-empty value. It is how the gate enforces edge source-tool
	// provenance (#3997/#3999): a Tier-2 shared-verb emitter that forgets to stamp
	// source_tool leaves matching edges with the property null, and the assertion
	// fails naming the verb, property, and offending count. The check is
	// absence-zero over the (evidence-narrowed) matching set — every matching edge
	// must carry it — because the narrowing already isolates exactly the verb whose
	// edges are required to be stamped. The companion MinimumCount finding
	// independently guards that the matching set is non-empty, so a property
	// assertion never passes vacuously on a verb that produced no edges. Default
	// empty = no property check (existing entries stay valid).
	RequiredEdgeProperties []string `json:"required_edge_properties,omitempty"`
	// AllowedEdgePropertyValues, keyed by property name, additionally pins each
	// matching edge's value for that property to the listed canonical vocabulary.
	// A value outside the set (or a non-string/absent value) is an offending edge.
	// Use it to pin source_tool to its normalized tokens (e.g. ansible, kustomize)
	// so an un-normalized or mistyped token fails the gate, not just a missing one.
	AllowedEdgePropertyValues map[string][]string `json:"allowed_edge_property_values,omitempty"`
}

// RequiredNode is a node-presence assertion that can also require a property.
// At least MinimumCount nodes carrying Label must exist; when
// RequiredNodeProperties is non-empty, at least MinimumCount nodes must carry
// each listed property with a non-empty value (and a value in the pinned set when
// AllowedNodePropertyValues names that property). It is the node-axis counterpart
// to RequiredCorrelation and is how the gate enforces the language/source_type
// axis (#4003): if language extraction regresses, the count of File nodes
// carrying a non-empty language drops below the floor and the gate fails.
//
// The property semantics are presence-positive (>= MinimumCount nodes carry the
// property), not absence-zero (no node lacks it): a label like File legitimately
// contains nodes with no detected language (LICENSE, .gitignore), so requiring
// every File to carry one would false-fail. Asserting a floor of correctly-tagged
// nodes proves the property is populated without lying about legitimately-untagged
// ones. (Edges differ — see RequiredEdgeProperties — because their evidence-kind
// narrowing isolates exactly the set that must all be stamped.)
type RequiredNode struct {
	ID                        string              `json:"id"`
	Description               string              `json:"description"`
	Label                     string              `json:"label"`
	MinimumCount              int64               `json:"minimum_count"`
	RequiredNodeProperties    []string            `json:"required_node_properties,omitempty"`
	AllowedNodePropertyValues map[string][]string `json:"allowed_node_property_values,omitempty"`
}

// DrainAssertions captures the B-7(a) queue-drain gate: both queues must reach a
// terminal state before any graph or query truth is checked.
type DrainAssertions struct {
	FactWorkItems           DrainBound `json:"fact_work_items"`
	SharedProjectionIntents DrainBound `json:"shared_projection_intents"`
}

// DrainBound carries the maximum tolerated residual/nonterminal row count for a
// queue. The JSON uses distinct key names per queue (residual_max vs
// nonterminal_max); both unmarshal into Max here.
type DrainBound struct {
	ResidualMax    *int64 `json:"residual_max"`
	NonterminalMax *int64 `json:"nonterminal_max"`
	Note           string `json:"note"`
}

// Max returns the tolerated ceiling for the bound, preferring whichever JSON key
// was present. Absent both, it defaults to 0 (strict drain).
func (b DrainBound) Limit() int64 {
	switch {
	case b.ResidualMax != nil:
		return *b.ResidualMax
	case b.NonterminalMax != nil:
		return *b.NonterminalMax
	default:
		return 0
	}
}

// QueryShapes describes the canonical MCP and HTTP responses defined by the
// snapshot for B-7(c) query truth. The gate currently enforces only the HTTP
// shapes (checkQuery); the MCP shapes are modeled but not yet asserted —
// asserting them is tracked in the gate-widening follow-up (#3866). The field is
// kept so the snapshot stays the single source of truth and the follow-up only
// adds the assertion, not the schema.
type QueryShapes struct {
	MCP  map[string]QueryShape `json:"mcp"`
	HTTP map[string]QueryShape `json:"http"`
}

// QueryShape declares the required response fields and minimum result count for
// one canonical query. ResultItemRequiredFields, when set, are validated on each
// element of the first array-valued required field.
type QueryShape struct {
	Description              string   `json:"description"`
	RequiredResponseFields   []string `json:"required_response_fields"`
	MinimumResults           int      `json:"minimum_results"`
	ResultItemRequiredFields []string `json:"result_item_required_fields"`
}

// LoadSnapshot reads and parses the golden snapshot at path.
func LoadSnapshot(path string) (Snapshot, error) {
	// #nosec G304 -- path is the operator-supplied golden snapshot path (a gate
	// flag), not user- or request-derived input.
	raw, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot %q: %w", path, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("parse snapshot %q: %w", path, err)
	}
	if snap.SchemaVersion == "" {
		return Snapshot{}, fmt.Errorf("snapshot %q: missing schema_version", path)
	}
	return snap, nil
}
