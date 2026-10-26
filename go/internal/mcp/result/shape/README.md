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
  canonical `mcp_dispatch_error` envelope with unavailable data and truth,
  subject to the same response budget.

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

## Performance and observability evidence

Issue #8800 adds `ESHU_MCP_RESULT_SHAPE`, a per-connection `shape` query
parameter, and a shape-aware render and response-budget path in
`internal/mcp`. The default stays `dual`.

Benchmark Evidence: `BenchmarkResultShapeRenderBudget` in
`go/internal/mcp/render_budget_bench_test.go` runs the real
`applyResponseBudget` (one render and marshal to size the result) and then the
send-time `renderToolResult` and `json.Marshal`, on a success envelope that fits
the 256 KiB budget, so no resource fallback or page applies. Input shape: a
`find_symbol` envelope of rows holding a 256-byte blob, sized to a one-copy
JSON payload of 2,456, 26,742, and 124,242 bytes. Backend: none; this path
touches no Postgres or graph query. Machine: 16-core arm64 Linux, go1.26.10,
shared with other jobs (load average 20 to 40), so ns/op is noisy and the
byte and allocation columns carry the proof. Command, from `go/internal/mcp`:
`go test -p 2 -run xxx -bench ResultShapeRenderBudget -benchtime 1s`, repeated
in alternating order with the base and head test binaries.

No-Regression Evidence: the baseline is merged `main` at `238b4015a9`, which
has `dual` only. The same benchmark file compiles there unchanged, beside a
one-shape stand-in for the shape table. Dual at head against the base, median
of 16 samples each (two alternating orders of 8):

| Payload | ns/op base | ns/op head | B/op base | B/op head | allocs base | allocs head | wire bytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2 KB | 33,822 | 32,430 | 27,500 | 27,642 | 204 | 204 | 5,220 |
| 25 KB | 308,310 | 295,125 | 284,173 | 284,086 | 1,965 | 1,965 | 54,320 |
| 120 KB | 1,396,129 | 1,443,296 | 1,354,420 | 1,362,852 | 9,008 | 9,008 | 251,432 |

Default `dual` allocates the same and sends the same bytes as the base. Time
is within the spread of the samples (the 120 KB row is 3.4 percent higher at
the median, with the head minimum below the base minimum). An earlier first
pass of 6 samples per side, base always run first in each round, read 15 to 29
percent slower for head, with fully overlapping ranges; two later passes in
opposite orders did not reproduce it, so it is recorded as ordering noise
rather than a regression, not as a speedup. Dual wire bytes stay under the
262,144-byte budget at 120 KB (251,432), and the benchmark fails if the budget
alters the result.

`structured` against `dual` at head, same process, median of 6 samples:

| Payload | wire bytes dual | wire bytes structured | ns/op dual | ns/op structured | B/op dual | B/op structured | allocs dual | allocs structured |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2 KB | 5,220 | 2,555 | 41,082 | 19,284 | 27,618 | 8,660 | 204 | 108 |
| 25 KB | 54,320 | 26,841 | 425,045 | 169,796 | 283,733 | 83,462 | 1,964 | 988 |
| 120 KB | 251,432 | 124,341 | 1,898,046 | 697,712 | 1,345,352 | 395,998 | 9,007 | 4,508 |

`structured` sends 0.49 times the bytes of `dual` for the same payload and
allocates about half as often. Its time column comes from the noisy pass, so
read it as the same direction as the byte and allocation columns, not as a
ratio to quote. The saving is the point of the shape: one copy of the payload
is rendered, marshalled, and sent instead of two. Queue and row counts do not
apply; the change adds no queue, lease, worker, or Cypher.

Observability Evidence: the four response-budget instruments carry the bounded
`shape` attribute (`dual` or `structured`, folded by `OrDual`), next to `tool`:
`eshu_dp_mcp_response_bytes`, `eshu_dp_mcp_response_over_budget_total`,
`eshu_dp_mcp_response_resource_fallback_total`, and
`eshu_dp_mcp_response_budget_page_total`. The three budget log lines (`mcp tool
response resource fallback`, `mcp tool response budget page`, `mcp tool
response over budget`) carry `shape`, and the over-budget line omits
`resource_bytes` under `structured`, which has no resource copy. An operator
reads `eshu_dp_mcp_response_bytes` by `shape` to see the saving per tool and
the resource-fallback counter to see which dual callers would benefit. The
instruments and queries are documented in
`docs/public/reference/telemetry/mcp-response-budget.md`.

Why the config surfaces are safe: the `ESHU_MCP_RESULT_SHAPE` entry in
`go/internal/envregistry/entries.go` is a closed enum (`dual`, `structured`)
with default `dual`, so an unset variable keeps today's result and an unknown
value refuses to start rather than falling back silently. The Helm
`values.yaml` change only adds a commented example under `mcpServer.env`;
`env` stays `{}`, so a deployment renders the same manifests until an operator
sets the variable, and setting it back to `dual` rolls back.
