// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rank walks a bounded search result across pages with a stateless
// cursor (#7746).
//
// find_code and search_entity_content page by offset in lexical order, and
// their optional hybrid ranker reorders only inside one page. A walk instead
// reads a fixed window: the first WindowSize (200) rows of one lane. The
// route picks the lane on the first page exactly as an offset request does
// (find_code: the graph lane first, the content lane when the graph has no
// rows or no graph is configured; search_entity_content: always content),
// and the cursor records it. A window has an order: OrderGraphName and
// OrderLexical keep the lane's own row order, so those pages equal the offset
// pages; OrderHybrid ranks the window by quantized score, then id, with
// unscored rows last. Each request rebuilds the window and returns the next
// limit rows after the rows already served. Rows past the window are never
// read: the walk ends at window_end, and lexical_truncated reports that more
// matches exist.
//
// The cursor holds no rows. It holds the tool, the lane, the number of rows
// served, a combinable digest of their ids, and a fingerprint of the basis
// (tool, lane, order, ranker identity, scope, and every row's id and content
// in window order). Walk refuses a cursor whose lane or basis differs from
// the rebuilt window (HTTP 409, basis_changed) and one whose served rows are
// not the top of the rebuilt order (HTTP 409, order_changed). Those two
// checks, not score bits, keep a walk from repeating or skipping a row: raw
// scores differ by a few ulp across architectures, and an approximate
// retriever can pick different candidates on another replica.
//
// Guaranteed: every walkable window row is served exactly once, or the walk
// stops with a 409; on the replica that serves page k, every row of page k
// comes after every row served before it. Not guaranteed: the same hybrid
// order on every replica. Score bits are stable per CPU architecture only, so
// replicas of mixed architectures can answer order_changed on every page.
//
// Advance and TrimWalk let the MCP response budget cut a page to a prefix
// and still return a cursor that resumes at the first dropped row.
// RecordPage and RecordError record eshu_dp_query_rank_cursor_total and the
// search.rank_cursor.* request-span attributes.
package rank
