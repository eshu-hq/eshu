# Relevance Walk (`rank_cursor`)

`POST /api/v0/code/search` (MCP `find_code`) and
`POST /api/v0/content/entities/search` (MCP `search_entity_content`) page by
`offset` in lexical order. The optional hybrid ranker reorders rows inside one
page only. A relevance walk returns the rows in hybrid relevance order across
pages instead (#7746). It is opt-in: a request without `rank_cursor` is
unchanged, byte for byte.

## How a walk works

1. Send the usual search with `"rank_cursor": "start"`.
2. The server reads the first 200 lexical matches (the window), scores all of
   them with the hybrid ranker (BM25 fused with a local hash-embedding vector
   by Reciprocal Rank Fusion), and returns the first `limit` rows in relevance
   order.
3. Send the same search with `rank_cursor` set to
   `rank_walk.next_rank_cursor` from the previous reply.
4. Stop when the reply has no `next_rank_cursor`. Then
   `rank_walk.window_end` is `true`.

Each request re-reads and re-scores the window. The cursor holds no rows: it
holds the number of rows served, a digest of their ids, and a fingerprint of
the window.

| Route | Window source | Scope |
| --- | --- | --- |
| `find_code` | The repository's content lane (name matches, then source matches). A walk never reads the graph lane. | `repo_id` is required. |
| `search_entity_content` | The lexical entity-content order. | `repo_id`, or exactly one `repo_ids` entry. |

Rows are ordered by fused score rounded to 8 significant digits, highest
first, then by `entity_id` ascending. A row the ranker cannot score comes
after the scored rows, by `entity_id`. Scored rows carry
`search_backend=hybrid`. Walk rows never carry `page_position`.

## Response

The page carries the route's normal fields plus `rank_walk`:

| Field | Meaning |
| --- | --- |
| `page_cursor` | The cursor this page was served from. |
| `next_rank_cursor` | Pass as `rank_cursor` for the next page. Absent at the window end. |
| `served` | Rows the walk has served through this page. |
| `window_rows` | Walkable rows in the window (at most 200). |
| `window_size` | The fixed window bound, 200. |
| `window_end` | This page served the last window row. |
| `lexical_truncated` | More than 200 lexical matches exist. The walk does not read them. |
| `excluded_rows` | Window rows without an `entity_id`, or repeating one, left out of the walk. |

`truncated` is `true` while the walk has more rows, and at the window end
when `lexical_truncated` is `true`. `offset` is not used.

## Past the window

The walk reads only the window. It ends with `window_end=true` and does not
fall back to offset rows. When `lexical_truncated` is `true`, page the rest
with `offset` on `search_entity_content` (from `offset=200`, in lexical order).
`find_code` pages only inside its first 200 matches, with or without a walk.

## Errors

| Status | `error.code` | `details.reason` | When |
| --- | --- | --- | --- |
| 400 | `rank_cursor_invalid` | `malformed` | The cursor is not one this server returned (garbage, edited, truncated). |
| 400 | `rank_cursor_invalid` | `tool_mismatch` | The cursor came from the other route. |
| 400 | `rank_cursor_unsupported` | `ranker_disabled` | The hybrid ranker is off: semantic search is not enabled (no `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` and no eligible `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` profile). |
| 400 | `rank_cursor_unsupported` | `exact_search` | `find_code` with `exact=true`. |
| 400 | `rank_cursor_unsupported` | `scope_not_single_repo` | No single repository was named. |
| 400 | `rank_cursor_unsupported` | `offset_conflict` / `cursor_conflict` | `rank_cursor` with a positive `offset`, or with the unscoped `cursor`. |
| 400 | `rank_cursor_unsupported` | `file_search` | `POST /api/v0/content/files/search`, which has no walk. |
| 409 | `rank_cursor_basis_mismatch` | `candidate_set_changed` | The window changed: rows, their content, the query scope, or the ranker. |
| 409 | `rank_cursor_basis_mismatch` | `rank_order_changed` | The window is the same, but the rows already served are no longer the top of this server's order. |

After a 409 or a `rank_cursor_invalid` 400, `details.restart_with` is
`"start"`: restart the walk. A client that does not ask for the Eshu envelope
gets the code and reason in the `detail` text.

## What is guaranteed

- Every walkable window row is served exactly once, or the walk stops with a
  409. This holds without trusting score bits: raw scores differ by up to
  4 ulp between arm64 and amd64, and approximate vector retrieval can pick a
  different candidate set on another architecture.
- On the replica that serves a page, every row of that page ranks after every
  row served before it.

What is not guaranteed: the same order on every replica. A difference that
crosses the boundary between served and unserved rows is a 409. A difference
that stays inside the served rows, or inside the unserved rows, is not visible
and does no harm.

## MCP response budget

When a walk page is over the 262,144-byte MCP reply cap, the reply keeps the
first rows of the page in walk order, sets `budget_page`, and replaces
`rank_walk.next_rank_cursor` with a cursor that resumes at the first dropped
row. It never adds `next_offset` to a walk page.

## Cost and signals

A walk page scores the whole window on every request: about 19.4 ms CPU and
22 MB of transient allocation at 200 rows on linux/arm64, the same as one
`limit=200` ranked page. The scoring pass emits the `query.hybrid_rerank`
span with `search.rerank.rows` equal to the window size. Each walk request
records `eshu_dp_query_rank_cursor_total{tool,outcome}` and the request-span
attributes `search.rank_cursor.outcome`, `search.rank_cursor.served`, and, on
a 400 or 409, `search.rank_cursor.reason` (see
[Telemetry](../telemetry/index.md)).
