// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeowners implements the codeowners-ownership query family: the
// bounded, graph-backed read of one repository's Phase 3 DECLARES_CODEOWNER
// edges (issue #5419 Phase 4) plus the manifest-vs-codeowners
// effective_owner resolved by resolveEffectiveRepositoryOwner.
//
// The handler serves GET /api/v0/codeowners/ownership with deterministic
// keyset pagination over (order_index, pattern, owner_ref): the no-cursor
// page is one query, a cursor page is three disjoint bounded branch reads
// merged back into global order (the pinned NornicDB image evaluates the
// equivalent mixed OR predicate incorrectly, so the branches execute
// individually). effective_owner applies the precedence contract -- a
// service-catalog manifest declaration with an exact/derived outcome wins,
// otherwise the CODEOWNERS last-match-wins rule (highest order_index),
// otherwise the zero value -- reading the reducer's service-catalog
// correlation store and the graph, never writing to either.
//
// It moved out of root package query (#6060 lane A L2) so the family can be
// read, tested, and changed without pulling in the rest of the query
// surface. It may import only dependency-neutral leaves -- querycontract
// (envelopes, capabilities, row-value decoders, repository access filter,
// the shared service-catalog correlation port), queryauth (request auth
// bounds, tests only), queryspan (span plumbing) -- never root package
// query itself, which would create an import cycle: root's
// family_codeowners_shim.go imports this package for the compatibility
// alias cmd/api and cmd/mcp-server still use.
//
// Three contracts callers must hold. Every read is bounded: the limit clamp
// (default 50, max 200) plus the limit+1 truncation probe, the 10s graph
// read budget, and the manifest lookup cap
// (querycontract.ServiceCatalogCorrelationMaxLimit). A scoped caller not
// granted repository_id gets the bounded empty page -- empty ownership, no
// next_cursor, zero effective_owner -- without touching either read path,
// so an ungranted repository is indistinguishable from a granted-but-empty
// one. Capability registration stays in root package query
// (contract_capability_matrix_ext.go), which owns the router and always
// links into the production binary.
//
// See README.md for the full boundary and AGENTS.md for the per-symbol
// export list.
package codeowners
