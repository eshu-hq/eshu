// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codequery implements the code-family query handlers: the
// *CodeHandler HTTP surface behind /api/v0/code/* (search, symbols,
// structural inventory, topics, secrets, imports, call graph, flow,
// relationships, dead code, complexity, quality, call chain,
// route-to-caller, cypher, visualize, bundles) and the NornicDB/postgres
// readers those handlers execute.
//
// It moved out of root package query (#6060 lane A) so the family can be
// read, tested, and changed without pulling in the rest of the query
// surface. Root keeps the alias in code_alias.go
// (type CodeHandler = codequery.CodeHandler) and the seam in code_seam.go
// for the staying callers (content readers, contract matrix, cmd wirings)
// that still name this family's surface.
//
// Import discipline: this package may import dependency-neutral leaves --
// codemodel, querycontract, queryauth, queryspan, querygraphrows,
// queryselector, querytestutil, contentread, entitysemantics, codeshaping,
// codeprovenance, facts, parser, reducer, the internal/search* ranking
// packages, telemetry -- but NEVER root package query itself, which would
// create an import cycle (root imports this package for the alias and
// seam). Language-specific queries live in the sibling leaf
// go/internal/query/language (language.Handler, aliased by root as
// LanguageQueryHandler); that leaf imports this package, so this package
// must not import it back. It mounts from APIRouter.Mount via its own
// Language field (root handler.go), and both cmd wirings construct it
// with Neo4j/Content/Profile/Logger.
//
// Digest discipline: the queryplan-pinned builders in this package
// relocate only; their emitted Cypher text is unchanged. Where a move
// forced a spelling change (an export the seam needs, or a qualification
// to a leaf type), a same-named alias preserves the pre-move declaration
// bytes so source_sha256 keeps matching -- see relationshipStoryRequest
// and callGraphMetricsEdgeScanLimit. Never re-freeze a cypher_sha256;
// never invent a source_sha256 (take it from the gate's own mismatch
// report after proving the body differs only in the sanctioned way).
package codequery
