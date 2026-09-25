# MCP Response-Budget Telemetry

The MCP dispatcher refuses any `tools/call` response larger than 256 KiB
(`defaultToolResponseByteBudget`), counted on both wire copies, and returns the
`mcp_response_over_budget` error envelope instead. Two metrics, emitted at
`applyResponseBudget` in `go/internal/mcp/dispatch_budget.go`, make that limit
visible before a caller hits it.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_mcp_response_bytes` | histogram (`By`) | `tool` | Serialized response size the guard measured, recorded for every budgeted response. Buckets run from 1 KiB to 1 MiB, with resolution around the 256 KiB budget. |
| `eshu_dp_mcp_response_over_budget_total` | counter | `tool` | Responses replaced by the `mcp_response_over_budget` envelope. |

`tool` is a registered MCP tool name, resolved before the guard runs, so its
cardinality is bounded by the tool catalog.

## Reading them

- A tool whose `eshu_dp_mcp_response_bytes` p95 approaches 262144 is heading
  for `mcp_response_over_budget`; narrow its default limit or bound its rows
  before callers see the error.
- A non-zero rate on `eshu_dp_mcp_response_over_budget_total` names the tool
  whose default arguments exceed the budget. The matching log line is
  `mcp tool response over budget`, with `tool`, `response_bytes`, and
  `budget_bytes`.
- A disabled guard (`budget <= 0`) records nothing.

Parser fingerprint keys (`body_fp_exact`, `body_fp_renamed`, `body_sketch`,
`body_shingles`, `body_token_count`) were 23-59% of the payload on five of the
tools measured over budget in #7129. They are store-internal and are stripped
from entity `metadata` in every API and MCP response (#7167), so a drop in
`eshu_dp_mcp_response_bytes` for the entity-search tools after that change is
expected.
