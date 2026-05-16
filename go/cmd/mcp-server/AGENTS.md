# AGENTS.md — cmd/mcp-server guidance for LLM assistants

## Read first

1. `go/cmd/mcp-server/README.md` — pipeline position, lifecycle, configuration,
   and operational notes
2. `go/cmd/mcp-server/wiring.go` — `wireAPI` and `newMCPQueryRouter`; understand
   these before touching handler composition or env-var wiring
3. `go/cmd/mcp-server/main.go` — transport selection and shutdown; understand
   the `switch transport` before touching startup or signal handling
4. `go/internal/mcp/README.md` — MCP tool dispatch, the SSE session model, and
   the protocol handler
5. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry

## Invariants this package enforces

- **Validation before datastore** — `wireAPI` resolves `loadQueryProfile`,
  `loadGraphBackend`, and `ResolveAPIKey` before opening any connection
  (`wiring.go:32`). An invalid profile, backend, or key returns an error before
  any dial.
- **Postgres required** — `wireAPI` returns an error if both `ESHU_POSTGRES_DSN`
  and `ESHU_CONTENT_STORE_DSN` are empty. The `openQueryGraph` call is skipped
  when `ProfileLocalLightweight` is active or `ESHU_DISABLE_NEO4J` is true
  (`wiring.go:179`).
- **IaC stores always wired** — `newMCPQueryRouter` always sets
  `IaCHandler.Reachability` and `IaCHandler.Management` to Postgres-backed
  query adapters (`wiring.go:146`). Do not set either to nil.
- **MCP read tools must have matching query handlers** — `newMCPQueryRouter`
  wires `CICDHandler` and `SupplyChainHandler` to their Postgres read models so
  `list_ci_cd_run_correlations`, `list_supply_chain_impact_findings`, and
  `list_sbom_attestation_attachments` do not dispatch to 404 routes.
- **Auth on query routes** — `query.AuthMiddleware` wraps the `query.APIRouter`
  handler before it is passed to `mcp.NewServer`. The MCP transport endpoints
  (`/sse`, `/mcp/message`, `/health`) handle auth separately inside the MCP
  transport mux.
- **stdio mode has no HTTP admin surface** — the admin mux is passed to
  `NewServer` only in HTTP mode. In `stdio` mode the transport switch at
  `main.go:54` does not call `NewServer` with an admin mux, so those routes
  are not mounted.
- **Telemetry shutdown on background context** — `telemetry.NewProviders`
  (`main.go:37`) returns a providers value whose `Shutdown` is called with
  `context.Background()`, not with the cancelled root context, so traces
  flushed during shutdown complete.

## Common changes and how to scope them

- **Add a new query handler** → add a field to the `query.APIRouter` struct,
  wire it in `newMCPQueryRouter` in `wiring.go`, assert it in
  `wiring_test.go`, and add the matching tool in `go/internal/mcp/dispatch.go`.
  Run
  `cd go && go test ./cmd/mcp-server ./internal/mcp -count=1`. Why: the
  compile-time assertions (`query.Neo4jReader` satisfies `query.GraphQuery`,
  `query.ContentReader` satisfies `query.ContentStore` — `wiring.go:22`) fail
  if the handler does not
  satisfy its interface; the dispatch route test in `internal/mcp` fails if
  the route is missing.

- **Change transport default** → edit the fallback in `main.go:40` and update
  `doc.go`. Run `go test ./cmd/mcp-server -count=1`. Why: `doc.go` documents
  the default and is read by the service description surface.

- **Add a new env var** → read it via `getenv` in `wireAPI` or `main`, add it
  to the configuration table in `README.md`, and add a test in `wiring_test.go`
  that asserts failure before datastore connection when the var is invalid. Why:
  all env validation must complete before datastore connections.

- **Change the admin surface** → touch `mountRuntimeSurface` in `wiring.go` and
  update the corresponding test in `runtime_surface_test.go`. Why: the tests
  assert `/healthz`, `/readyz`, `/metrics`, and `/admin/status` routes are
  present and return correct shapes.

## Failure modes and how to debug

- Symptom: binary exits 1 immediately on startup → check structured log for
  `event_name=runtime.startup.failed`; sub-causes are bad API key, bad profile,
  bad backend, Postgres dial failure, or telemetry init failure.

- Symptom: MCP client receives no tools → the server started in `stdio` mode
  but the client is pointing at an HTTP URL, or vice versa; check
  `ESHU_MCP_TRANSPORT`.

- Symptom: `/healthz` returns 404 in stdio mode → by design; admin routes are
  only mounted in HTTP mode via `Server.RunHTTP`.

- Symptom: MCP tool returns auth error on `/api/v0/*` routes → API key missing
  or wrong; `query.AuthMiddleware` enforces it on all `/api/` routes.

- Symptom: `ESHU_POSTGRES_DSN` set but Postgres ping fails → check Postgres
  reachability and credentials; `wireAPI` will return before Neo4j dial.

## Anti-patterns specific to this package

- **Calling handler methods directly** — do not call `query.RepositoryHandler`
  or other handlers from `wiring.go` outside of `query.APIRouter.Mount`. All
  routing goes through the `APIRouter`.

- **Setting `ESHU_DISABLE_NEO4J` in production** — this skips the Neo4j dial
  and limits query capability. It is intended for lightweight local profiles
  only.

- **Relying on MCP transport auth for `/api/` routes** — the MCP transport mux
  does not protect `/api/`. Auth for those routes is enforced by
  `query.AuthMiddleware` at `wiring.go:89`.

## What NOT to change without an ADR

- The query handler composition in `newMCPQueryRouter` — adding or removing
  handlers changes the MCP tool surface and must be coordinated with
  `internal/mcp/dispatch.go` tool definitions and `docs/docs/guides/mcp-guide.md`.
- Transport options for `ESHU_MCP_TRANSPORT` — adding a new transport type
  changes the documented wire contract; see `docs/docs/deployment/service-runtimes.md`.
