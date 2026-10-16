# Relevance Walk (`rank_cursor`)

`POST /api/v0/code/search` (MCP `find_code`) and
`POST /api/v0/content/entities/search` (MCP `search_entity_content`) page by
`offset` in lexical order. The optional hybrid ranker reorders rows inside one
page only. A walk pages a fixed window instead, so a ranked result keeps its
relevance order across pages (#7746). It is opt-in: a request without
`rank_cursor` is unchanged, byte for byte.

## How a walk works

1. Send the usual search with `"rank_cursor": "start"`.
2. The server picks the lane, reads its first 200 rows (the window), orders
   the window, and returns the first `limit` rows.
3. Send the same search with `rank_cursor` set to `rank_walk.next_cursor`
   from the previous reply.
4. Stop when the reply has no `next_cursor`. Then `rank_walk.window_end` is
   `true`.

Each request re-reads the lane and the window under the caller's grant. The
cursor holds no rows: it holds the lane, the number of rows served, a digest
of their ids, and a fingerprint of the basis.

## Lanes and orders

| Route | Lane | Order |
| --- | --- | --- |
| `find_code` | `graph` when the graph search returns rows. Page 1 picks the lane exactly as a plain request does. | `graph_name`: the graph lane's own order (name, then id). Pages equal the plain `offset` pages. |
| `find_code` | `content` when the graph search returns no rows, or no graph is configured. | `hybrid` when the hybrid ranker scores the window; `lexical` (the lane's own order) with `exact=true`, with no ranker, or when no row can be scored. |
| `search_entity_content` | Always `content`. Needs `repo_id` or exactly one `repo_ids` entry. | `hybrid`, or `lexical` when no row can be scored. Needs the hybrid ranker. |

`find_code` never refuses a walk for a missing ranker. With the ranker off,
or with `exact=true`, `rank_cursor` still gives a stable exactly-once walk in
the lane's own order (`graph_name` or `lexical`), with no relevance reorder.
`rank_walk.order` and `rank_walk.ranked` tell the client which order it got.

A `hybrid` order sorts by fused score rounded to 8 significant digits,
highest first, then by `entity_id` ascending. A row the ranker cannot score
comes after the scored rows, by `entity_id`. Scored rows carry
`search_backend=hybrid`. Walk rows never carry `page_position`.

## Response

The page carries the lane's normal fields plus `rank_walk`:

| Field | Meaning |
| --- | --- |
| `lane` | `graph` or `content`. |
| `order` | `graph_name`, `hybrid`, or `lexical`. |
| `ranked` | `true` only for `hybrid`. |
| `position` | Rows the walk has served through this page. |
| `next_cursor` | Pass as `rank_cursor` for the next page. Absent at the window end. |
| `page_cursor` | The cursor this page was served from. |
| `window_rows` | Walkable rows in the window (at most 200). |
| `window_size` | The fixed window bound, 200. |
| `window_end` | This page served the last window row. |
| `lexical_truncated` | More than 200 matches exist. The walk does not read them. |
| `excluded_rows` | Window rows without an `entity_id`, or repeating one, left out of the walk. |

`truncated` is `true` while the walk has more rows, and at the window end
when `lexical_truncated` is `true`. `offset` is not used.

## Past the window

The walk reads only the window. It ends with `window_end=true` and does not
fall back to offset rows. When `lexical_truncated` is `true`, page the rest
with `offset` on `search_entity_content` (from `offset=200`, in lexical order).
`find_code` pages only inside its first 200 matches, with or without a walk.

## Errors

The OpenAPI schema `RankCursorError` lists every code and reason below, and
the 409 is the `RankCursorConflict` response.

| Status | `error.code` | `details.reason` | When |
| --- | --- | --- | --- |
| 400 | `rank_cursor_invalid` | `malformed` | The cursor is not one this server returned (garbage, edited, truncated, or repeating a row). |
| 400 | `rank_cursor_invalid` | `tool_mismatch` | The cursor came from the other route. |
| 400 | `rank_cursor_invalid` | `invalid_limit` | The page limit is not positive. |
| 400 | `rank_cursor_unsupported` | `ranker_disabled` | `search_entity_content` and the hybrid ranker is off: semantic search is not enabled (no `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` and no eligible `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` profile). |
| 400 | `rank_cursor_unsupported` | `scope_not_single_repo` | No single repository was named. |
| 400 | `rank_cursor_unsupported` | `offset_conflict` / `cursor_conflict` | `rank_cursor` with a positive `offset`, or with the unscoped `cursor`. |
| 400 | `rank_cursor_unsupported` | `file_search` | `POST /api/v0/content/files/search`, which has no walk. |
| 409 | `rank_cursor_basis_mismatch` | `basis_changed` | The basis changed: the lane, the window rows or their content, the query scope, the order, or the ranker. |
| 409 | `rank_cursor_basis_mismatch` | `order_changed` | The basis is the same, but the rows already served are no longer the top of this server's order. |

After a 409 or a `rank_cursor_invalid` 400, `details.restart_with` is
`"start"`: restart the walk. A client that does not ask for the Eshu envelope
gets the code and reason in the `detail` text.

## What is guaranteed

- Every walkable window row is served exactly once, or the walk stops with a
  409. This holds without trusting score bits.
- On the replica that serves a page, every row of that page comes after every
  row served before it.

What is not guaranteed: the same `hybrid` order on every replica. Score bits
are stable per CPU architecture only: raw scores differ by up to 4 ulp
between arm64 and amd64, and approximate vector retrieval can pick a
different candidate set on another architecture. Behind replicas of mixed
architectures a `hybrid` walk can therefore answer 409 `order_changed` on
every page. A difference that stays inside the served rows, or inside the
unserved rows, is not visible and does no harm. `graph_name` and `lexical`
walks read no scores.

## MCP response budget

When a walk page is over the 262,144-byte MCP reply cap, the reply keeps the
first rows of the page in walk order, sets `budget_page`, and replaces
`rank_walk.next_cursor` with a cursor that resumes at the first dropped row.
It never adds `next_offset` to a walk page.

## Cost and signals

A `hybrid` page scores the whole window on every request: about 19.4 ms CPU
and 22 MB of transient allocation at 200 rows on linux/arm64, the same as one
`limit=200` ranked page. A `graph_name` or `lexical` page scores nothing. The
scoring pass emits the `query.hybrid_rerank` span with `search.rerank.rows`
equal to the window size. Each walk request records
`eshu_dp_query_rank_cursor_total{tool,outcome,reason}` and the request-span
attributes `search.rank_cursor.outcome`, `search.rank_cursor.reason`,
`search.rank_cursor.position`, and, on a served page,
`search.rank_cursor.lane` and `search.rank_cursor.order` (see
[Relevance Walk Telemetry](../telemetry/rank-cursor.md)).
