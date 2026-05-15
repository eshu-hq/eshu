# AGENTS.md — internal/mcp guidance for LLM assistants

## Read first

1. `go/internal/mcp/README.md` — pipeline position, tool groups, SSE session
   model, dispatch model, and exported surface
2. `go/internal/mcp/server.go` — `Server`, `NewServer`, `handleMessage`,
   `handleSSE`, `handleHTTPMessage`, and `RunHTTP`; understand the message
   dispatch switch before touching protocol handling
3. `go/internal/mcp/dispatch.go`, `go/internal/mcp/dispatch_args.go`, and
   `go/internal/mcp/dispatch_package_registry.go` — `dispatchTool`,
   `resolveRoute`, package-registry cursor mapping, and argument helpers;
   understand `parseCanonicalEnvelope` before touching response shaping
4. `go/internal/mcp/types.go` — `ToolDefinition` and `ReadOnlyTools`; this is
   the tool registry entry point
5. `go/internal/query/` — the `http.Handler` that backs every tool call;
   understand `ResponseEnvelope` and `EnvelopeMIMEType` before changing
   response shape

## Invariants this package enforces

- **Every registered tool has a dispatch route** — `ReadOnlyTools` and
  `resolveRoute` must stay in sync. The dispatch route test in `tools_test.go`
  calls `resolveRoute` for every tool and fails if any returns an error.

- **Shared truth with HTTP** — `dispatchTool` calls `ServeHTTP` via
  `NewRecorder` (`dispatch.go:57`), using the same handler the HTTP API
  exposes. Do not add separate query logic inside this package.

- **Envelope MIME type constant** — `EnvelopeMIMEType` is written as the
  resource MIME type at `server.go:389`. Do not replace with a string literal;
  the constant is the public contract between this package and `internal/query`.

- **Authorization passthrough** — `dispatchTool` (`dispatch.go:46`) forwards
  the `Authorization` header from the original MCP request to the internal
  handler. If the handler requires auth, it must arrive via this path.

- **`Accept` header always set** — `dispatch.go:42` sets
  `Accept: application/eshu.envelope+json`. Handlers gating on this header
  will return the canonical envelope. Removing this header breaks envelope
  detection for all tools.

- **`normalizeQualifiedIdentifier` for service paths** — uses `Cut` at
  `dispatch_args.go:33` to split on `:` and return the tail. Service tools must
  apply this helper; missing it produces paths like
  `/api/v0/services/workload:name/context` which no handler matches.

- **SSE buffer drop is non-fatal** — when the session channel is full,
  the logger calls `Warn` at `server.go:268` and drops the message. Callers
  must not assume every tool response arrives via SSE if the channel is
  saturated.

## Common changes and how to scope them

- **Add a new MCP tool** →
  1. Add a `ToolDefinition` in the matching `tools_*.go` file (or a new file
     named `tools_<group>.go`).
  2. Add a `case` in `resolveRoute` in `dispatch.go`.
  3. Add the route mapping to the tool-to-route table in `README.md`.
  4. Add a test in `dispatch_test.go` asserting the route, method, and body.
  5. Update the `ReadOnlyTools` count test count in `tools_test.go`.
  6. Run `cd go && go test ./internal/mcp -count=1`.
  Why: the dispatch route test will catch a missing route;
  the `ReadOnlyTools` count test will catch a count mismatch.

- **Structural inventory tools** → keep `inspect_code_inventory` as a thin
  dispatch path to `POST /api/v0/code/structure/inventory`. Do not add
  content-index filtering or raw Cypher in MCP; the query handler owns bounds,
  truth metadata, and source handles.

- **Import dependency tools** → keep `investigate_import_dependencies` as a
  thin dispatch path to `POST /api/v0/code/imports/investigate`. Do not add
  file/module expansion or raw Cypher in MCP; the query handler owns graph
  bounds, query type validation, truth metadata, canonical row keys, and source
  handles.

- **Call graph metrics tools** → keep `inspect_call_graph_metrics` as a thin
  dispatch path to `POST /api/v0/code/call-graph/metrics`. Do not add
  recursive or hub-function Cypher in MCP; the query handler owns repo scoping,
  graph aggregation, truth metadata, canonical `functions` rows, source
  handles, and truncation.

- **Change an existing tool's argument mapping** → update `resolveRoute` in
  `dispatch.go`, update the matching `tools_*.go` `InputSchema`, and update or
  add a test in `dispatch_test.go`. Why: the `InputSchema` is the advertised
  contract; mismatches between schema and dispatch body produce silent wrong
  queries.

- **Add a new argument helper** → add near `stringSlice` in
  `dispatch_args.go` or near `str`, `intOr`, and `boolOr` in `dispatch.go`.
  Write a focused unit test. Why: helpers are
  shared by multiple tools; a type-assertion bug silently produces zero values.

- **Change SSE keepalive interval** → edit the ticker duration in `handleSSE`.
  The keepalive loop calls `Flush` after each tick (`server.go:215`). Update
  `README.md`. Why: clients may have hard-coded assumptions about keepalive
  cadence.

- **Change the MCP protocol version** → edit `ProtocolVersion` in
  `handleMessage` (`server.go:339`). Check the MCP spec for backward
  compatibility. Why: clients that cache `initialize` results may reject
  version changes without a new session.

## Failure modes and how to debug

- Symptom: tool call returns `isError=true` with `"unknown tool: <name>"` →
  the tool is in `ReadOnlyTools` but missing from `resolveRoute`;
  the dispatch route test should have caught this — check
  whether tests were run.

- Symptom: tool returns plain JSON instead of the canonical envelope with
  `truth` metadata → the handler is not returning the three-key envelope shape
  (`data`, `truth`, `error`); `Envelope` in `dispatchResult` stays nil and the
  response takes the plain-JSON path; check `parseCanonicalEnvelope`
  (`dispatch_envelope.go:15`) and the handler's response contract.

- Symptom: SSE client receives no response after `POST /mcp/message` →
  the session channel (`sseSession.ch`) may be full (capacity 64); check the
  log for `"sse session buffer full"`.

- Symptom: service tool (`get_service_context`, `get_service_story`) returns
  404 from the internal handler → a qualified identifier like `workload:name`
  was not stripped; verify `PathEscape` receives the stripped value at
  `dispatch.go:356`.

- Symptom: `find_dead_iac` returns empty results with a Postgres-backed
  reachability store → the IaC reachability field may not be wired in
  the binary; check `cmd/mcp-server/wiring.go` at `newMCPQueryRouter`.

## Anti-patterns specific to this package

- **Adding query logic inside dispatch** — do not query Postgres or the graph
  directly from `dispatchTool` or `resolveRoute`. All data access goes through
  the `handler` passed to `NewServer`.

- **Constructing envelope fields manually** — do not build `{data, truth,
  error}` JSON by hand. If a tool needs a non-standard response shape, consult
  `internal/query` and add the handler there.

- **Using string literals for MIME types** — always use `query.EnvelopeMIMEType`.

- **Skipping the dispatch route test** — this is the main
  guard against orphaned tool definitions. Do not remove or disable it.

## What NOT to change without an ADR

- `ReadOnlyTools` output (tool names, required fields) — removing or renaming a
  tool is a breaking change for all MCP clients; coordinate with the MCP guide
  and a protocol version update.
- `parseCanonicalEnvelope` detection logic — the three-key check (`data`,
  `truth`, `error`) is the wire contract between `internal/query` and this
  package; see `docs/docs/guides/mcp-guide.md` for the structured results
  contract.
- SSE session model (endpoint event format, channel-backed delivery) — clients
  depend on the `event: endpoint\ndata: /mcp/message?sessionId=...` format;
  changing it breaks existing MCP client integrations.
