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
        "description": "One page of entity content search rows. Owner: contentread/content_handler.go entityContentSearchResponse over contentSearchResponse, with clip markers from querycontract/source_cache_clip.go and the walk block from rank.Page.Block on rank-cursor pages.",
        "properties": {
          "results": {
            "type": "array",
            "description": "Page rows in repo_id, relative_path, start_line, entity_id order. Owner: contentread/content_handler_rerank.go entityContentSearchResponse.",
            "items": {"$ref": "#/components/schemas/EntityContent"}
          },
          "count": {"type": "integer", "description": "Rows in this page. Owner: contentread/content_handler.go contentSearchResponse counts len(results)."},
          "limit": {"type": "integer", "description": "Request limit served. Owner: contentread/content_handler.go contentSearchResponse reads req.limit()."},
          "offset": {"type": "integer", "description": "Request offset served. Owner: contentread/content_handler.go contentSearchResponse reads req.offset()."},
          "truncated": {"type": "boolean", "description": "True when another matching row follows the page, and always true when partial is present."},
          "partial": {
            "type": "object",
            "description": "Present only when a search with no repository filter ended at its work budget before the page was proven complete. results is then an ordered prefix of the exact answer. Owner: querycontract/paged_content_searcher.go SearchPartial, attached by ContentHandler.searchEntitiesUnscoped.",
            "properties": {
              "reason": {"type": "string", "enum": ["candidate_budget_exceeded", "budget_exceeded_on_large_document", "pattern_has_no_trigram"], "description": "Closed budget-exhaustion reason from querycontract/paged_content_searcher.go SearchPartial.Reason."},
              "rows_scanned_in_order": {"type": "integer", "description": "Rows this call visited in repo_id, relative_path, start_line, entity_id order, up to and including cursor; a resumed call counts from its request cursor."},
              "rows_matched": {"type": "integer", "description": "Matching rows found so far, before the request offset is applied."},
              "cursor": {
                "type": "object",
                "description": "Last key the budgeted walk visited; resuming starts strictly after it. Owner: querycontract/paged_content_searcher.go SearchCursor.",
                "properties": {
                  "repo_id": {"type": "string", "description": "Repository the visited key belongs to. Owner: querycontract/paged_content_searcher.go SearchCursor.RepoID."},
                  "relative_path": {"type": "string", "description": "Repository-relative file path of the visited key. Owner: querycontract/paged_content_searcher.go SearchCursor.RelativePath."},
                  "start_line": {"type": "integer", "description": "Omitted when zero. Owner: querycontract/paged_content_searcher.go SearchCursor.StartLine (omitempty)."},
                  "entity_id": {"type": "string", "description": "Visited entity id; empty on a file-search cursor. Owner: querycontract/paged_content_searcher.go SearchCursor.EntityID (omitempty)."}
                }
              },
              "budget_ms": {"type": "integer", "description": "The requested work budget. Owner: querycontract/paged_content_searcher.go SearchPartial.BudgetMS."},
              "elapsed_ms": {"type": "integer", "description": "SQL wall time the server measured. Owner: querycontract/paged_content_searcher.go SearchPartial.ElapsedMS."},
              "overrun_ms": {"type": "integer", "description": "How far a cancelled statement ran past its own timeout; above 100 the reason is budget_exceeded_on_large_document. Owner: querycontract/paged_content_searcher.go SearchPartial.OverrunMS."},
              "progressed": {"type": "boolean", "description": "True when this call advanced the cursor past the request cursor. False means it scanned nothing inside the budget and resuming repeats the identical request."},
              "hint": {"type": "string", "description": "How to get an exact answer: scope with repo_id, or raise ESHU_CONTENT_SEARCH_BUDGET_MS when nothing progressed. Owner: querycontract/paged_content_searcher.go SearchPartial.Hint (SearchPartialHint / SearchPartialNoProgressHint)."}
            }
          },
          "source_backend": {"type": "string", "enum": ["postgres_content_store"], "description": "Backend that served the page; always postgres_content_store on this route. Owner: contentread/content_handler.go contentSearchResponse hard-codes it."},
          "source_cache_clip_bytes": {"type": "integer", "description": "Read-time source_cache ceiling in bytes (4096) applied to every row of this response; always present. Owner: querycontract/source_cache_clip.go AddSourceCacheClipMarkers."},
          "source_cache_clipped_rows": {"type": "integer", "description": "Number of returned rows whose source_cache was clipped to source_cache_clip_bytes; 0 when none. Clipped rows carry source_cache_clipped, source_cache_clip_bytes, and source_cache_total_bytes. Owner: querycontract/source_cache_clip.go AddSourceCacheClipMarkers."},
          "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present. Owner: querycontract/source_cache_clip.go AddDocstringClipMarkers."},
          "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows whose docstring was clipped; 0 when none. Owner: querycontract/source_cache_clip.go AddDocstringClipMarkers."},
          "rank_walk": {"$ref": "#/components/schemas/RankWalk", "description": "Relevance-walk block, present only on a page served for a rank_cursor request. Owner: contentread/rank_walk.go writeEntityRankWalkPage writes rank.Page.Block()."}
        }
      },
`
