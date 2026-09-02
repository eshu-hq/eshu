# AGENTS.md — internal/mcp guidance for LLM assistants

## Read first

1. `go/internal/mcp/README.md` — pipeline position, tool groups, SSE session
   model, dispatch model, and exported surface
2. `go/internal/mcp/server.go` — `Server`, `NewServer`, `handleMessage`, and
   `RunHTTP`; `go/internal/mcp/server_sse.go` — `handleSSE`,
   `handleHTTPMessage`, and the mutex-guarded `sseSession` (send/shutdown);
   `go/internal/mcp/transport_auth.go` — `WithTransportAuth`,
   `authenticatedTransportHandler`, `authPrincipalKey`, and `peekMCPMethod`.
   Understand the message dispatch switch and the transport-auth wrap before
   touching protocol handling
3. `go/internal/mcp/dispatch.go`, `go/internal/mcp/dispatch_timeout.go`,
   `go/internal/mcp/dispatch_args.go`,
   `go/internal/mcp/dispatch_package_registry.go`,
   `go/internal/mcp/dispatch_cicd.go`, `go/internal/mcp/dispatch_codeowners.go`,
   `go/internal/mcp/dispatch_secrets_iam.go`,
   `go/internal/mcp/dispatch_observability_coverage.go`,
   `go/internal/mcp/dispatch_container_image.go`,
   `go/internal/mcp/dispatch_supply_chain_impact.go`,
   `go/internal/mcp/dispatch_security_alert.go`,
   `go/internal/mcp/dispatch_admission_decisions.go`,
   `go/internal/mcp/dispatch_kubernetes.go`,
   `go/internal/mcp/dispatch_infra_search.go`,
   `go/internal/mcp/dispatch_impact.go`, and
   `go/internal/mcp/dispatch_code_flow.go` — `dispatchTool`,
   deadline handling, `resolveRoute`, the child route adapters, and argument
   helpers; understand `parseCanonicalEnvelope` before touching response
   shaping.
   Package-registry request selection itself lives in
   `go/internal/mcp/packageregistry`, CI/CD run-correlation request selection in
   `go/internal/mcp/cicd`, CODEOWNERS ownership request selection in
   `go/internal/mcp/codeowners`, secrets/IAM posture request selection in
   `go/internal/mcp/secretsiam`, observability-coverage request selection in
   `go/internal/mcp/observabilitycoverage`, container-image identity request
   selection in `go/internal/mcp/containerimage`, supply-chain-impact request
   selection in `go/internal/mcp/supplychainimpact`, security-alert
   reconciliation request selection in `go/internal/mcp/securityalert`,
   admission-decisions request selection in
   `go/internal/mcp/admissiondecisions`, Kubernetes-correlation request
   selection in `go/internal/mcp/kubernetes`, infrastructure-search
   request selection in `go/internal/mcp/infrasearch`, impact-analysis
   request selection in `go/internal/mcp/impact`, code-flow request
   selection in `go/internal/mcp/codeflow`, and dead-code request selection
   in `go/internal/mcp/deadcode`, whose `deadCodeRoute` adapter lives in
   `dispatch.go` itself rather than a dedicated adapter file
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
  `NewRecorder`, using the same handler the HTTP API exposes. Do not add
  separate query logic inside this package.

- **Bounded dispatch context** — `dispatchTool` wraps handler requests in a
  bounded child context and returns a context error if the deadline or parent
  context ends before response parsing. Query handlers must honor
  `r.Context()` cancellation instead of starting unbounded work.

- **Envelope MIME type constant** — `EnvelopeMIMEType` is written as the
  resource MIME type at `server.go:288`. Do not replace with a string literal;
  the constant is the public contract between this package and `internal/query`.

- **Authorization passthrough** — `dispatchTool` forwards the `Authorization`
  header from the original MCP request to the internal handler. If the handler
  requires auth, it must arrive via this path.

- **`Accept` header always set** — dispatch sets
  `Accept: application/eshu.envelope+json`. Handlers gating on this header
  will return the canonical envelope. Removing this header breaks envelope
  detection for all tools.

- **`normalizeQualifiedIdentifier` for service paths** — uses `Cut` at
  `dispatch_args.go:33` to split on `:` and return the tail. Service tools must
  apply this helper; missing it produces paths like
  `/api/v0/services/workload:name/context` which no handler matches.

- **SSE buffer drop / closed session is non-fatal** — `sseSession.send`
  (`server_sse.go`) returns false when the session channel is full OR the
  session has already been closed (client disconnected mid-dispatch);
  `handleHTTPMessage` then logs `Warn` (`"sse session buffer full or closed"`)
  and drops the message. The send is mutex-guarded against the teardown's
  `shutdown`, so a post-disconnect delivery never panics on a closed channel.
  Callers must not assume every tool response arrives via SSE.

## Common changes and how to scope them

- **Add a new MCP tool** →
  1. Add a `ToolDefinition` in the matching `tools_*.go` file (or a new file
     named `tools_<group>.go`).
  2. Add a `case` in `resolveRoute` in `dispatch.go`.
  3. Add the route mapping to the tool-to-route table in `README.md`.
  4. Add a test in `dispatch_test.go` asserting the route, method, and body.
  5. Update the `ReadOnlyTools` count test count in `tools_test.go`.
  6. Run `cd go && go test ./internal/mcp/... -count=1`.
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

- **Move an existing registration family** → put only its definitions in a
  child package that imports `toolcontract`; retain a root wrapper and the
  exact `ReadOnlyTools` position. Routes stay with their current root router.
  Freshness is deliberately split: repository freshness stays in
  `dispatch_repositories.go`, while generation and delta routes stay in
  `dispatch_freshness.go`. Semantic routing is also deliberately split:
  evidence stays in `dispatch_semantic_evidence.go`, while search stays in
  `dispatch_semantic_search.go`. Investigation routing remains split between
  `dispatch_investigation_workflows.go` and
  `dispatch_investigation_packets.go`. Service routing is deliberately split:
  catalog correlations stay in `dispatch_repositories.go` and
  `dispatch_service_catalog.go`, while context, story, investigation, and
  intelligence-report routes stay in `dispatch.go` and
  `dispatch_service_selector.go`.
  Ecosystem registration is one 23-definition group, but routing remains split
  across `dispatch_ecosystem.go`, `dispatch_repositories.go`, `dispatch.go`,
  the `dispatch_infra_search.go` adapter over `infrasearch`, and the
  `dispatch_impact.go` adapter over `impact`.

- **Extract a domain route** → express its family membership decision, decoded
  arguments, and selected HTTP request through `routecontract`; keep the root
  global fanout and thin adapter in place, and prove the old and neutral shapes
  agree. The family selector must not execute the request.

- **Add a new argument helper** → add near `stringSlice` in
  `dispatch_args.go` or near `str`, `intOr`, and `boolOr` in `dispatch.go`.
  Write a focused unit test. Why: helpers are
  shared by multiple tools; a type-assertion bug silently produces zero values.

- **Change SSE keepalive interval** → edit the ticker duration in `handleSSE`
  (`server_sse.go`). The keepalive loop calls `Flush` after each tick
  (`server_sse.go:126`). Update `README.md`. Why: clients may have hard-coded
  assumptions about keepalive cadence.

- **Change the MCP protocol version** → edit `ProtocolVersion` in
  `handleMessage` (`server.go:242`). Check the MCP spec for backward
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
  `dispatch.go:326`.

- Symptom: `find_dead_iac` returns empty results with a Postgres-backed
  reachability store → the IaC reachability field may not be wired in
  the binary; check `cmd/mcp-server/wiring_router.go` at
  `newMCPQueryRouterWithSemanticEmbedding`.

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
  package; see `docs/public/guides/mcp-guide.md` for the structured results
  contract.
- SSE session model (endpoint event format, channel-backed delivery) — clients
  depend on the `event: endpoint\ndata: /mcp/message?sessionId=...` format;
  changing it breaks existing MCP client integrations.
