# MCP Budget Page Evidence (#7725)

`find_code` and `search_entity_content` advertise `limit` maximum 200, but a
legal request at that maximum over a common name returned
`mcp_response_over_budget` (610,762 and 884,737 serialized bytes against the
262,144-byte budget in the 2026-10-08 read sweep). The error carried no rows and
no way to reach the remainder.

## Page size model

A throwaway test (not committed) built rows shaped like a common-name hit: a
12 KiB source body and a 2 KiB docstring, which the read-time clips cut to
4,096 and 512 bytes, then dispatched `limit` 200 with the budget disabled and
measured the single-copy (resource-only) size the guard compares to the budget.

| Tool | Rows | Single-copy bytes | Bytes per row | Rows that fit 262,144 |
| --- | --- | --- | --- | --- |
| `search_entity_content` | 200 | 1,038,916 | 5,194 | about 50 |
| `find_code` | 200 | 1,486,172 | 7,430 | about 35 |

Both calls returned the over-budget error before the change. The model is
therefore "whole rows by serialized size", not a fixed row count: the fit count
varies with the row, so the trim measures the real size.

## Change

- `trimToBudgetPage` (`go/internal/mcp/dispatch_budget.go`) runs after the
  resource-only fallback fails. For the two row-list tools it binary-searches the
  largest first run of whole rows whose resource-only envelope is within budget,
  using the same `estimateResponseBytes` measure the guard uses, so a returned
  page fits by construction. It sets `data.truncated`, `data.next_offset`,
  `data.budget_page`, a `truth.omissions` entry, and recounts the clip markers
  over the kept rows. The over-budget error stays for other tools and for a
  first row that is over budget alone.
- The trim sits at dispatch, not in the handlers: the budget is an MCP
  dispatch concern, and only the dispatcher can measure the final envelope
  (summary text, truth block, JSON escaping inside the embedded resource). A
  handler-side byte estimate would drift from that measure.
- `find_code` gains `offset`. The handler asks the store for
  `offset + limit + 1` rows and skips `offset`; the effective limit shrinks so
  `offset + limit` stays within the 200-row ranked window, which keeps the store
  probe at or below its existing 201-row cap. An offset of 200 or more is a 400.
  The response carries `offset` only when the request set a positive one.
- The repository-anchored graph query orders by `e.name, e.id`. `e.name` alone
  is not a total order, so tied rows could swap across the page edge between
  calls. The global content path already ends its sort on `entity_id`.

No-Regression Evidence: a request that fits the budget is unchanged. The test
`TestBudgetPageLeavesFittingResponsesUnchanged` compares the rendered result
with the guard on and off and requires equal bytes and no `budget_page`,
`next_offset`, or `truth.omissions`. The one Cypher change adds `e.id` as a
second sort key to `BuildSearchGraphEntitiesQuery` (anchor
`(r:Repository {id: $repo_id})`, bounded by `LIMIT $limit`, at most 201 rows):
the sort already runs over the filtered rows and `e.id` is a property already
read, so the plan shape is the same. A live Neo4j `PROFILE` before and after was
not run in this change.

Observability Evidence: `eshu_dp_mcp_response_budget_page_total{tool}` counts
pages. The `mcp tool response budget page` log carries `tool`, `response_bytes`,
`emitted_bytes`, `budget_bytes`, `rows_returned`, and `rows_available`. The
existing `eshu_dp_mcp_response_bytes` histogram still records the attempted size.
