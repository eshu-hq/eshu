// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// readSurfaceBackingKind classifies what kind of live artifact a read-surface
// label or literal route ultimately resolves to. Typed rather than
// special-cased per label so a new label (or a new content_relationships-style
// alias) is a data change, not a code change.
type readSurfaceBackingKind string

// languageParityReadSurfaceNone is the literal sentinel a language row uses
// to declare, truthfully, that it has no read surface at all -- mirroring
// factKindReadSurfaceNone (read_surface_factkind.go) for the fact-kind side
// of this same gate. scripts/verify-parser-relationship-kit.sh requires
// every language_features row to carry a non-empty read_surfaces list (so a
// row can never silently go undocumented), but "no real consumer exists" is
// a legitimate, permanent state for a graph-written-but-unconsumed or
// parsed-but-unmaterialized language (see #5334). "none" lets a row say that
// honestly instead of claiming an unresolvable label or being forced into a
// grandfather entry.
const languageParityReadSurfaceNone = "none"

const (
	// readSurfaceBackingMCPTool means Ref is an exact tool.Name entry in
	// ReadOnlyTools() (and, for the six labels that are literal MCP dispatch
	// case strings, also the case string in dispatch.go/dispatch_impact.go).
	readSurfaceBackingMCPTool readSurfaceBackingKind = "mcp_tool"
	// readSurfaceBackingGoSymbol means Ref is a "<file>.go:<symbol>" pointer
	// into query.ReadSurfaceGoSymbolBackings, itself kept honest by a
	// compile-time reference in go/internal/query.
	readSurfaceBackingGoSymbol readSurfaceBackingKind = "go_symbol"
	// readSurfaceBackingAPIRoute means Ref is a "METHOD /path" surface
	// matched positionally against the live route inventory. No language-
	// parity label uses this kind today (all nine resolve to an MCP tool or
	// a Go symbol); it exists so a future label can be added as data rather
	// than requiring a new resolution code path.
	readSurfaceBackingAPIRoute readSurfaceBackingKind = "api_route"
)

// readSurfaceBacking is one closed-map entry: the kind of live artifact a
// label resolves to, and the exact reference to check it against.
type readSurfaceBacking struct {
	Kind readSurfaceBackingKind
	Ref  string
}

// languageParityReadSurfaceBacking is the closed, hand-maintained map from
// every abstract read_surfaces label specs/language-feature-parity-ledger.v1.yaml
// uses to the live artifact that actually backs it. This is the #5335 gate's
// core anti-false-claim mechanism: TestLanguageParityReadSurfacesResolveToRealConsumers
// (read_surface_consumer_existence_test.go) fails closed for any label not in
// this map, and fails for any label whose Ref is not confirmed live.
//
// Seven labels equal a registered tool name directly, so Ref equals the
// label: six are literal MCP dispatch case strings (dispatch.go,
// dispatch_impact.go), and "list_relationship_edges" (#5369) is routed
// through its own dispatch function (dispatch_relationship_edges.go) rather
// than the shared case-string switch, but its label still equals its tool
// name. Two labels are aliases the label text does not name directly:
// "entity_context" is served by the get_entity_context MCP tool, and
// "content_relationships" is served by the unexported
// query.buildContentRelationshipSet Go symbol (there is no MCP tool wrapping
// it 1:1 -- it backs several tools' relationship sections).
var languageParityReadSurfaceBacking = map[string]readSurfaceBacking{
	"execute_language_query":      {Kind: readSurfaceBackingMCPTool, Ref: "execute_language_query"},
	"entity_context":              {Kind: readSurfaceBackingMCPTool, Ref: "get_entity_context"},
	"content_relationships":       {Kind: readSurfaceBackingGoSymbol, Ref: "content_relationships.go:buildContentRelationshipSet"},
	"find_dead_code":              {Kind: readSurfaceBackingMCPTool, Ref: "find_dead_code"},
	"get_code_relationship_story": {Kind: readSurfaceBackingMCPTool, Ref: "get_code_relationship_story"},
	"list_relationship_edges":     {Kind: readSurfaceBackingMCPTool, Ref: "list_relationship_edges"},
	"trace_deployment_chain":      {Kind: readSurfaceBackingMCPTool, Ref: "trace_deployment_chain"},
	"trace_resource_to_code":      {Kind: readSurfaceBackingMCPTool, Ref: "trace_resource_to_code"},
	"trace_route_callers":         {Kind: readSurfaceBackingMCPTool, Ref: "trace_route_callers"},
}

// resolveLanguageParityReadSurface reports whether label resolves to a live
// consumer, given the closed backing map, the live MCP read-only tool name
// set, and the go_symbol compile-check registry (query.ReadSurfaceGoSymbolBackings).
// It deliberately does not consult the grandfather ledger -- callers apply
// that separately (see grandfatheredLanguageParityRowOK in
// read_surface_consumer_existence_test.go) so grandfathering stays visible at
// the call site instead of being buried in resolution logic.
func resolveLanguageParityReadSurface(
	label string,
	backing map[string]readSurfaceBacking,
	liveMCPTools map[string]struct{},
	goSymbolBackings map[string]bool,
) (ok bool, reason string) {
	if label == languageParityReadSurfaceNone {
		return true, ""
	}
	b, known := backing[label]
	if !known {
		return false, fmt.Sprintf("label %q is not in the closed read-surface backing map (languageParityReadSurfaceBacking)", label)
	}
	switch b.Kind {
	case readSurfaceBackingMCPTool:
		if _, exists := liveMCPTools[b.Ref]; !exists {
			return false, fmt.Sprintf("label %q backs mcp_tool %q, which is not in ReadOnlyTools()", label, b.Ref)
		}
		return true, ""
	case readSurfaceBackingGoSymbol:
		if !goSymbolBackings[b.Ref] {
			return false, fmt.Sprintf("label %q backs go_symbol %q, which is not confirmed in query.ReadSurfaceGoSymbolBackings", label, b.Ref)
		}
		return true, ""
	case readSurfaceBackingAPIRoute:
		return false, fmt.Sprintf("label %q backs api_route %q, but no live-route check is wired for language-parity labels yet", label, b.Ref)
	default:
		return false, fmt.Sprintf("label %q has unsupported backing kind %q", label, b.Kind)
	}
}

// languageParityRowDigest hashes a language row's exact read_surfaces list so
// the grandfather ledger can detect an edit: reordering, adding, or removing
// any entry changes the digest and un-grandfathers the row.
func languageParityRowDigest(readSurfaces []string) string {
	sum := sha256.Sum256([]byte(strings.Join(readSurfaces, "\x00")))
	return fmt.Sprintf("%x", sum)
}

// liveMCPToolNameSet returns the set of every ReadOnlyTools() name, for O(1)
// mcp_tool backing checks.
func liveMCPToolNameSet() map[string]struct{} {
	tools := ReadOnlyTools()
	out := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		out[tool.Name] = struct{}{}
	}
	return out
}
