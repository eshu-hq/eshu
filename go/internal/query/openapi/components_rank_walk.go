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
        "description": "Present only on a page served for a rank_cursor request. The walk ranks the first window_size lexical rows once per request and serves them in quantized hybrid-score order with an entity_id tiebreak; every walkable row is served exactly once. Rows past the window are never read.",
        "required": ["page_cursor", "served", "window_rows", "window_size", "window_end", "lexical_truncated", "excluded_rows"],
        "properties": {
          "page_cursor": {"type": "string", "description": "Cursor this page was served from."},
          "next_rank_cursor": {"type": "string", "description": "Pass as rank_cursor to read the next page; absent when window_end is true."},
          "served": {"type": "integer", "minimum": 0, "description": "Rows the walk has served through this page."},
          "window_rows": {"type": "integer", "minimum": 0, "maximum": 200, "description": "Walkable rows in the window."},
          "window_size": {"type": "integer", "minimum": 200, "maximum": 200, "description": "The fixed window bound (200)."},
          "window_end": {"type": "boolean", "description": "True when this page served the last window row."},
          "lexical_truncated": {"type": "boolean", "description": "True when more lexical matches exist past the window; the walk does not read them. Page those with offset instead."},
          "excluded_rows": {"type": "integer", "minimum": 0, "description": "Window rows left out of the walk because they had no entity_id or repeated one."}
        }
      },
`
