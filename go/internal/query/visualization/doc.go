// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package visualization implements the visualization-packet derivation route
// behind POST /api/v0/visualizations/derive (#6642 Part A, split off the
// #6060 lane A query-root restructure): Handler, its Mount method, and the
// three derivation builders -- BuildServiceStoryPacket,
// BuildEvidenceCitationPacket, and
// BuildIncidentContextPacket -- plus their FromMap adapters for
// canonical HTTP/MCP/CLI JSON maps.
//
// Every builder is a pure transformation over a source response the caller
// already received and authorized: it performs no graph, content, or
// reducer read, and surfaces no field beyond what the source response
// carried. Node and edge IDs are derived deterministically from the
// underlying entity/handle identity, never from iteration order; the
// subgraph is sorted by stable ID and bounded by MaxNodes and
// MaxEdges with explicit truncation; the source TruthEnvelope
// is copied verbatim; and each node may reference the evidence_citation
// handle that hydrates it. An unsupported source shape returns an explicit
// unsupported packet with limitations and recommended next calls rather than
// erroring.
//
// This package imports querycontract (the VisualizationBuilder
// implementation, the content-model types, row/string helpers, and the HTTP
// envelope helpers) and incident/model (the incident-context read-model
// types the third builder consumes); it MUST NOT import the query root, or
// root would cycle back through its own compatibility aliases in
// visualization_alias.go, which import this package for the Handler type
// alias, the Packet/View aliases, and the three
// Build*/Build*FromMap forwarders staying root and internal/mcp callers
// still use. See README.md for the file layout and move evidence, and
// AGENTS.md for the per-symbol export rationale.
package visualization
