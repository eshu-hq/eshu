# MCP Response-Budget Telemetry

The MCP dispatcher measures the serialized `tools/call` result object, as sent
for the connection's result shape, against a 256 KiB budget
(`defaultToolResponseByteBudget`); the JSON-RPC wrapper and transport newline
are outside that count. The limit is the same in both shapes.

- Under the `dual` shape (the default), the result carries the payload twice:
  `structuredContent` and the embedded resource. When the two copies exceed the
  budget together, but the complete resource-only result fits, the result omits
  `structuredContent` and returns a successful result with the full payload in
  the resource. It returns the `mcp_response_over_budget` error envelope only
  when the resource-only result also exceeds the budget and the tool cannot
  page.
- Under the `structured` shape, a tool result carries the payload once, as
  `structuredContent` beside one text summary. There is no second copy to drop,
  so a result that fits is untouched, and one that does not goes straight to a
  page or the error envelope.

In both shapes `find_code` and `search_entity_content` page instead of
failing: the largest first run of whole rows that fits is returned with
`truncated=true` and `next_offset` (#7725). Four metrics emitted at
`applyResponseBudget` in `go/internal/mcp/dispatch_budget.go` expose these
decisions. A connection picks its shape with `?shape=dual|structured`, and the
process default comes from `ESHU_MCP_RESULT_SHAPE` (#8800).

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_mcp_response_bytes` | histogram (`By`) | `tool`, `shape` | Serialized size of the attempted result as sent for the connection's result shape (two copies under `dual`, one under `structured`), recorded for every budgeted response; 0 when the result cannot be marshalled. Buckets run from 1 KiB to 1 MiB, with resolution around the 256 KiB budget. |
| `eshu_dp_mcp_response_resource_fallback_total` | counter | `tool`, `shape` | Successful responses that omit `structuredContent` while retaining the full result in the embedded resource. Records only under `dual`; it is zero by construction under `structured`. |
| `eshu_dp_mcp_response_budget_page_total` | counter | `tool`, `shape` | Over-budget responses answered as a page of whole rows with `truncated=true` and `next_offset` (`find_code`, `search_entity_content`). |
| `eshu_dp_mcp_response_over_budget_total` | counter | `tool`, `shape` | Responses replaced by the `mcp_response_over_budget` envelope because the result as sent exceeds the budget (under `dual`, even the resource-only result), or a single row alone exceeds it. |

`tool` is a registered MCP tool name, resolved before the guard runs, so its
cardinality is bounded by the tool catalog. `shape` is `dual` or `structured`,
a closed set, so it at most doubles the series count. The metric names did not
change, so dashboards keep working; every sample recorded before #8800 is
`dual`. A query that needs the old meaning of "attempted two-copy size" should
filter `shape="dual"`, and a sum across shapes mixes the two meanings.

## Reading them

- A tool whose `eshu_dp_mcp_response_bytes` p95 approaches 262144 may start
  using the resource-only response (`dual`) or a page or error (`structured`). Check the fallback counter before changing
  a tool's result bound.
- A non-zero `eshu_dp_mcp_response_resource_fallback_total` rate means clients
  must read the embedded resource when `structuredContent` is absent. The
  `mcp tool response resource fallback` log includes `tool`, `shape`, `response_bytes`
  (attempted size in the connection's shape), `emitted_bytes`, and `budget_bytes`.
- A non-zero `eshu_dp_mcp_response_budget_page_total` rate means clients ask
  `find_code` or `search_entity_content` for more rows than fit the budget. The
  `mcp tool response budget page` log includes `tool`, `shape`, `response_bytes`
  (attempted size in the connection's shape), `emitted_bytes`, `budget_bytes`, `rows_returned`,
  and `rows_available`. Clients should follow `next_offset` or lower `limit`.
- A non-zero `eshu_dp_mcp_response_over_budget_total` rate means even the full
  resource does not fit. The `mcp tool response over budget` log includes
  `tool`, `shape`, `response_bytes`, and `budget_bytes`, plus `resource_bytes`
  under `dual` only; narrow the query scope or page size to retrieve the
  remaining data. `resource_bytes` is the size of the resource-only copy.
  Under `structured` there is no such copy, so the field is absent and
  `response_bytes` is the single emitted size.
- A disabled guard (`budget <= 0`) records nothing.

Parser fingerprint keys (`body_fp_exact`, `body_fp_renamed`, `body_sketch`,
`body_shingles`, `body_token_count`) were 23-59% of the payload on five of the
tools measured over budget in #7129. They are store-internal and are stripped
from entity `metadata` in every API and MCP response (#7167), so a drop in
`eshu_dp_mcp_response_bytes` for the entity-search tools after that change is
expected.
