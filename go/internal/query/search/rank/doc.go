// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rank walks a bounded search result in global relevance order
// across pages, with a stateless cursor (#7746).
//
// find_code and search_entity_content page by offset in lexical order, and
// their optional hybrid ranker reorders only inside one page. A rank walk
// instead ranks a fixed window: the first WindowSize (200) rows of the
// lexical order. Each request rebuilds the window, scores all of it, orders
// it with Order (quantized score descending, then id ascending, then
// unscored rows by id), and returns the next limit rows after the rows
// already served. Rows past the window are never read: the walk ends at
// window_end, and lexical_truncated reports that more lexical matches exist.
//
// The cursor holds no rows. It holds the number of rows served, a combinable
// digest of their ids, and a fingerprint of the window (tool, scope, ranker
// identity, and every row's id and content in lexical order). Walk refuses a
// cursor whose fingerprint differs from the rebuilt window (HTTP 409,
// candidate_set_changed) and one whose served rows are not the top of the
// rebuilt order (HTTP 409, rank_order_changed). Those two checks, not score
// bits, are what keep a walk from repeating or skipping a row: raw scores
// differ by a few ulp across architectures, and an approximate retriever
// can pick different candidates on another replica.
//
// Guaranteed: every walkable window row is served exactly once, and on the
// replica that serves page k, every row of page k ranks after every row
// served before it. Not guaranteed: the same order on every replica or
// architecture; a difference that crosses the served boundary is a 409, and
// one that stays inside the served rows or inside the unserved rows is
// invisible and harmless.
//
// Advance and TrimWalk let the MCP response budget cut a page to a prefix
// and still return a cursor that resumes at the first dropped row.
// RecordOutcome and RecordError record eshu_dp_query_rank_cursor_total and
// the search.rank_cursor.* request-span attributes.
package rank
