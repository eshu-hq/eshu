// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticsearch implements the curated semantic-search handler
// family: bounded retrieval over the persisted search-document corpus in
// keyword, semantic, and hybrid modes, repository-to-scope resolution,
// graph-neighborhood reranking, corpus snapshot caching, and the
// search-vector-ready freshness downgrade, all served under
// SemanticSearchHandler at POST /api/v0/search/semantic.
//
// It moved out of root package query (#6060) so this family can be read,
// tested, and changed without pulling in the rest of the query surface. It
// depends only on the dependency-neutral leaf packages under internal/query --
// querycontract (response and truth envelopes, capability gates, repository
// access filters), queryauth (the request-scoped grant snapshot and the
// permission-catalog predicates), and queryspan (the per-route HTTP span) --
// never on root package query itself, which would create an import cycle:
// root's semantic_search_alias.go imports this package for the compatibility
// aliases cmd/api and cmd/mcp-server still use.
//
// Two contracts callers must hold. A store implementing
// SemanticSearchIndexStore must filter on both SemanticSearchIndexQuery.ScopeID
// and RepoID; those differ after a repository is re-ingested under a new scope,
// and honoring only one answers outside the caller's grant. A caller reading
// SemanticSearchIndexResult must treat CorpusMayBeTruncated as part of the
// answer: an empty result set with it set means the bound was hit, not that the
// corpus is empty.
//
// This family's capability stays registered in root package query
// (contract_capability_matrix.go), which owns the router and always links into
// the production binary. Because go test ./internal/query/semanticsearch cannot
// link root (the cycle above), main_test.go's TestMain registers the same
// capability with querycontract before this package's own tests run; see that
// file's doc comment for why it exists and why it is not redundant.
package semanticsearch
