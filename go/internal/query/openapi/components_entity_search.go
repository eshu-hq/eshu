// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsEntitySearch documents the entity content search response,
// including the partial object of a search with no repository filter (#7009).
// It is included into components' "schemas" object as a separate constant
// fragment to keep components.go under the repository's file-size limit, like
// componentsSignInPolicy.
const componentsEntitySearch = `      "EntityContentSearchResponse": {
        "type": "object",
        "properties": {
          "results": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/EntityContent"}
          },
          "count": {"type": "integer"},
          "limit": {"type": "integer"},
          "offset": {"type": "integer"},
          "truncated": {"type": "boolean", "description": "True when another matching row follows the page, and always true when partial is present."},
          "partial": {
            "type": "object",
            "description": "Present only when a search with no repository filter ended at its work budget before the page was proven complete. results is then an ordered prefix of the exact answer.",
            "properties": {
              "reason": {"type": "string", "enum": ["candidate_budget_exceeded", "budget_exceeded_on_large_document"]},
              "rows_scanned_in_order": {"type": "integer", "description": "Rows this call visited in repo_id, relative_path, start_line, entity_id order, up to and including cursor; a resumed call counts from its request cursor."},
              "rows_matched": {"type": "integer", "description": "Matching rows found so far, before the request offset is applied."},
              "cursor": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string"},
                  "relative_path": {"type": "string"},
                  "start_line": {"type": "integer", "description": "Omitted when zero."},
                  "entity_id": {"type": "string"}
                }
              },
              "budget_ms": {"type": "integer", "description": "The requested work budget."},
              "elapsed_ms": {"type": "integer", "description": "SQL wall time the server measured."},
              "overrun_ms": {"type": "integer", "description": "How far a cancelled statement ran past its own timeout; above 100 the reason is budget_exceeded_on_large_document."},
              "progressed": {"type": "boolean", "description": "True when this call advanced the cursor past the request cursor. False means it scanned nothing inside the budget and resuming repeats the identical request."},
              "hint": {"type": "string"}
            }
          },
          "source_backend": {"type": "string"},
          "source_cache_clip_bytes": {"type": "integer", "description": "Read-time source_cache ceiling in bytes (4096) applied to every row of this response; always present."},
          "source_cache_clipped_rows": {"type": "integer", "description": "Number of returned rows whose source_cache was clipped to source_cache_clip_bytes; 0 when none. Clipped rows carry source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes."},
          "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
          "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none."},
          "rank_walk": {"$ref": "#/components/schemas/RankWalk"}
        }
      },
`
