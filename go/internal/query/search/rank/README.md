# rank

Stateless relevance walk over a bounded search window for `find_code` and
`search_entity_content` (#7746). Read `doc.go` for the contract.

## Where it sits

```mermaid
flowchart LR
  A[route handler] -->|first 200 lexical rows| B[Window]
  A -->|hybrid ranker scores| C[scores]
  B --> D[rank.Walk]
  C --> D
  D -->|page ids, rank_walk block| A
  E[MCP response budget] -->|kept prefix| F[rank.TrimWalk]
```

The route handlers (`query/codequery`, `query/contentread`) fetch the window
and score it. This package holds no I/O: it orders, checks the cursor, cuts
the page, and mints the next cursor.

## Wire contract

| Field | Where | Meaning |
| --- | --- | --- |
| `rank_cursor` | request | `"start"` opens a walk; any other value must be a `next_rank_cursor`. |
| `rank_walk.page_cursor` | response | Cursor this page was served from. |
| `rank_walk.next_rank_cursor` | response | Resumes after this page; absent at the window end. |
| `rank_walk.served` | response | Rows served by the walk through this page. |
| `rank_walk.window_rows` | response | Walkable rows in the window (at most 200). |
| `rank_walk.window_end` | response | This page served the last window row. |
| `rank_walk.lexical_truncated` | response | More lexical matches exist past the window; the walk does not read them. |
| `rank_walk.excluded_rows` | response | Window rows without an id or duplicated, left out of the walk. |

Errors: 400 `rank_cursor_invalid`, 400 `rank_cursor_unsupported`, and 409
`rank_cursor_basis_mismatch`, each with `details.reason`.

## Cost

A walk page scores the whole 200-row window on every request: about 19.4 ms
CPU and 22 MB transient allocation on linux/arm64, the same as one
`limit=200` ranked page (see `docs/internal/evidence/7746-rank-cursor.md`).

## Telemetry

`eshu_dp_query_rank_cursor_total{tool,outcome}` and the request-span
attributes `search.rank_cursor.outcome`, `search.rank_cursor.reason`, and
`search.rank_cursor.served`. The scoring pass also emits the
`query.hybrid_rerank` span with `search.rerank.rows` equal to the window size.
