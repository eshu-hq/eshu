// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package indexstore reads the persisted BM25 search index for active
// curated search documents. It is the `search/index/` leaf of the
// storage/postgres split (#6693).
//
// EshuSearchIndexStore.Search ranks documents for one bounded scope, repo,
// and query: it joins the requested scope's active generation before
// scoring, so postings from a superseded generation are ignored without a
// query-time rebuild, then scores candidates with BM25 over the persisted
// eshu_search_index_terms postings and returns them ordered best-first
// alongside the corpus's indexed-document count. A search with no scope,
// repo, query, or anchor fails validation before any query runs.
//
// SortedSearchIndexTerms and BuildEshuSearchIndexQuery are exported only for
// storage/postgres's own live partition-pruning proof
// (eshu_search_index_bm25_partition_live_test.go), which builds the same
// BM25 query this package issues and runs it under EXPLAIN to prove scope
// pruning. They are not part of this package's read API for production
// callers, which use Search.
//
// This package must not import the parent postgres package.
package indexstore
