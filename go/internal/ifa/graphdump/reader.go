// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import "context"

// Node is a graph node's canonicalizable identity: its label set and its
// full property map, exactly as read from the graph backend (e.g. NornicDB's
// `labels(n)` / `properties(n)`). Node intentionally carries no internal
// element ID, backend handle, or scan position — see the package doc for why
// graph identity must be content-addressed rather than ID-addressed for a
// determinism comparison to be meaningful.
type Node struct {
	// Labels holds every label projected onto the node. A NornicDB/Neo4j node
	// may carry more than one label (see the package doc's proven-facts
	// note); order is insignificant, Canonicalize sorts Labels before
	// serializing.
	Labels []string
	// Props holds the node's full property map as returned by the backend.
	// Values must be JSON-marshalable (string, bool, a numeric type,
	// []any/[]string-shaped slices, map[string]any, or nil); a value outside
	// that set makes Canonicalize return an error instead of silently
	// dropping the property.
	Props map[string]any
}

// Edge is a graph relationship's canonicalizable identity: its type, its own
// property map, and a full labels+props snapshot of each endpoint. Edge
// deliberately repeats each endpoint's Labels/Props instead of referencing a
// Node by slice index or backend ID: Canonicalize derives each endpoint's
// content digest straight from these fields, so an edge's canonical form
// never depends on iteration order, insertion order, or any internal
// identifier the backend assigns to the node.
type Edge struct {
	// Type is the relationship type (e.g. "DEPENDS_ON", "GCP_ATTACHED_TO").
	Type string
	// FromLabels is the source endpoint's label set.
	FromLabels []string
	// FromProps is the source endpoint's full property map.
	FromProps map[string]any
	// ToLabels is the target endpoint's label set.
	ToLabels []string
	// ToProps is the target endpoint's full property map.
	ToProps map[string]any
	// Props holds the edge's own property map, if any. A relationship with
	// no properties should leave this nil; normalizeProps treats nil and
	// empty identically.
	Props map[string]any
}

// Reader is the narrow read surface Canonicalize needs from a graph backend.
// It exists so graphdump's canonicalization logic is unit-testable against an
// in-memory fake, with no NornicDB/Neo4j/Docker dependency: production
// callers (the `ifa graph-dump` verb) implement Reader over a live Cypher
// session — a bare
// `MATCH (n) RETURN labels(n) AS labels, properties(n) AS props` for
// StreamNodes, and `MATCH (a)-[r]->(b) RETURN labels(a), properties(a),
// type(r), properties(r), labels(b), properties(b)` for StreamEdges, yielding
// each row straight off the Bolt result cursor — while tests implement it over
// a plain slice.
//
// The read surface is a STREAMING one (StreamNodes/StreamEdges with a yield
// callback) rather than one that returns whole slices, so a live reader never
// holds the entire node/edge set in memory at once: Canonicalize converts each
// record to bytes and discards the struct (issue #5009). Reader must never
// expose a backend element ID, and callers must not rely on any particular
// iteration order: Canonicalize sorts its own output, so a Reader that yields
// nodes/edges in a different order on every call is exactly as valid as one
// that yields a fixed order.
type Reader interface {
	// StreamNodes calls yield for every node in the graph, in any order, and
	// returns the first error yield returns (which stops iteration) or the
	// first backend read error. A streaming implementation (the live Bolt
	// reader) never materializes the whole node set: Canonicalize converts each
	// node to its canonical bytes inside yield and retains only those bytes, so
	// peak memory is the canonical record set, not the full Node struct graph
	// (issue #5009 — the Node/Edge structs, with each Edge duplicating both
	// endpoints' property maps, are the dominant cost at medium+ scale-lab
	// slots).
	StreamNodes(ctx context.Context, yield func(Node) error) error
	// StreamEdges calls yield for every relationship in the graph, in any
	// order, with the same streaming contract as StreamNodes.
	StreamEdges(ctx context.Context, yield func(Edge) error) error
}
