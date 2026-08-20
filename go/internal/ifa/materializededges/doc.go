// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package materializededges implements Ifá's materialized-edge coverage
// contract (#5351): one pure vacuity guard per reducer-materialized graph
// edge family (SQL relationships, documentation edges, code calls, rationale
// edges, codeowners ownership, deployable-unit edges), plus the
// replaycoverage.Resolver that dispatches a coverage-manifest row to the
// right guard by family name.
//
// # What a vacuity guard proves
//
// A guard takes a cataloged Odù and a hand-derived expected-edge-set fixture,
// runs the SAME production extraction/resolution seam the reducer uses over
// the Odù's own facts, and asserts the result matches the fixture EXACTLY —
// not "contains," both directions. A family with no registered guard cannot
// resolve covered even if a manifest row names one: "add a domain = DATA
// ONLY" (design §3) covers the fixture and manifest rows, but a new family's
// first coverage always adds its own small guard function too
// (MaterializedEdgeOduResolver.Resolve's dispatch switch, materialized_edges.go).
//
// This is deliberately narrower than "coverage": a guard proves the
// extractor, not the live graph write. The live edge write is a MERGE on
// endpoint identity, so a missing endpoint node can make the write a silent
// no-op no offline guard sees — the live ifa-determinism/ifa-fault-injection
// proof gates close that half, and the manifest's committed baseline row is
// what justifies trusting them together.
//
// # Package boundary (#6053)
//
// This package was split out of go/internal/ifa to keep that package under
// the repository's directory file-count gate. It depends on ifa (Odu,
// Catalog, CatalogByName, DiscoveredEvidence, and several exported per-family
// catalog identifiers and cassette loaders) but ifa's production code does
// not import this package back — the dependency is one-directional.
//
// A handful of ifa test files could not move here even though they exercise
// a moved guard, because they also prove a genuinely cross-family invariant
// (e.g. TestIFALiveMatrixGenerationIDsAreUniqueAcrossScopes spans every
// family's live cassette, including ones outside this package's move set).
// Those files duplicate the one or two small pure constants or generic test
// helpers they need rather than importing this package, documented at each
// duplication site; see AGENTS.md.
//
// RunMaterializedEdgeCoverage derives reducer edge-family requirements and
// resolves them against hand-derived Odù expectations. SQL relationships
// require baseline, delta-tombstone, and fault dimensions; the live matrices
// prove all three across the nine SQL writer-registry types. Code calls require
// baseline and fault dimensions; the live matrices exact-assert their five
// edges at N=1/2/4 and after domain-scoped worker and graph-write failures.
// Documentation edges require baseline and fault dimensions. Both live matrices
// exact-assert their three DOCUMENTS edges in baseline and domain-scoped
// recovery cells; the fault delta cell protects them through its full-record
// collateral comparison.
// Rationale edges require baseline and fault dimensions. Both live matrices
// exact-assert full EXPLAINS records with the complete source, relationship,
// and target properties carried by the expected fixture. The determinism
// matrix also proves the generation-2 exact-one survivor.
package materializededges
