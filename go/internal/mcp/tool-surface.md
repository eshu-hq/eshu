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

## The find_tool ranker

`router` and `core_router` lead with `find_tool`, which needs a ranker.
`core_router` advertises `find_tool`, `call_tool`, and the 10 core tools listed
in `router/core.go` (#8759).
`cmd/mcp-server` passes `WithRouterRanker` with the lexical ranker
(`router/rank`) on every surface (#8771): `find_tool` stays callable on `full`,
and a `?surface=router` override can select it per request.

1. `NewServer` applies every option, then calls `router.Host.Prepare` with the
   final `s.tools`. Option order does not matter: a `WithRouterRanker` given
   before `WithQueryProfile` still indexes the profile-filtered list, so a
   hidden tool never enters the index.
2. `Prepare` loads the catalog entries once and builds the ranker once.
3. The router Engine gets that same entries map, and `callTool` passes the same
   `s.tools` slice as `Request.Visible`, so `rank` reuses its index on every
   call (see the rank README, "Index lifecycle").
4. `newMCPServer` in `cmd/mcp-server` returns an error when the surface is
   `router` or `core_router` and no ranker was built, and `main` exits non-zero.

An empty tool list after the profile filter builds an empty index; `find_tool`
then answers with no candidates, not an error. Rank only reads the index, so
concurrent calls share it safely.

## Operator signals

`cmd/mcp-server` logs `mcp tool surface` at startup with `surface`,
`tool_count`, and `tools_list_bytes` from `Server.ToolSurfaceSummary`, plus
`router_ranker` (true when built), `router_ranker_tool_count`,
`router_ranker_entry_count`, and `router_ranker_build_ms` (the catalog-entry
load is not in the build time). A `router` or `core_router` server with no
ranker logs `mcp server refused to start: no find_tool ranker` with
`event_name=runtime.startup.failed` and exits 1. The `mcp server started`
line also carries `tool_surface`.

Performance Evidence (#8771): measured on linux/arm64, 16 cores, Go 1.26.10,
with the real registry (163 tools after the production profile filter, 167
catalog entries) and no graph or Postgres backend. Two scratch runs (this
lane's and the review's) on the shared dev host, load average 14 to 17; the
ranges cover both. The one-time ranker build (`rank.New`) costs 7.7 to 12.0 ms
and 6.0 to 6.2 MB allocated. All of `newMCPServer` on the `router` surface
costs 9.7 to 16.5 ms and 8.6 MB allocated, against 0.28 to 0.59 ms without a
ranker, so the ranker and catalog load add about 9 to 16 ms once at startup.
The server retains about 1.1 MB more heap with the ranker (1.58 MB against
0.49 MB after GC). The default `full` surface pays both, since it builds the
ranker too. Per call, `Rank` on the real registry with the startup index costs
22 to 40 us, 51 to 54 allocs, and 11.7 to 13.5 KB. A full `find_tool` over
`POST /mcp/message` (JSON-RPC decode, validation, ranking, playbook match,
encode) costs 0.22 to 0.39 ms. Before this change `find_tool` answered
`mcp_router_ranker_unavailable`; had the Engine loaded its own entries map,
every call would have rebuilt the index (about 8 to 12 ms).
`TestMCPServerBuildsTheRankerOnceAndRanksTheSameInputs` pins one build and
identical inputs across concurrent calls. Scratch benchmarks, not committed.

Observability Evidence (#8771): the startup line above carries the ranker
fields (`TestLogToolSurfaceRecordsTheRankerFields` pins them), and `eshu_dp_mcp_router_find_total{outcome}` with the `mcp.router.find`
span covers each request.
