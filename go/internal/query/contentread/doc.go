// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contentread implements the content-read query handler family:
// HTTP file/entity content reads, bounded content search, and the bounded
// in-request hybrid re-rank, all served under ContentHandler.
//
// It moved out of root package query (#6060) so this handler family can be
// read, tested, and changed without pulling in the rest of the query
// surface. Production code here depends only on dependency-neutral leaf
// packages -- querycontract (ports, row types, response and truth envelopes,
// the paged-searcher seam, readiness errors), queryselector
// (repository-selector resolution), codemodel (document-ID recovery for
// re-rank), and the search* leaf packages for the hybrid ranker. Tests
// additionally use queryauth (scoped auth contexts) and querytestutil (the
// shared ContentStore double). Nothing here imports root package query
// itself, which would create an import cycle: root's
// content_read_alias.go imports contentread for the ContentHandler,
// ContentHybridRanker, ContentResultReranker, and FileContent compatibility
// aliases cmd/api and cmd/mcp-server still use.
//
// What deliberately did NOT move: the ContentReader store implementation and
// every content_reader_*.go file stay in root. ContentReader is shared
// infrastructure, not handler code -- sibling-lane files define
// (cr *ContentReader) read-model methods on it, and it satisfies 13 sibling
// interfaces asserted by root's interface-export tripwire, so Go's
// same-package method rule pins it in place. The one seam the two sides
// share, the paged-search fast path, crosses the boundary as the exported
// querycontract.PagedContentSearcher with decomposed primitive parameters:
// an unexported request type or interface could never be satisfied across
// the boundary, only silently missed. Root's compile-time assertion
// (_ querycontract.PagedContentSearcher = (*ContentReader)(nil) in
// content_reader.go) and this package's paged_searcher_tripwire_test.go keep
// that seam loud on both sides.
package contentread
