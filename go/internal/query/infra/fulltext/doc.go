// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fulltext owns the #7245 read cutover of the infra resource search
// (POST /api/v0/infra/resources/search) onto the Neo4j keyword FULLTEXT index
// infra_search_key_ft that the writers in storage/cypher/infra/search keep.
//
// The legacy search scans every node of up to 27 infra labels and filters
// each one with a twelve-field CONTAINS predicate, so its cost is flat in the
// label size. The full-text form asks the index for every node whose joined
// key contains the escaped query (plus every overflowed node, whose key is
// not stored), keeps the caller's label set, re-applies the unchanged
// predicate and filters to every candidate, and projects the same columns.
// The index only narrows the candidates; the predicate still decides the
// rows, so the result set equals the legacy one.
//
// Select decides the statement per request. The full-text read runs only on
// Neo4j, for a non-empty query, when ESHU_INFRA_SEARCH_KEY_READ is on, and
// when the cached Readiness reports the index ONLINE and no unkeyed node.
// Readiness is probed at start and on an interval, never per request.
// Reader.Rows falls back to the legacy statement for one request when the
// full-text read fails, and counts it. NornicDB, the structured (empty-query)
// search, and the ArgoCD category-only read keep the legacy statements.
package fulltext
