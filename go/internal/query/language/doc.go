// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package language implements the language-specific entity query route
// behind POST /api/v0/code/language-query (#6642, split off the #6060 lane A
// query-root restructure): Handler, its Mount method, the graph-backed,
// graph-first-content, and content-only entity-type dispatch, the four
// Cypher builders, the language canonicalization/spelling registry, and the
// truth-basis-to-reason and truth-basis-to-source_backend mappings the route
// reports.
//
// Handler gates every read on the shared symbol_graph.language_entities
// capability (querycontract.CapabilityUnsupported) before touching GraphQuery
// or ContentStore, resolves and validates the caller's repository selector
// and grant through codequery before dispatch, and reports the real
// querycontract.TruthBasis it observed serving each answer rather than a
// caller-assumed constant -- TruthBasisContentIndex when no live graph
// backend is configured, TruthBasisHybrid when a graph read was enriched with
// content-store metadata, TruthBasisAuthoritativeGraph for a pure graph read,
// and TruthBasisNoBackendRead for a scoped caller with no repository grants,
// answered without reaching any backend. A scoped caller with no repository
// grants gets an empty result set without reaching either backend.
//
// The Directory branch of the graph-backed dispatch is not a single
// build-and-run like the other three labels. Its statement seeks each granted
// repository's directories by an indexed repo_id and binds no Repository at
// all (#6541), so the branch resolves the repository-id list first, then runs
// the statement, then re-sorts and truncates the whole result, then fills
// repo_name from a second bounded read keyed on the surviving page. The
// measurements that force each of those steps are on buildDirectoryCypher and
// sortAndTruncateDirectoryRows; directory.go owns the branch.
//
// This package imports codequery (the repository-selector and language-query
// grant helpers), codequery/relationships/story (the entity-search dispatch
// its own tests cross-check against), entitysemantics (semantic-summary
// attachment), querycontract (profiles, envelopes, capability registration,
// HTTP helpers, read ports, and their content-model closure), rows
// (the shared semantic-metadata Cypher projection fragment), and queryspan
// (the shared handler-span seam); it MUST NOT import the query root, or root
// would cycle back through its own compatibility aliases in
// language_alias.go, which import this package for the Handler type alias
// and the forwarders cmd/api and cmd/mcp-server wiring still use. See
// README.md for the file layout and move evidence, and AGENTS.md for the
// per-symbol export rationale.
package language
