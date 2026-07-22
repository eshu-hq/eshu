// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

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
	// RequiredSelfLoops pin the count of (n)-[Relationship]->(n) self-loop edges
	// on nodes carrying a given label and property value (the self-loop-axis
	// counterpart to RequiredCorrelations and RequiredNodes). Optional and
	// additive: an empty list preserves historical behaviour (no self-loop
	// assertions).
	RequiredSelfLoops []RequiredSelfLoop `json:"required_self_loops,omitempty"`
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

// RequiredSelfLoop is a bounded assertion on the count of (n:Label
// {NodeProperty: NodePropertyValue})-[:Relationship]->(n) self-loop edges —
// same source and target node. Unlike RequiredCorrelation (an existence-only
// floor), a self-loop count must be bounded on BOTH sides: a floor alone
// cannot distinguish "genuine recursion survives" from "a declaration-vs-
// call-site bug (eshu-hq/eshu#5332) reintroduced a spurious self-loop per
// declaration", since both push the count up. NodeProperty/NodePropertyValue
// scope the match to one language/family sharing a node label (e.g. Function)
// so one language's self-loop count is not conflated with another's.
type RequiredSelfLoop struct {
	ID                string `json:"id"`
	Description       string `json:"description"`
	Label             string `json:"label"`
	Relationship      string `json:"relationship"`
	NodeProperty      string `json:"node_property"`
	NodePropertyValue string `json:"node_property_value"`
	MinimumCount      int64  `json:"minimum_count"`
	MaximumCount      int64  `json:"maximum_count"`
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

// QueryShapes describes the canonical MCP, HTTP, and CLI responses defined by
// the snapshot for B-7(c) query truth. The HTTP shapes are asserted by checkQuery
// against a running eshu-api; the MCP shapes are asserted live by checkMCPQuery
// against a running eshu-mcp-server when -mcp-base-url is set (#3866 criterion 4),
// invoking each tool through the MCP tool layer rather than only the HTTP routes
// the tools proxy to. CLI shapes are asserted offline as first-class golden
// contracts and parity rows for API/MCP/CLI shared query surfaces.
type QueryShapes struct {
	MCP  map[string]QueryShape `json:"mcp"`
	HTTP map[string]QueryShape `json:"http"`
	CLI  map[string]QueryShape `json:"cli,omitempty"`
}

// QueryShape declares the required response fields and minimum result count for
// one canonical query. ResultItemRequiredFields, when set, are validated on each
// element of the first array-valued required field.
type QueryShape struct {
	Description              string   `json:"description"`
	RequiredResponseFields   []string `json:"required_response_fields"`
	MinimumResults           int      `json:"minimum_results"`
	ResultItemRequiredFields []string `json:"result_item_required_fields"`
	// Envelope keeps a query response wrapped in Eshu's {data,truth,error}
	// envelope for assertion. The default false preserves the historical
	// behaviour where MCP envelopes are unwrapped before shape checks.
	//
	// This is a per-shape assertion choice, not a property of any transport:
	// MCP dispatch itself always sends Accept: application/eshu.envelope+json
	// on every HTTP call it makes (go/internal/mcp/dispatch.go:65,
	// unconditional). It is this gate's own harness that decides whether an
	// individual shape sees the envelope or not, driven by this field on both
	// sides — the HTTP client sends the envelope Accept header only when
	// Envelope is true (query.go:68's `if shape.Envelope`), and the MCP
	// client's maybeUnwrapTruthEnvelope (mcp.go:130-135) strips the envelope
	// from the tool result before assertion when Envelope is false. Set it to
	// true to assert the wrapped shape via RequiredJSONValues/RequiredJSONPaths
	// dotted into "data.*"; leave it false (and list the unwrapped field names
	// directly in RequiredResponseFields) when the shape needs
	// MinimumResults-based array counting, since EvaluateQueryShape only
	// locates the first array-valued field among top-level
	// RequiredResponseFields.
	Envelope bool `json:"envelope,omitempty"`
	// RequestBody is the JSON body for an HTTP query shape. Empty means the gate
	// sends no body, which is the historical GET-only behaviour.
	RequestBody map[string]any `json:"request_body,omitempty"`
	// RequiredJSONPaths are dot-separated response paths that must resolve to at
	// least one non-empty value. A segment ending in [] traverses a non-empty
	// array, for example data.candidate_buckets.dead[].
	RequiredJSONPaths []string `json:"required_json_paths,omitempty"`
	// RequiredJSONValues pins dot-separated response paths to deterministic
	// values. Array traversal uses the same [] suffix as RequiredJSONPaths and
	// passes when any resolved value equals the expected value.
	RequiredJSONValues map[string]any `json:"required_json_values,omitempty"`
	// RequiredJSONObjectMatches pins related fields to the same object. Each
	// path resolves one or more objects, and every expected partial object must
	// match a single resolved object. This prevents independent wildcard value
	// checks from accepting reversed or unrelated relationship endpoints.
	RequiredJSONObjectMatches map[string][]map[string]any `json:"required_json_object_matches,omitempty"`
	// ExpectedErrorContains declares a deliberate MCP refusal/error shape. It is
	// used only for MCP tools whose local-full-stack proof is an explicit
	// profile, fixture, or runtime-ceiling refusal rather than a successful data
	// payload.
	ExpectedErrorContains string `json:"expected_error_contains,omitempty"`
	// Arguments are the tool-call arguments for an MCP query shape (e.g.
	// get_repo_summary needs a repo selector). Empty/omitted for argument-less
	// tools and for HTTP shapes.
	Arguments map[string]any `json:"arguments,omitempty"`
	// Command is the CLI argv after the "eshu" binary name for a CLI query
	// surface. Empty for HTTP and MCP shapes.
	Command []string `json:"command,omitempty"`
	// TruthClass names the answer-truth class this surface is expected to return.
	// Shared API/MCP/CLI query rows must agree on it.
	TruthClass string `json:"truth_class,omitempty"`
	// ParityWith names peer query shapes this shape must agree with, using
	// "http:<shape-key>", "mcp:<tool-name>", or "cli:<command-key>" refs.
	ParityWith []string `json:"parity_with,omitempty"`
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
