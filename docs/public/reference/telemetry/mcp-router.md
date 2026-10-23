# MCP Router Telemetry

The opt-in MCP router surface (epic #8752) adds two tools that stay out of the
tool registry: `find_tool` ranks the registered tools for an intent, and
`call_tool` runs one by name. `handleMessage` in `go/internal/mcp/server.go`
hands both names to the router engine in `go/internal/mcp/router` before
`dispatchTool`. Three metrics and two spans tell an operator whether routing
works: does `find_tool` return candidates, do agents call what it offered, and
how often do their arguments fail the target's schema.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_mcp_router_find_total` | counter | `outcome` | `find_tool` requests. `candidates`: at least one tool returned. `empty`: the ranker matched nothing the caller may call. `error`: invalid request, no ranker configured, or a ranker failure. |
| `eshu_dp_mcp_router_call_total` | counter | `tool`, `outcome` | `call_tool` requests. `dispatched`: the target ran through the same dispatch as a direct `tools/call` (its own errors are in the result, not here). `invalid_arguments`: the arguments failed the target's input schema, or the `call_tool` request itself was malformed. `unknown_tool`: no registered tool has that name. `refused`: the target was `find_tool`, `call_tool`, or `ask`, or the credential can never call it. |
| `eshu_dp_mcp_router_target_rank` | histogram | none | Where the `call_tool` target sat in the same session's last `find_tool` result, 1-based. `0` means it was not offered, or the session had no `find_tool` result. Buckets 0 through 10 give every position its own bucket. |

`tool` is a registered tool name, or one of the three refused target names
(`find_tool`, `call_tool`, `ask`), or `unregistered`. An unregistered name, a
name over 128 bytes, and a malformed `call_tool` request (no string `name`, or
no `arguments` object) all record `unregistered`, so a caller cannot grow the
label set. `outcome` is a closed set.

A session is the caller's credential principal, the same key that binds an SSE
session. Stdio and unauthenticated transports share one session. The engine
remembers the last result of at most 1024 sessions. Eviction is first-in,
first-out by a session's first `find_tool`; a repeat `find_tool` replaces the
result but does not move the session. An evicted session's next `call_tool`
records rank `0`.

Router error results are bounded whatever the input: a `call_tool` name over
128 bytes is rejected without being echoed or compared with registered names;
at most 20 field errors are returned (`field_errors_truncated` is true only
when more existed); a caller-supplied key in a field path is cut at 256 bytes;
and an array that fails `maxItems` gets no per-item errors.

## Spans

| Span | Wraps | Attributes |
| --- | --- | --- |
| `mcp.router.find` | One `find_tool` request: validation, the per-call credential filter, ranking, and the playbook match. | `outcome` |
| `mcp.router.call` | One `call_tool` request: refusal and lookup, schema validation, the credential check, and the target's dispatch. The dispatched tool's own work is a child of this span. | `mcp.router.target` (the bounded `tool` label above), `outcome` |

A span whose outcome is not `candidates`, `empty`, or `dispatched` has error
status.

## Reading them

- A rising `eshu_dp_mcp_router_find_total{outcome="error"}` rate on a router
  surface means agents cannot discover tools. The result's error code names the
  cause: `mcp_router_invalid_arguments`, `mcp_router_ranker_unavailable`, or
  `mcp_router_rank_failed`.
- `eshu_dp_mcp_router_find_total{outcome="empty"}` with a scoped credential can
  be expected: `find_tool` never offers a tool the API auth middleware always
  refuses for that credential (`execute_cypher_query` among them) or one the
  query profile hides. A tool the middleware admits only under deployment
  policy is offered and may still be refused at dispatch.
- `eshu_dp_mcp_router_call_total{outcome="invalid_arguments"}` by `tool` shows
  which schemas agents misread. The result carries the field errors, the input
  schema, and an example call, so a retry should succeed.
- A target-rank distribution weighted at `0` means agents call tools
  `find_tool` did not offer: either they skip discovery or ranking misses their
  intent. Weight at 1 means the top candidate is the one agents use.
- `refused` on `call_tool` for a scoped credential is a policy answer, not a
  fault. The same call through `tools/call` would get HTTP 403 from the API
  auth middleware.
