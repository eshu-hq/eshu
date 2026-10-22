# MCP tool surface

`WithToolSurface` (#8758, epic #8752) chooses what `tools/list` advertises. It
changes advertisement only: the surface never gates `tools/call`, and `s.tools`
is never rewritten. This differs from the query profile: a tool the profile
hides still fails closed on `tools/call`, while a tool only the surface hides
still dispatches.

## How the surface is chosen

1. `cmd/mcp-server` parses `ESHU_MCP_TOOL_SURFACE` with `router.ParseSurface`
   and passes `WithToolSurface`. An unset value is `full`; an unknown value
   refuses to start.
2. `tools/list` calls `router.Advertise(s.tools, surface)` on the
   profile-filtered registry, so `WithQueryProfile` hides tools in every mode
   and option order does not matter. `full` returns `s.tools` itself, so its
   bytes match a server with no surface option.
3. A `surface` query parameter overrides the process surface:
   - `POST /mcp/message?surface=<value>` applies to that request alone.
   - `GET /sse?surface=<value>` is validated before a session exists, stored
     on the `sseSession`, and appended to the `endpoint` event URL, so every
     POST the client follows carries it.
   - An SSE-linked POST with no `surface` uses its session's. A POST that
     names its own wins over the session's.
   - The value is trimmed and case-folded, as the env setting is; a
     whitespace-only value means "absent", so the process surface applies.
   - An unknown value gets a 400 whose message lists the allowed values.
4. The override travels in the request context (`withSurfaceOverride`), so the
   `handleMessage` signature does not change.

## Operator signals

`cmd/mcp-server` logs `mcp tool surface` at startup with `surface`,
`tool_count`, and `tools_list_bytes` from `Server.ToolSurfaceSummary`. The
`mcp server started` line also carries `tool_surface`.

`router` and `core_router` need the `find_tool` and `call_tool` handlers
(#8757) and the core tool set (#8759). The default `full` is unaffected.
