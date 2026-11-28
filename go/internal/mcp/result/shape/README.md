# MCP result shape

## Purpose

`resultshape` names the two wire layouts of an MCP `tools/call` result and
carries a connection's choice through a request context.

- `Dual` (default): text summary, the envelope again as an embedded resource,
  and `structuredContent`. This is the layout Eshu sent before `Structured`
  existed.
- `Structured`: one text summary and `structuredContent`. A tool result's
  payload crosses the wire once and no embedded resource is sent. Successful
  non-object values are wrapped as `structuredContent.value` with result-level
  `_meta.eshu/nonObjectEncoding` (`json` or `text`); plain objects and
  canonical envelopes stay unwrapped. Generic dispatch failures carry a
  canonical `mcp_dispatch_error` envelope with unavailable data and truth
  when it fits the response budget; oversized failures use
  `mcp_response_over_budget`.

## Ownership boundary

This package owns the value, `Parse`, `OrDual`, and the context carrier
(`WithOverride`, `FromContext`). The parent `internal/mcp` package renders the
result, counts it against the response budget, reads the `shape` query
parameter, and records telemetry. `cmd/mcp-server` reads `ESHU_MCP_RESULT_SHAPE`.

## Exported surface

- `Mode`, `Dual`, `Structured`
- `Parse`, `OrDual`
- `WithOverride`, `FromContext`

## Dependencies

Standard library only, so the parent package and `cmd/mcp-server` can both
import it without a cycle.

## Telemetry

None here. The parent package labels the response-budget instruments with the
shape as a bounded `shape` attribute.

## Gotchas

- `Parse` does not trim. Callers that accept padded input trim first, as they
  do for the tool surface.
- `OrDual` returns `Structured` only for exactly `Structured`; the zero value
  and any unrecognized `Mode` become `Dual`. `WithResultShape`, the render, the
  budget, and the metric label all go through it, so a value that did not come
  from `Parse` cannot act as a third shape.
