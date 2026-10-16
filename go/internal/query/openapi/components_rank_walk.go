// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsRankWalk documents the rank_walk block a find_code or
// search_entity_content page carries when the request sent rank_cursor
// (#7746). It is included into components' "schemas" object as a separate
// constant fragment to keep components.go under the file-size limit, like
// componentsEntitySearch. The keys mirror rank.Page.Block; the root OpenAPI
// test fails when the handler emits a key this schema lacks.
const componentsRankWalk = `      "RankWalk": {
        "type": "object",
        "description": "Present only on a page served for a rank_cursor request. The walk reads the first window_size rows of one lane once per request and serves each walkable row exactly once. Rows past the window are never read.",
        "required": ["lane", "order", "ranked", "page_cursor", "position", "window_rows", "window_size", "window_end", "lexical_truncated", "excluded_rows"],
        "properties": {
          "lane": {"type": "string", "enum": ["graph", "content"], "description": "The lane page 1 picked (find_code: graph first, content when the graph has no rows or no graph is configured; search_entity_content: always content). A different lane on a later page answers 409 basis_changed."},
          "order": {"type": "string", "enum": ["graph_name", "hybrid", "lexical"], "description": "graph_name: the graph lane's own order (the offset pages). hybrid: quantized hybrid BM25+vector score, then entity_id. lexical: the content lane's own order (exact=true, no ranker, or no scorable row)."},
          "ranked": {"type": "boolean", "description": "True only when order is hybrid."},
          "page_cursor": {"type": "string", "description": "Cursor this page was served from."},
          "next_cursor": {"type": "string", "description": "Pass as rank_cursor to read the next page; absent when window_end is true."},
          "position": {"type": "integer", "minimum": 0, "description": "Rows the walk has served through this page."},
          "window_rows": {"type": "integer", "minimum": 0, "maximum": 200, "description": "Walkable rows in the window."},
          "window_size": {"type": "integer", "minimum": 200, "maximum": 200, "description": "The fixed window bound (200)."},
          "window_end": {"type": "boolean", "description": "True when this page served the last window row."},
          "lexical_truncated": {"type": "boolean", "description": "True when more matches exist past the window; the walk does not read them."},
          "excluded_rows": {"type": "integer", "minimum": 0, "description": "Window rows left out of the walk because they had no entity_id or repeated one."}
        }
      },
      "RankCursorError": {
        "type": "object",
        "description": "The error envelope fields of a rank_cursor failure. 400 rank_cursor_invalid and rank_cursor_unsupported, and 409 rank_cursor_basis_mismatch, carry details.reason; a 409 and a rank_cursor_invalid 400 also carry details.restart_with=\"start\". basis_changed: the lane, window rows or content, query scope, order, or ranker changed. order_changed: the same window ranks differently on this replica; score bits are stable per CPU architecture only, so replicas of mixed architectures can answer it on every page.",
        "properties": {
          "code": {"type": "string", "enum": ["rank_cursor_invalid", "rank_cursor_unsupported", "rank_cursor_basis_mismatch"]},
          "reason": {"type": "string", "enum": ["malformed", "tool_mismatch", "invalid_limit", "basis_changed", "order_changed", "ranker_disabled", "scope_not_single_repo", "file_search", "offset_conflict", "cursor_conflict"]},
          "restart_with": {"type": "string", "enum": ["start"]}
        }
      },
`
