# MCP Tool-Call Telemetry

Every MCP `tools/call` that reaches the dispatcher is counted once by
`eshu_dp_mcp_tool_calls_total`. The dispatcher (`dispatchToolWithOptions` in
`go/internal/mcp/dispatch.go`) opens the count as soon as it knows the requested
name, before it validates arguments, resolves a route, builds a request, or runs
the response budget. It writes the count when dispatch ends, in a `defer`, so a
timeout, a cancellation, an early refusal, and a panic all count.

The dispatcher is not only the MCP transport. The ask engine runs its tools
through `mcp.RunReadOnlyTool`, which calls the same dispatcher in process, and
both `cmd/api` and `cmd/mcp-server` wire the ask engine. Every ask-engine tool
run is therefore counted too, in whichever process served the ask request, and no
label tells it apart from a client `tools/call`. `eshu_dp_mcp_response_bytes`
has the same property. The examples select the MCP server process with
`service_name="mcp-server"`; that drops the `api` process's ask runs but not the
ask runs the MCP server itself serves.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_mcp_tool_calls_total` | counter | `tool`, `outcome` | One increment per dispatched `tools/call`. |

`tool` is the requested name when it is a registered tool. Any other name,
including an empty one and a name that differs only in case or spacing, records
the single value `unknown`, so a client cannot grow the label set. A `call_tool`
request counts under its target's name, because the router hands the target to
the same dispatcher; the `find_tool` and `call_tool` requests themselves are
counted by [MCP Router Telemetry](mcp-router.md).

Under `structured`, generic dispatch failures are budgeted after this counter
closes. An oversized error therefore keeps its dispatcher outcome (`error` or
`unknown_tool`); `eshu_dp_mcp_response_over_budget_total` records the wire
replacement separately.

`outcome` is a closed set. Each value names one way dispatch ends:

| `outcome` | Dispatch ended with |
| --- | --- |
| `ok` | A handler answer the client sees as a success, including a budget page or a resource-only fallback. |
| `error` | A handler answer with a non-2xx status, or a failure after routing, such as a request that could not be built. |
| `over_budget` | The `mcp_response_over_budget` envelope replacing the response. |
| `unknown_tool` | A name no route claims. The `tool` label is `unknown`. |
| `invalid_request` | A registered tool whose arguments the route builder refused before any request was built, such as a required id left empty. |
| `timeout` | The dispatch deadline firing with no usable answer (`mcp_dispatch_timeout`). |
| `partial_timeout` | The deadline firing after the handler wrote a bounded `deadline_truncated` envelope that was returned (`investigate_code_topic` only). |
| `canceled` | The caller's context ending before an answer (`mcp_dispatch_canceled`). |
| `panic` | Dispatch unwinding by panic. The panic is not recovered. |

## Reading it

Call share per tool, the series that picks the `core_router` list:

```promql
sum by (tool) (increase(eshu_dp_mcp_tool_calls_total{service_name="mcp-server"}[14d]))
```

Failure share per tool, the one line to run at 3 AM when a tool looks broken:

```promql
sum by (tool) (rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server",outcome!="ok"}[15m]))
  / sum by (tool) (rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server"}[15m]))
```

Which tools this line drops: a tool with no calls in the window, and a tool that
has had calls but never a non-`ok` outcome since its series appeared. The second
case is the healthy one, and the line is meant to show only tools with failures.
A tool that fails every call stays on the line at 1, because the non-`ok` series
is the one that exists.

Success share per tool needs more care. The `ok` series for a tool is created
on its first `ok` call, so a tool that has never succeeded has no `ok` series.
`sum by (tool) (rate(...{outcome="ok"}[15m])) / sum by (tool) (rate(...[15m]))`
therefore drops exactly that tool: PromQL division only keeps label sets present
on both sides. `1 - failure share` has the opposite hole: it drops every tool
with no failures, which should read 100 percent. Use this form, which falls back
to zero for a tool with no `ok` series:

```promql
(
    sum by (tool) (rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server",outcome="ok"}[15m]))
  or
    sum by (tool) (rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server"}[15m])) * 0
)
  / sum by (tool) (rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server"}[15m]))
```

`or` keeps every `ok` tool and adds a zero for each tool that appears only in the
all-outcomes side. This form drops only a tool with no calls in the window. A
tool whose series exist but saw no calls in the window reads `NaN` (0 divided by
0), which a comparison such as `< 0.95` filters out.

Failure share by cause for one tool: `sum by (outcome)
(rate(eshu_dp_mcp_tool_calls_total{service_name="mcp-server",tool="<name>",outcome!="ok"}[15m]))`.

A rising `unknown_tool` share means clients ask for names the server does not
register, usually a stale tool list or a router that did not offer the tool.
`timeout` and `canceled` point at the deadline and at clients that gave up.
`invalid_request` is a refusal before any request was built, so it points at the
arguments the client sent.

`error` is not a server-fault signal on its own. It covers every non-2xx handler
answer, so a caller mistake the handler answers with a 400 envelope, or a
`not_found` for an entity the caller named wrongly, counts the same as a 5xx or a
transport failure. Failure share therefore includes caller-fixable answers. There
is no status-class label to split them; read the handler's own logs or its
envelope error code when the question is whether the server is at fault.

## Against the response-bytes series

[`eshu_dp_mcp_response_bytes`](mcp-response-budget.md) is recorded after a
successful dispatch and after the budget check, so timeouts, cancellations,
unknown names, invalid arguments, handler failures without an envelope, panics,
and every call made with the budget disabled never reach it. The count of
`eshu_dp_mcp_response_bytes` for a tool is therefore at most that tool's
`outcome` in (`ok`, `error`, `over_budget`, `partial_timeout`) calls, and the
gap between the two is the traffic the older series cannot see. Use
`eshu_dp_mcp_tool_calls_total` for call share and failure share; use
`eshu_dp_mcp_response_bytes` for response size.

The counter was added to size that gap for the `core_router` tool list; see the
[decision record](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/8759-mcp-core-router-tool-set.md).

## Cost

No-Regression Evidence: one counter write per call with precomputed attributes,
about 180 ns and one 16-byte allocation (`BenchmarkCallCounterWriteOnly` in
`go/internal/mcp/dispatch_calls_bench_test.go`). Building the attribute set per
call cost about 430 ns and four allocations (296 B) on the 16-core arm64 host that
produced the figures here, and 550 to 620 ns on a more heavily loaded host, so it
is not used.
`BenchmarkDispatchWithCallCounter` against `BenchmarkDispatchWithoutCallCounter`
adds one allocation (72 to 73) and about 20 B, against a few microseconds for the
dispatch itself with a no-op handler.

Observability Evidence: this page is the operator view; the metric is the
signal, and `go/internal/mcp/dispatch_calls_test.go` proves each outcome through
the production dispatch path.
