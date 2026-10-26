# MCP Response Budget: Complete Resource Fallback

Status: Accepted for implementation.

## Context

MCP `tools/call` normally returns the same HTTP query result twice: as
`structuredContent` and as JSON `text` in an embedded resource. It also returns
a bounded human summary. The guard measures the serialized `mcpToolResult`
against 256 KiB, excluding the JSON-RPC wrapper and transport newline. Its
former behavior replaced the entire result with
`mcp_response_over_budget` whenever those copies together exceeded the limit,
even if one complete copy would fit.

The full-corpus hardcoded-secrets readback exposed that case. For the largest
repository, a default 25-row result measured 282,968 bytes in the current MCP
shape. A serialization shim over the corresponding HTTP envelope measured
283,022 bytes with the candidate's side-table fields, while its escaped
resource payload measured 146,733 bytes. The original 25-row read contract must
remain available; lowering its default would remove findings from that call.

## Decision

Render the normal two-copy result first and measure its exact serialized size.
If it fits, keep both machine-readable copies. If it exceeds 256 KiB, render
the same complete result without `structuredContent`. When that resource-only
result fits, return it with `isError=false`. The embedded resource keeps the
canonical `{data, truth, error}` envelope, or the complete plain JSON payload
for a route that has not adopted the envelope. The bounded text remains a
summary, never the evidence source.

If the resource-only result object also exceeds the budget, return the existing
`mcp_response_over_budget` error envelope and narrowing guidance. Do not drop
rows, silently change the tool limit, or emit an `mcpToolResult` beyond the budget.

Clients read `structuredContent` when present and otherwise parse the matching
embedded resource's JSON `text`. A client that reads only `structuredContent`
must add that fallback to handle large successful results. The success shape
change is documented in the [MCP Reference](../../public/reference/mcp-reference.md)
and [MCP Guide](../../public/guides/mcp-guide.md). It does not change tool
names, arguments, HTTP query envelopes, or the SSE transport.

## Alternatives considered

- Raising the global 256 KiB budget would allow larger context loads from every
  tool and would not remove the duplicate payload.
- Reducing the default 25-row hardcoded-secrets limit would change the accepted
  result set for the call under review.
- Truncating rows or resource text would make a successful result incomplete
  and conflict with the shared HTTP/MCP truth contract.

## Operator signals and proof

`eshu_dp_mcp_response_bytes` records the attempted two-copy size. The bounded
`eshu_dp_mcp_response_resource_fallback_total{tool}` counter records successful
resource-only responses; `eshu_dp_mcp_response_over_budget_total{tool}` records
refusals. The `mcp tool response resource fallback` log carries `tool`,
`response_bytes`, `emitted_bytes`, and `budget_bytes`. The refusal log and
in-band error details retain budget accounting. No result content or repository
identifier becomes a metric label.

The implementation proof must cover a default 25-row canonical result that
formerly failed, a plain JSON result, an ordinary two-copy response, and a
resource too large to fit. It must measure exact serialized bytes including
the summary and JSON escaping, and compare the complete fallback resource
with the handler result. The original 25-row full-corpus MCP readback remains
the runtime acceptance check.

A local three-run, 250 ms microbenchmark on a synthetic 25-row envelope
measured the former estimator at 376.8–384.5 µs/op and exact rendering at
570.3–570.6 µs/op. The added per-call cost was about 0.19 ms. This measures
serialization overhead only; it is not a full-corpus latency result.

## Decision note, 2026-10-23: a one-copy result shape (#8800)

The decision above stands for the default `dual` shape. Issue #8796 measured
that Claude Code and Codex both put the two copies of a result into the model's
input, and the arbiter ruled that a connection may choose a one-copy
`structured` shape: one text summary plus `structuredContent`, with no embedded
resource. This note records how that interacts with the budget.

- The budget keeps counting `len(json.Marshal(renderToolResult(...)))`, the
  exact bytes sent in the connection's shape. The 262,144-byte limit does not
  move, and no tool default is loosened. Defaults calibrated to two-copy
  accounting stay as they are; under `structured` they simply have more room.
- Under `structured` there is no second copy to drop, so the resource-only
  fallback does not apply and `ResourceOnly` is never set. Any result up to
  256 KiB passes untouched, including the results of about 128 KiB and up that
  `dual` would rewrite to a resource. A result over 256 KiB goes to a page for
  `find_code` and `search_entity_content` (the page fits by construction,
  because `trimToBudgetPage` measures the same rendering) and to
  `mcp_response_over_budget` for every other tool.
- Error results follow the shape: a text item and `structuredContent`, with no
  `eshu://tool-error/dispatch` resource. Generic dispatch failures under
  `structured` use `mcp_dispatch_error` with nil data/truth and one bounded
  pointer summary when the exact result fits the budget. Oversized failures
  use `mcp_response_over_budget`. `dual` keeps text-only bytes.
- The four budget instruments and three budget log lines gain a bounded `shape`
  attribute (`dual`, `structured`). Metric names are unchanged so dashboards
  keep working; samples recorded before this change are all `dual`.
  `eshu_dp_mcp_response_resource_fallback_total` keeps its meaning and records
  only under `dual`; it is zero by construction under `structured`. The
  `eshu_dp_mcp_response_bytes` description now reads "as sent for the
  connection's result shape" instead of "including both wire copies".
- Rollout is opt-in: `?shape=` on `/sse` and `/mcp/message`,
  `ESHU_MCP_RESULT_SHAPE` for the process default, default `dual`.
  `eshu mcp setup` opts the `claude` and `codex` platforms in. Rollback is
  switching the connection or the process default back to `dual`.
- Not decided here, and not implemented: flipping the default to `structured`
  (it needs a two-client re-measurement within 1.1 times the one-copy rate, a
  scoring run that is not worse, a manual run on the other documented clients,
  a week of no over-budget increase on a hosted deployment, the golden-corpus
  gate run on the final changed head). Protocol-version negotiation is already
  implemented ([#8953](https://github.com/eshu-hq/eshu/pull/8953)); the supported
  revision remains `2024-11-05`.
- The token saving was measured only on the pre-change doubling. The
  post-change two-client re-measurement is pending, so no saving is claimed for
  the `structured` shape.
- The [structured result-contract proof](../evidence/8800-structured-result-contract.md)
  at `635a19daadba81ed6f72f0a9bf8f2d1ea6dc5c03` passed 629 checks with zero
  required failures and two advisory timing warnings. Runtime code and gate
  inputs are unchanged by its documentation-only evidence record.
- The four proof-script readers now prefer `structuredContent` and retain the
  absent-only resource fallback ([#9158](https://github.com/eshu-hq/eshu/pull/9158)).
  Their parser fixtures cover both shapes and invalid responses.
- A successful non-object body under `structured` now uses the object
  `structuredContent: {"value": ...}` and result-level `_meta` marker
  `eshu/nonObjectEncoding: "json"` or `"text"`. JSON arrays, scalars, and
  `null` retain their value, including full number precision; non-JSON UTF-8
  text is escaped as a JSON string. The single text summary points to
  `structuredContent.value`. Plain objects and canonical envelopes remain
  unwrapped; `dual` is byte-compatible. The same renderer measures the wrapper,
  marker, and summary against 262,144 bytes. The default remains `dual`.
- `find_tool` results and router error results come from the router, not tool
  dispatch, and carry the payload as text and as `structuredContent` in both
  shapes. "Once on the wire" describes tool results only.
