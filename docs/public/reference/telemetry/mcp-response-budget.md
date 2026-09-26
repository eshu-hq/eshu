# MCP Response-Budget Telemetry

The MCP dispatcher measures the serialized `tools/call` result object against a
256 KiB budget (`defaultToolResponseByteBudget`); the JSON-RPC wrapper and
transport newline are outside that count. When the normal result's
`structuredContent` and embedded resource exceed the budget together, but the
complete resource-only result fits, it omits `structuredContent` and returns a
successful result with the full payload in the resource. It returns the
`mcp_response_over_budget` error envelope only when the resource-only result
also exceeds the budget. Three metrics emitted at `applyResponseBudget` in
`go/internal/mcp/dispatch_budget.go` expose these decisions.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_mcp_response_bytes` | histogram (`By`) | `tool` | Serialized size of the attempted two-copy result, recorded for every budgeted response; 0 when the result cannot be marshalled. Buckets run from 1 KiB to 1 MiB, with resolution around the 256 KiB budget. |
| `eshu_dp_mcp_response_resource_fallback_total` | counter | `tool` | Successful responses that omit `structuredContent` while retaining the full result in the embedded resource. |
| `eshu_dp_mcp_response_over_budget_total` | counter | `tool` | Responses replaced by the `mcp_response_over_budget` envelope because even the resource-only result exceeds the budget. |

`tool` is a registered MCP tool name, resolved before the guard runs, so its
cardinality is bounded by the tool catalog.

## Reading them

- A tool whose `eshu_dp_mcp_response_bytes` p95 approaches 262144 may start
  using the resource-only response. Check the fallback counter before changing
  a tool's result bound.
- A non-zero `eshu_dp_mcp_response_resource_fallback_total` rate means clients
  must read the embedded resource when `structuredContent` is absent. The
  `mcp tool response resource fallback` log includes `tool`, `response_bytes`
  (attempted two-copy size), `emitted_bytes`, and `budget_bytes`.
- A non-zero `eshu_dp_mcp_response_over_budget_total` rate means even the full
  resource does not fit. The `mcp tool response over budget` log includes
  `tool`, `response_bytes`, and `budget_bytes`; narrow the query scope or page
  size to retrieve the remaining data.
- A disabled guard (`budget <= 0`) records nothing.

Parser fingerprint keys (`body_fp_exact`, `body_fp_renamed`, `body_sketch`,
`body_shingles`, `body_token_count`) were 23-59% of the payload on five of the
tools measured over budget in #7129. They are store-internal and are stripped
from entity `metadata` in every API and MCP response (#7167), so a drop in
`eshu_dp_mcp_response_bytes` for the entity-search tools after that change is
expected.
