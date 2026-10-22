// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Annotation records the backend and cost class for one callable surface. It is
// the curated half of the catalog: the surface inventory says a surface exists;
// this overlay says how it is served.
type Annotation struct {
	Backend Backend
	Cost    CostClass
	// Intents are curated natural-language phrases an agent might use when it
	// needs this surface. The MCP router ranks tools against them. Every MCP
	// tool carries 3 to 8 phrases (annotations_intents_*.go); API routes carry
	// none, and an empty slice there means "not curated", not "matches nothing".
	Intents []string
	// Canonical marks the preferred tool in a family of overlapping tools. The
	// router uses it to break ranking ties. Exactly one tool per family sets it
	// (toolFamilies in annotations_intents_families.go); it is false for every
	// API route and for MCP tools that belong to no family.
	Canonical bool
}

// annotations is the curated overlay keyed by surface name. It merges the
// per-kind annotation maps (HTTP API routes and MCP tools), each of which was
// classified by reading the owning handler. Every implemented api_route and
// mcp_tool surface in the inventory MUST have an entry here; the coverage drift
// test (TestOverlayCoversInventory) fails otherwise.
func annotations() map[string]Annotation {
	merged := make(map[string]Annotation)
	for name, a := range askRouteAnnotations() {
		merged[name] = a
	}
	for name, a := range askToolAnnotations() {
		merged[name] = a
	}
	applyToolIntents(merged)
	return merged
}
