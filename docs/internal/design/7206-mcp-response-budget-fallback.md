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
