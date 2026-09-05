// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codemodel implements the code-model query leaf: the read-model
// builders, row decoders, response shapers, and Cypher/postgres query
// constructors behind the find_code, code-graph, complexity, dead-code,
// code-flow, import-dependency, relationship-story, and code-search reads.
// (codeowners-ownership stays in root until a later #6060 phase.)
//
// It moved out of root package query (#6060 lane A) so the family can be
// read, tested, and changed without pulling in the rest of the query
// surface. It may import only dependency-neutral leaves -- querycontract
// (envelopes, capabilities, row-value decoders, repository access filter),
// queryauth (request auth bounds), queryspan (span plumbing), the
// internal/search* hybrid ranking packages, internal/facts, and
// internal/codeprovenance -- never on root package query itself, which
// would create an import cycle: root's family_code_shim.go imports this
// package for the compatibility aliases the staying code family still
// uses.
//
// Three contracts callers must hold. Every builder here is a pure
// read-model constructor: Cypher text, row slices, and response envelopes
// are byte-identical to their root predecessors (queryplan-pinned builders
// relocate only; the Cypher text they emit is unchanged). Nothing here
// widens a caller-authorized scope or performs an unbounded read; the
// scope gates live with the staying handlers that call these builders.
// Capability registration stays in root package query, which owns the
// router and always links into the production binary.
//
// See README.md for the full boundary and AGENTS.md for the per-symbol
// export list.
package codemodel
